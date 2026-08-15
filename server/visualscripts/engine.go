package visualscripts

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"sync"
	"time"
)

type compiledPlan struct {
	graph      GraphDocument
	nodes      map[string]GraphNode
	outgoing   map[string]map[string][]GraphEdge
	simulation bool
}

type queuedRun struct {
	run     *Run
	trigger GraphNode
	message Message
	plan    *compiledPlan
}

type instanceRuntime struct {
	org            string
	scriptID       string
	instanceKey    string
	activeRevision int
	maxConcurrency int
	queueLimit     int
	ctx            context.Context
	cancel         context.CancelFunc
	mu             sync.Mutex
	queue          []*queuedRun
	running        int
	stopped        bool
}

type Engine struct {
	store       Store
	registry    *Registry
	mu          sync.RWMutex
	plans       map[string]*compiledPlan
	statuses    map[string]RuntimeStatus
	context     map[string]any
	instances   map[string]*instanceRuntime
	rootCtx     context.Context
	rootCancel  context.CancelFunc
	lifecycleMu sync.Mutex
	stateMu     sync.RWMutex
	closed      bool
	runs        sync.WaitGroup
	sequence    uint64
	services    RuntimeServices
	triggerMu   sync.Mutex
	triggers    map[string]*triggerSession
	edgeStates  map[string]edgeState
	nodeHandler func(context.Context, GraphNode, Message) (string, Message, bool, error)
}

func New(store Store) *Engine {
	return NewWithServices(store, RuntimeServices{})
}

func NewWithServices(store Store, services RuntimeServices) *Engine {
	ctx, cancel := context.WithCancel(context.Background())
	if services.TagRouter == nil {
		services.TagRouter = NewTagChangeRouter(100, 100)
	}
	if services.Now == nil {
		services.Now = time.Now
	}
	return &Engine{store: store, registry: NewRegistry(), plans: make(map[string]*compiledPlan), statuses: make(map[string]RuntimeStatus), context: make(map[string]any), instances: make(map[string]*instanceRuntime), rootCtx: ctx, rootCancel: cancel, services: services, triggers: make(map[string]*triggerSession), edgeStates: make(map[string]edgeState)}
}

// Close cancels all queued and running script instances and waits for their
// goroutines to finish. Long-running node implementations must honor the
// context passed to handleNode so shutdown remains prompt.
func (e *Engine) Close() {
	e.lifecycleMu.Lock()
	if e.closed {
		e.lifecycleMu.Unlock()
		return
	}
	e.closed = true
	e.stopTriggers("", "")
	e.rootCancel()
	e.lifecycleMu.Unlock()
	e.cancelInstances("", "", "Engine stopped")
	e.runs.Wait()
}

func (e *Engine) Registry() *Registry { return e.registry }

func (e *Engine) Validate(graph GraphDocument) ValidationResult {
	return ValidateGraph(e.registry, graph)
}

func (e *Engine) Deploy(ctx context.Context, org, scriptID string, revision int) (*RuntimeStatus, error) {
	return e.deploy(ctx, org, scriptID, revision, true)
}

func (e *Engine) deploy(ctx context.Context, org, scriptID string, revision int, startAutonomous bool) (*RuntimeStatus, error) {
	e.stateMu.Lock()
	defer e.stateMu.Unlock()
	rev, err := e.store.GetVisualScriptRevision(ctx, org, scriptID, revision)
	if err != nil {
		return nil, err
	}
	if rev == nil {
		return nil, ErrNotFound
	}
	validation := e.Validate(rev.Graph)
	if !validation.Valid {
		return nil, fmt.Errorf("revision %d is invalid", revision)
	}
	script, err := e.store.GetVisualScript(ctx, org, scriptID)
	if err != nil {
		return nil, err
	}
	if script == nil {
		return nil, ErrNotFound
	}
	plan := compile(validation.Graph, script.Simulation)
	if err := e.store.SetVisualScriptActiveRevision(ctx, org, scriptID, &revision); err != nil {
		return nil, err
	}
	e.cancelInstances(org, scriptID, "Script revision changed")
	e.stopTriggers(org, scriptID)
	e.mu.Lock()
	e.plans[scriptKey(org, scriptID)] = plan
	e.clearContextLocked(org, scriptID)
	e.sequence++
	e.mu.Unlock()
	if startAutonomous && script.DesiredState == "running" {
		e.startTriggers(org, scriptID, revision, plan)
	}
	status, err := e.Status(ctx, org, scriptID)
	return &status, err
}

// StartCurrent validates and compiles the single current script before it can run.
// Revisions remain an internal optimistic-write mechanism and are not a user-facing state.
func (e *Engine) StartCurrent(ctx context.Context, org, scriptID string) (*RuntimeStatus, error) {
	script, err := e.store.GetVisualScript(ctx, org, scriptID)
	if err != nil {
		return nil, err
	}
	if script == nil {
		return nil, ErrNotFound
	}
	if script.LatestRevision < 1 {
		return nil, errors.New("save the script before starting it")
	}
	if _, err = e.deploy(ctx, org, scriptID, script.LatestRevision, false); err != nil {
		return nil, err
	}
	if err = e.store.ClearVisualScriptRuns(ctx, org, scriptID); err != nil {
		return nil, fmt.Errorf("clearing run trace: %w", err)
	}
	e.mu.Lock()
	status := e.statuses[scriptKey(org, scriptID)]
	status.LastTriggerAt = nil
	status.LastCompleteAt = nil
	status.ErrorSummary = ""
	status.QueueDepth = 0
	e.statuses[scriptKey(org, scriptID)] = status
	e.mu.Unlock()
	return e.SetDesiredState(ctx, org, scriptID, "running")
}

func (e *Engine) StartActivated(ctx context.Context) []error {
	errorsFound := []error{}
	if err := e.store.CancelIncompleteVisualScriptRuns(ctx, time.Now().UTC(), "Server restarted before the run completed"); err != nil {
		errorsFound = append(errorsFound, fmt.Errorf("recovering incomplete visual script runs: %w", err))
	}
	scripts, err := e.store.ListActivatedVisualScripts(ctx)
	if err != nil {
		return append(errorsFound, err)
	}
	for _, script := range scripts {
		if _, err := e.StartCurrent(ctx, script.OrgName, script.ID); err != nil {
			errorsFound = append(errorsFound, fmt.Errorf("starting activated script %s/%s: %w", script.OrgName, script.Name, err))
		}
	}
	return errorsFound
}

func (e *Engine) Undeploy(ctx context.Context, org, scriptID string) (*RuntimeStatus, error) {
	e.stateMu.Lock()
	defer e.stateMu.Unlock()
	if err := e.store.SetVisualScriptDesiredState(ctx, org, scriptID, "stopped"); err != nil {
		return nil, err
	}
	if err := e.store.SetVisualScriptActiveRevision(ctx, org, scriptID, nil); err != nil {
		return nil, err
	}
	e.cancelInstances(org, scriptID, "Script stopped")
	e.stopTriggers(org, scriptID)
	e.mu.Lock()
	delete(e.plans, scriptKey(org, scriptID))
	e.clearContextLocked(org, scriptID)
	e.sequence++
	e.mu.Unlock()
	status, err := e.Status(ctx, org, scriptID)
	return &status, err
}

func (e *Engine) SetDesiredState(ctx context.Context, org, scriptID, state string) (*RuntimeStatus, error) {
	e.stateMu.Lock()
	defer e.stateMu.Unlock()
	if state != "stopped" && state != "running" && state != "paused" {
		return nil, fmt.Errorf("invalid desired state %q", state)
	}
	script, err := e.store.GetVisualScript(ctx, org, scriptID)
	if err != nil {
		return nil, err
	}
	if script == nil {
		return nil, ErrNotFound
	}
	if state != "stopped" && script.ActiveRevision == nil {
		return nil, errors.New("deploy a valid revision before starting the script")
	}
	if err := e.store.SetVisualScriptDesiredState(ctx, org, scriptID, state); err != nil {
		return nil, err
	}
	if state == "stopped" {
		e.cancelInstances(org, scriptID, "Script stopped")
		e.stopTriggers(org, scriptID)
		e.mu.Lock()
		e.clearContextLocked(org, scriptID)
		e.mu.Unlock()
	} else if state == "paused" {
		e.stopTriggers(org, scriptID)
	} else if state == "running" {
		plan, planErr := e.plan(ctx, org, scriptID, *script.ActiveRevision)
		if planErr != nil {
			return nil, planErr
		}
		e.startTriggers(org, scriptID, *script.ActiveRevision, plan)
	}
	status, err := e.Status(ctx, org, scriptID)
	return &status, err
}

func (e *Engine) Status(ctx context.Context, org, scriptID string) (RuntimeStatus, error) {
	script, err := e.store.GetVisualScript(ctx, org, scriptID)
	if err != nil {
		return RuntimeStatus{}, err
	}
	if script == nil {
		return RuntimeStatus{}, ErrNotFound
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	key := scriptKey(org, scriptID)
	status := e.statuses[key]
	status.ScriptID = scriptID
	status.DesiredState = script.DesiredState
	status.ActiveRevision = script.ActiveRevision
	status.LatestRevision = script.LatestRevision
	if script.ActiveRevision == nil {
		status.RuntimeState = "idle"
	} else {
		status.RuntimeState = script.DesiredState
	}
	e.sequence++
	status.Sequence = e.sequence
	e.statuses[key] = status
	return status, nil
}

func (e *Engine) RunManual(ctx context.Context, org, scriptID string, request RunRequest) (*Run, error) {
	if strings.TrimSpace(request.InstanceKey) == "" {
		request.InstanceKey = "manual"
	}
	return e.enqueueTrigger(ctx, org, scriptID, request, "core.manual", time.Time{})
}

// RunTagChange is the RTDB-facing trigger entry point. TagChangeRouter assigns
// InstanceKey from wildcard path segments, so events for the same resolved
// device share context and limits while different devices run independently.
func (e *Engine) RunTagChange(ctx context.Context, org, scriptID, triggerNodeID string, change TagChange) (*Run, error) {
	instanceKey := strings.TrimSpace(change.InstanceKey)
	if instanceKey == "" {
		instanceKey = NormalizePathPattern(change.TagPath)
	}
	return e.enqueueTrigger(ctx, org, scriptID, RunRequest{
		TriggerNodeID: triggerNodeID,
		InstanceKey:   instanceKey,
		DevicePath:    change.DevicePath,
		TagPath:       change.TagPath,
		Value:         change.Value,
		Fields:        change.Fields,
	}, "", change.Timestamp)
}

func (e *Engine) enqueueTrigger(ctx context.Context, org, scriptID string, request RunRequest, requiredType string, triggerAt time.Time) (*Run, error) {
	if e.services.CanExecute != nil && !e.services.CanExecute() {
		return nil, ErrNotLeader
	}
	// Serialize acceptance with state transitions. In particular, a trigger that
	// observed "running" cannot create a fresh instance after Stop has cancelled
	// the script's existing instances.
	e.stateMu.RLock()
	defer e.stateMu.RUnlock()
	if e.rootCtx.Err() != nil {
		return nil, ErrNotRunning
	}
	script, err := e.store.GetVisualScript(ctx, org, scriptID)
	if err != nil {
		return nil, err
	}
	if script == nil || script.ActiveRevision == nil {
		return nil, errors.New("no active revision")
	}
	if script.DesiredState != "running" {
		return nil, fmt.Errorf("%w: script is %s", ErrNotRunning, script.DesiredState)
	}
	plan, err := e.plan(ctx, org, scriptID, *script.ActiveRevision)
	if err != nil {
		return nil, err
	}
	triggerID := request.TriggerNodeID
	if triggerID == "" {
		for _, node := range plan.graph.Nodes {
			definition, _ := e.registry.Definition(node.Type)
			if (requiredType == "" && definition.Category == "Triggers") || node.Type == requiredType {
				triggerID = node.ID
				break
			}
		}
	}
	trigger, ok := plan.nodes[triggerID]
	definition, definitionOK := e.registry.Definition(trigger.Type)
	if !ok || !definitionOK || definition.Category != "Triggers" || (requiredType != "" && trigger.Type != requiredType) {
		if requiredType == "core.manual" {
			return nil, errors.New("select a Manual trigger")
		}
		return nil, errors.New("select a trigger node")
	}
	instanceKey := strings.TrimSpace(request.InstanceKey)
	if instanceKey == "" {
		instanceKey = "default"
	}
	if len(instanceKey) > 512 {
		return nil, errors.New("script instance key exceeds 512 bytes")
	}
	if strings.ContainsRune(instanceKey, '\x00') {
		return nil, errors.New("script instance key contains an invalid character")
	}
	now := time.Now().UTC()
	if triggerAt.IsZero() {
		triggerAt = now
	} else {
		triggerAt = triggerAt.UTC()
	}
	runID := newID("run")
	run := &Run{RunID: runID, OrgName: org, ScriptID: scriptID, ActiveRevision: *script.ActiveRevision, TriggerNodeID: triggerID, InstanceKey: instanceKey, StartedAt: now, Status: "queued"}
	message := Message{ID: newID("msg"), CorrelationID: runID, OrgName: org, ScriptID: scriptID, ActiveRevision: *script.ActiveRevision, TriggerNodeID: triggerID, InstanceKey: instanceKey, TriggerTimestamp: triggerAt, DevicePath: request.DevicePath, TagPath: NormalizePathPattern(request.TagPath), Value: request.Value, Fields: cloneFields(request.Fields)}
	if message.Fields == nil {
		message.Fields = make(map[string]any)
	}
	if encoded, marshalErr := json.Marshal(message); marshalErr != nil || len(encoded) > 256<<10 {
		return nil, errors.New("trigger message must be JSON-compatible and no larger than 256 KiB")
	}
	runtime := e.instanceRuntime(org, scriptID, instanceKey, *script.ActiveRevision, plan)
	task := &queuedRun{run: run, trigger: trigger, message: message, plan: plan}
	runtime.mu.Lock()
	if runtime.stopped {
		runtime.mu.Unlock()
		return nil, ErrNotRunning
	}
	if runtime.running >= runtime.maxConcurrency && len(runtime.queue) >= runtime.queueLimit {
		runtime.mu.Unlock()
		return nil, ErrQueueFull
	}
	if err := e.store.AppendVisualScriptRun(ctx, run); err != nil {
		runtime.mu.Unlock()
		return nil, err
	}
	startNow := runtime.running < runtime.maxConcurrency
	if startNow {
		runtime.running++
	} else {
		runtime.queue = append(runtime.queue, task)
	}
	runtime.mu.Unlock()

	e.recordAcceptedTrigger(org, scriptID, now, !startNow)
	response := *run
	if startNow {
		if !e.launchRun(runtime, task) {
			e.cancelQueuedRun(task.run, "Engine stopped")
		}
	}
	return &response, nil
}

func (e *Engine) instanceRuntime(org, scriptID, instanceKey string, revision int, plan *compiledPlan) *instanceRuntime {
	key := instanceRuntimeKey(org, scriptID, revision, instanceKey)
	e.mu.Lock()
	defer e.mu.Unlock()
	if runtime := e.instances[key]; runtime != nil {
		return runtime
	}
	ctx, cancel := context.WithCancel(e.rootCtx)
	runtime := &instanceRuntime{
		org: org, scriptID: scriptID, instanceKey: instanceKey, activeRevision: revision,
		maxConcurrency: plan.graph.Settings.MaxConcurrency, queueLimit: plan.graph.Settings.QueueLimit,
		ctx: ctx, cancel: cancel,
	}
	e.instances[key] = runtime
	return runtime
}

func (e *Engine) launchRun(runtime *instanceRuntime, task *queuedRun) bool {
	e.lifecycleMu.Lock()
	if e.closed {
		e.lifecycleMu.Unlock()
		return false
	}
	e.runs.Add(1)
	e.lifecycleMu.Unlock()
	go func() {
		defer e.runs.Done()
		e.executeQueuedRun(runtime, task)
		e.finishInstanceRun(runtime)
	}()
	return true
}

func (e *Engine) executeQueuedRun(runtime *instanceRuntime, task *queuedRun) {
	task.run.Status = "running"
	_ = e.persistRun(task.run)
	executionStarted := time.Now().UTC()
	execErr := e.execute(runtime.ctx, task.plan, task.trigger, task.message, task.run)
	completed := time.Now().UTC()
	task.run.CompletedAt = &completed
	task.run.DurationMS = completed.Sub(executionStarted).Milliseconds()
	if execErr != nil {
		if errors.Is(execErr, context.Canceled) {
			task.run.Status = "cancelled"
			task.run.Message = "Run cancelled"
		} else {
			task.run.Status = "error"
			task.run.Message = execErr.Error()
		}
	} else {
		task.run.Status = "ok"
		task.run.Message = "Run completed"
	}
	_ = e.persistRun(task.run)
	e.recordCompletedRun(runtime.org, runtime.scriptID, completed, execErr)
}

func (e *Engine) finishInstanceRun(runtime *instanceRuntime) {
	var next *queuedRun
	runtime.mu.Lock()
	if runtime.running > 0 {
		runtime.running--
	}
	if !runtime.stopped && len(runtime.queue) > 0 {
		next = runtime.queue[0]
		runtime.queue = runtime.queue[1:]
		runtime.running++
	}
	runtime.mu.Unlock()
	if next != nil {
		e.adjustQueueDepth(runtime.org, runtime.scriptID, -1)
		if !e.launchRun(runtime, next) {
			e.cancelQueuedRun(next.run, "Engine stopped")
		}
	}
}

func (e *Engine) cancelInstances(org, scriptID, reason string) {
	e.mu.Lock()
	runtimes := make([]*instanceRuntime, 0)
	for key, runtime := range e.instances {
		if (org == "" || runtime.org == org) && (scriptID == "" || runtime.scriptID == scriptID) {
			delete(e.instances, key)
			runtimes = append(runtimes, runtime)
		}
	}
	e.mu.Unlock()
	for _, runtime := range runtimes {
		runtime.mu.Lock()
		runtime.stopped = true
		runtime.cancel()
		queued := runtime.queue
		runtime.queue = nil
		runtime.mu.Unlock()
		if len(queued) > 0 {
			e.adjustQueueDepth(runtime.org, runtime.scriptID, -len(queued))
		}
		for _, task := range queued {
			e.cancelQueuedRun(task.run, reason)
		}
	}
}

func (e *Engine) cancelQueuedRun(run *Run, reason string) {
	completed := time.Now().UTC()
	run.CompletedAt = &completed
	run.Status = "cancelled"
	run.Message = reason
	_ = e.persistRun(run)
	e.recordCompletedRun(run.OrgName, run.ScriptID, completed, context.Canceled)
}

func (e *Engine) persistRun(run *Run) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return e.store.CompleteVisualScriptRun(ctx, run)
}

func (e *Engine) recordAcceptedTrigger(org, scriptID string, at time.Time, queued bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	key := scriptKey(org, scriptID)
	status := e.statuses[key]
	status.LastTriggerAt = &at
	if queued {
		status.QueueDepth++
	}
	e.sequence++
	status.Sequence = e.sequence
	e.statuses[key] = status
}

func (e *Engine) adjustQueueDepth(org, scriptID string, delta int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	key := scriptKey(org, scriptID)
	status := e.statuses[key]
	status.QueueDepth += delta
	if status.QueueDepth < 0 {
		status.QueueDepth = 0
	}
	e.sequence++
	status.Sequence = e.sequence
	e.statuses[key] = status
}

func (e *Engine) recordCompletedRun(org, scriptID string, completed time.Time, execErr error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	key := scriptKey(org, scriptID)
	status := e.statuses[key]
	status.LastCompleteAt = &completed
	status.ErrorSummary = ""
	if execErr != nil && !errors.Is(execErr, context.Canceled) {
		status.ErrorSummary = execErr.Error()
	}
	e.sequence++
	status.Sequence = e.sequence
	e.statuses[key] = status
}

func (e *Engine) plan(ctx context.Context, org, scriptID string, revision int) (*compiledPlan, error) {
	key := scriptKey(org, scriptID)
	e.mu.RLock()
	plan := e.plans[key]
	e.mu.RUnlock()
	if plan != nil {
		return plan, nil
	}
	rev, err := e.store.GetVisualScriptRevision(ctx, org, scriptID, revision)
	if err != nil || rev == nil {
		return nil, fmt.Errorf("loading active revision: %w", err)
	}
	validation := e.Validate(rev.Graph)
	if !validation.Valid {
		return nil, errors.New("active revision is invalid")
	}
	script, err := e.store.GetVisualScript(ctx, org, scriptID)
	if err != nil || script == nil {
		if err == nil {
			err = ErrNotFound
		}
		return nil, err
	}
	plan = compile(validation.Graph, script.Simulation)
	e.mu.Lock()
	e.plans[key] = plan
	e.mu.Unlock()
	return plan, nil
}

func compile(graph GraphDocument, simulation ...bool) *compiledPlan {
	plan := &compiledPlan{graph: graph, nodes: make(map[string]GraphNode), outgoing: make(map[string]map[string][]GraphEdge)}
	if len(simulation) > 0 {
		plan.simulation = simulation[0]
	}
	for _, node := range graph.Nodes {
		plan.nodes[node.ID] = node
	}
	for _, edge := range graph.Edges {
		if plan.outgoing[edge.From.NodeID] == nil {
			plan.outgoing[edge.From.NodeID] = make(map[string][]GraphEdge)
		}
		plan.outgoing[edge.From.NodeID][edge.From.Port] = append(plan.outgoing[edge.From.NodeID][edge.From.Port], edge)
	}
	return plan
}

type queuedNode struct {
	node GraphNode
	msg  Message
}

func (e *Engine) execute(ctx context.Context, plan *compiledPlan, trigger GraphNode, message Message, run *Run) error {
	queue := []queuedNode{{node: trigger, msg: message}}
	hops := 0
	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		item := queue[0]
		queue = queue[1:]
		hops++
		if hops > 1000 {
			return errors.New("maximum execution hops exceeded")
		}
		definition, _ := e.registry.Definition(item.node.Type)
		if definition.OutputNode && !definition.SimulationSafe && e.services.CanExecute != nil && !e.services.CanExecute() {
			return fmt.Errorf("node %s: %w", item.node.ID, ErrNotLeader)
		}
		simulatedOutput := plan.simulation && definition.OutputNode && !definition.SimulationSafe
		var traceInput Message
		if item.node.Type == "core.debug" {
			traceInput = cloneMessage(item.msg)
		}
		var port string
		var nextMessage Message
		var action bool
		var err error
		if simulatedOutput {
			port, nextMessage, action = "out", item.msg, true
		} else {
			if e.nodeHandler != nil {
				port, nextMessage, action, err = e.nodeHandler(ctx, item.node, item.msg)
			} else {
				port, nextMessage, action, err = e.handleNode(ctx, item.node, item.msg)
			}
		}
		run.NodesExecuted++
		if action {
			run.ActionsAttempted++
		}
		if item.node.Type == "core.debug" {
			trace := TraceEvent{Sequence: run.NodesExecuted, Timestamp: time.Now().UTC(), NodeID: item.node.ID, NodeType: item.node.Type, Port: port, Status: "ok", Value: cloneValue(traceInput.Value), Fields: cloneFields(traceInput.Fields)}
			var config map[string]any
			if json.Unmarshal(item.node.Config, &config) == nil {
				trace.Message = strings.TrimSpace(stringConfig(config, "label"))
				trace.FormattedTimes = debugFormattedTimes(traceInput, config)
			}
			if err != nil {
				trace.Status = "error"
				trace.Message = err.Error()
			}
			if len(run.Trace) < 200 {
				run.Trace = append(run.Trace, trace)
			} else {
				run.DroppedTraces++
			}
		}
		if err != nil {
			run.FirstErrorNodeID = item.node.ID
		}
		if err != nil {
			return fmt.Errorf("node %s: %w", item.node.ID, err)
		}
		for _, edge := range plan.outgoing[item.node.ID][port] {
			next, ok := plan.nodes[edge.To.NodeID]
			if ok {
				queue = append(queue, queuedNode{node: next, msg: cloneMessage(nextMessage)})
			}
		}
	}
	return nil
}

func (e *Engine) handleNode(ctx context.Context, node GraphNode, msg Message) (string, Message, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", msg, false, err
	}
	var config map[string]any
	if err := json.Unmarshal(node.Config, &config); err != nil {
		return "", msg, false, err
	}
	switch node.Type {
	case "core.manual", "core.timer", "core.tag-changed", "core.rising-edge", "core.falling-edge", "core.startup":
		return "out", msg, false, nil
	case "core.compare":
		value, err := selectedValue(msg, stringConfig(config, "field"))
		if err != nil {
			return "", msg, false, err
		}
		result, err := compareValues(value, config["compareTo"], stringConfig(config, "operator"))
		return boolPort(result), msg, false, err
	case "core.compare-times":
		leftValue, err := selectedValue(msg, stringConfig(config, "leftField"))
		if err != nil {
			return "", msg, false, err
		}
		left, err := timestampMillis(leftValue)
		if err != nil {
			return "", msg, false, fmt.Errorf("left time: %w", err)
		}
		right, err := configuredOrMessageTime(msg, config, stringConfig(config, "rightSource"), "rightField", "rightTime", e.services.Now)
		if err != nil {
			return "", msg, false, fmt.Errorf("right time: %w", err)
		}
		result, err := compareTimeValues(left, right, stringConfig(config, "operator"))
		return boolPort(result), msg, false, err
	case "core.in-range":
		value, err := selectedValue(msg, stringConfig(config, "field"))
		if err != nil {
			return "", msg, false, err
		}
		n, ok := finiteFloat(value)
		if !ok {
			return "", msg, false, errors.New("in-range input must be a finite number")
		}
		minimum, err := configFiniteFloat(config, "minimum", 0)
		if err != nil {
			return "", msg, false, err
		}
		maximum, err := configFiniteFloat(config, "maximum", 100)
		if err != nil {
			return "", msg, false, err
		}
		if minimum > maximum {
			return "", msg, false, errors.New("in-range maximum must be greater than or equal to minimum")
		}
		inclusive := true
		if configured, exists := config["inclusive"]; exists {
			var valid bool
			inclusive, valid = configured.(bool)
			if !valid {
				return "", msg, false, errors.New("in-range inclusive must be a boolean")
			}
		}
		inside := n > minimum && n < maximum
		if inclusive {
			inside = n >= minimum && n <= maximum
		}
		return boolPort(inside), msg, false, nil
	case "core.not":
		value, err := selectedValue(msg, stringConfig(config, "field"))
		if err != nil {
			return "", msg, false, err
		}
		truth, ok := value.(bool)
		if !ok {
			return "", msg, false, errors.New("NOT requires a boolean")
		}
		return boolPort(!truth), msg, false, nil
	case "core.and", "core.or":
		fields, ok := config["fields"].([]any)
		if !ok || len(fields) == 0 {
			return "", msg, false, errors.New("logical node requires a non-empty fields array")
		}
		result := node.Type == "core.and"
		for _, fieldName := range fields {
			fieldValue, exists := getField(msg.Fields, fmt.Sprint(fieldName))
			boolean, isBoolean := fieldValue.(bool)
			if !exists || !isBoolean {
				return "", msg, false, fmt.Errorf("field %q is not boolean", fieldName)
			}
			if node.Type == "core.and" {
				result = result && boolean
			} else {
				result = result || boolean
			}
		}
		return boolPort(result), msg, false, nil
	case "core.set-field":
		setSelectedValue(&msg, stringConfig(config, "field"), config["value"])
	case "core.select-field":
		field := stringConfig(config, "field")
		selected, ok := getField(msg.Fields, field)
		if !ok {
			return "", msg, false, fmt.Errorf("field %q does not exist", field)
		}
		msg.Value = selected
	case "core.current-time":
		setSelectedValue(&msg, stringConfig(config, "outputField"), e.services.Now().UnixMilli())
	case "core.time-since":
		value, err := selectedValue(msg, stringConfig(config, "field"))
		if err != nil {
			return "", msg, false, err
		}
		then, err := timestampMillis(value)
		if err != nil {
			return "", msg, false, err
		}
		elapsed, err := elapsedInUnit(e.services.Now().UnixMilli()-then, stringConfig(config, "unit"))
		if err != nil {
			return "", msg, false, err
		}
		setSelectedValue(&msg, stringConfig(config, "outputField"), elapsed)
	case "core.multiply", "core.divide", "core.clamp", "core.scale":
		value, err := selectedValue(msg, stringConfig(config, "field"))
		if err != nil {
			return "", msg, false, err
		}
		output, err := transformNumber(node.Type, value, config)
		if err != nil {
			return "", msg, false, err
		}
		setSelectedValue(&msg, stringConfig(config, "field"), output)
	case "core.average":
		values := []any{}
		if fields, ok := config["fields"].([]any); ok && len(fields) > 0 {
			for _, fieldName := range fields {
				fieldValue, exists := getField(msg.Fields, fmt.Sprint(fieldName))
				if !exists {
					return "", msg, false, fmt.Errorf("field %q does not exist", fieldName)
				}
				values = append(values, fieldValue)
			}
		} else if list, ok := msg.Value.([]any); ok {
			values = list
		} else {
			return "", msg, false, errors.New("average requires a numeric value array or configured fields")
		}
		total := 0.0
		for _, item := range values {
			number, ok := asFloat(item)
			if !ok {
				return "", msg, false, errors.New("average inputs must be numeric")
			}
			total += number
		}
		if len(values) == 0 {
			return "", msg, false, errors.New("average requires at least one value")
		}
		msg.Value = total / float64(len(values))
	case "core.get-context":
		contextValue, ok := e.getContext(msg, node.ID, stringConfig(config, "scope"), stringConfig(config, "key"))
		if !ok {
			return "", msg, false, errors.New("context key does not exist")
		}
		msg.Value = contextValue
	case "core.set-context":
		contextValue := msg.Value
		if stringConfig(config, "source") == "configured" {
			contextValue = config["value"]
		}
		e.setContext(msg, node.ID, stringConfig(config, "scope"), stringConfig(config, "key"), contextValue)
	case "core.delete-context":
		e.deleteContext(msg, node.ID, stringConfig(config, "scope"), stringConfig(config, "key"))
	case "core.increment-context":
		amount, err := configFiniteFloat(config, "amount", 1)
		if err != nil {
			return "", msg, false, err
		}
		incremented, err := e.incrementContext(msg, node.ID, stringConfig(config, "scope"), stringConfig(config, "key"), amount)
		if err != nil {
			return "", msg, false, err
		}
		msg.Value = incremented
	case "core.set-time-context":
		timestamp, err := configuredOrMessageTime(msg, config, stringConfig(config, "source"), "field", "time", e.services.Now)
		if err != nil {
			return "", msg, false, err
		}
		e.setContext(msg, node.ID, "script", stringConfig(config, "key"), timestamp)
	case "core.get-time-context":
		contextValue, ok := e.getContext(msg, node.ID, "script", stringConfig(config, "key"))
		if !ok {
			return "", msg, false, errors.New("time variable does not exist")
		}
		timestamp, err := timestampMillis(contextValue)
		if err != nil {
			return "", msg, false, fmt.Errorf("stored time variable: %w", err)
		}
		setSelectedValue(&msg, stringConfig(config, "outputField"), timestamp)
	case "core.set-tag":
		if e.services.SetTag == nil {
			return "", msg, true, errors.New("Set Tag service is unavailable")
		}
		value := actionValue(msg, config)
		if err := e.services.SetTag(ctx, msg, node.ID, stringConfig(config, "tagPath"), value); err != nil {
			return "", msg, true, err
		}
		return "out", msg, true, nil
	case "core.send-control":
		if e.services.SendControl == nil {
			return "", msg, true, errors.New("Send Control service is unavailable")
		}
		timeoutSeconds, err := configFiniteFloat(config, "timeout", 10)
		if err != nil || timeoutSeconds <= 0 {
			return "", msg, true, errors.New("control timeout must be greater than zero")
		}
		if err := e.services.SendControl(ctx, msg, node.ID, stringConfig(config, "deviceName"), stringConfig(config, "tagPath"), actionValue(msg, config), time.Duration(timeoutSeconds*float64(time.Second))); err != nil {
			return "", msg, true, err
		}
		return "out", msg, true, nil
	case "core.send-notification":
		if e.services.SendNotification == nil {
			return "", msg, true, errors.New("Send Notification service is unavailable")
		}
		if err := e.services.SendNotification(ctx, msg, node.ID, stringConfig(config, "profile"), stringConfig(config, "severity"), stringConfig(config, "message"), stringConfig(config, "device")); err != nil {
			return "", msg, true, err
		}
		return "out", msg, true, nil
	case "core.log-event":
		if e.services.LogEvent == nil {
			return "", msg, true, errors.New("Log Event service is unavailable")
		}
		if err := e.services.LogEvent(ctx, msg, node.ID, stringConfig(config, "severity"), stringConfig(config, "message"), stringConfig(config, "device")); err != nil {
			return "", msg, true, err
		}
		return "out", msg, true, nil
	case "core.debug":
		return "out", msg, true, nil
	default:
		return "", msg, false, fmt.Errorf("node type %q cannot execute", node.Type)
	}
	return "out", msg, false, nil
}

func actionValue(msg Message, config map[string]any) any {
	if stringConfig(config, "source") == "configured" {
		return config["value"]
	}
	return msg.Value
}

func transformNumber(nodeType string, value any, config map[string]any) (float64, error) {
	n, ok := finiteFloat(value)
	if !ok {
		return 0, errors.New("transform requires a finite numeric value")
	}
	var result float64
	switch nodeType {
	case "core.multiply":
		factor, err := configFiniteFloat(config, "factor", 1)
		if err != nil {
			return 0, err
		}
		result = n * factor
	case "core.divide":
		divisor, err := configFiniteFloat(config, "divisor", 1)
		if err != nil {
			return 0, err
		}
		if divisor == 0 {
			return 0, errors.New("division by zero")
		}
		result = n / divisor
	case "core.clamp":
		minimum, err := configFiniteFloat(config, "minimum", 0)
		if err != nil {
			return 0, err
		}
		maximum, err := configFiniteFloat(config, "maximum", 100)
		if err != nil {
			return 0, err
		}
		if minimum > maximum {
			return 0, errors.New("clamp maximum must be greater than or equal to minimum")
		}
		result = math.Max(minimum, math.Min(maximum, n))
	case "core.scale":
		inputMin, err := configFiniteFloat(config, "inputMin", 0)
		if err != nil {
			return 0, err
		}
		inputMax, err := configFiniteFloat(config, "inputMax", 100)
		if err != nil {
			return 0, err
		}
		outputMin, err := configFiniteFloat(config, "outputMin", 0)
		if err != nil {
			return 0, err
		}
		outputMax, err := configFiniteFloat(config, "outputMax", 1)
		if err != nil {
			return 0, err
		}
		if inputMin == inputMax {
			return 0, errors.New("input range has zero width")
		}
		result = outputMin + (n-inputMin)*(outputMax-outputMin)/(inputMax-inputMin)
	}
	if math.IsNaN(result) || math.IsInf(result, 0) {
		return 0, errors.New("numeric overflow")
	}
	return result, nil
}

func compareValues(left, right any, operator string) (bool, error) {
	if l, ok := finiteFloat(left); ok {
		if r, rightOK := finiteFloat(right); rightOK {
			switch operator {
			case "<":
				return l < r, nil
			case "<=":
				return l <= r, nil
			case "==":
				return l == r, nil
			case "!=":
				return l != r, nil
			case ">=":
				return l >= r, nil
			case ">":
				return l > r, nil
			}
		}
	}
	if operator == "==" {
		return reflect.DeepEqual(left, right), nil
	}
	if operator == "!=" {
		return !reflect.DeepEqual(left, right), nil
	}
	leftString, leftOK := left.(string)
	rightString, rightOK := right.(string)
	if leftOK && rightOK {
		switch operator {
		case "<":
			return leftString < rightString, nil
		case "<=":
			return leftString <= rightString, nil
		case ">=":
			return leftString >= rightString, nil
		case ">":
			return leftString > rightString, nil
		}
	}
	return false, errors.New("comparison operands are incompatible")
}

func asFloat(value any) (float64, bool) {
	switch n := value.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	case int16:
		return float64(n), true
	case int8:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint64:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint8:
		return float64(n), true
	case json.Number:
		v, err := n.Float64()
		return v, err == nil
	default:
		return 0, false
	}
}

func finiteFloat(value any) (float64, bool) {
	number, ok := asFloat(value)
	return number, ok && !math.IsNaN(number) && !math.IsInf(number, 0)
}

func configFiniteFloat(config map[string]any, key string, defaultValue float64) (float64, error) {
	value, exists := config[key]
	if !exists {
		return defaultValue, nil
	}
	number, ok := finiteFloat(value)
	if !ok {
		return 0, fmt.Errorf("%s must be a finite number", key)
	}
	return number, nil
}

func selectedValue(msg Message, field string) (any, error) {
	if field == "" {
		return msg.Value, nil
	}
	value, ok := getField(msg.Fields, field)
	if !ok {
		return nil, fmt.Errorf("field %q does not exist", field)
	}
	return value, nil
}

func setSelectedValue(msg *Message, field string, value any) {
	if field == "" {
		msg.Value = value
		return
	}
	if msg.Fields == nil {
		msg.Fields = make(map[string]any)
	}
	setField(msg.Fields, field, value)
}

func getField(fields map[string]any, path string) (any, bool) {
	parts := strings.Split(path, ".")
	var current any = fields
	for _, part := range parts {
		mapping, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = mapping[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func setField(fields map[string]any, path string, value any) {
	parts := strings.Split(path, ".")
	current := fields
	for _, part := range parts[:len(parts)-1] {
		next, ok := current[part].(map[string]any)
		if !ok {
			next = make(map[string]any)
			current[part] = next
		}
		current = next
	}
	current[parts[len(parts)-1]] = value
}

func cloneFields(fields map[string]any) map[string]any {
	if fields == nil {
		return nil
	}
	copy := make(map[string]any, len(fields))
	for key, value := range fields {
		copy[key] = cloneValue(value)
	}
	return copy
}

func cloneMessage(message Message) Message {
	copy := message
	copy.Value = cloneValue(message.Value)
	copy.Fields = cloneFields(message.Fields)
	return copy
}

func cloneValue(value any) any {
	switch item := value.(type) {
	case map[string]any:
		return cloneFields(item)
	case []any:
		copy := make([]any, len(item))
		for i := range item {
			copy[i] = cloneValue(item[i])
		}
		return copy
	default:
		return value
	}
}

func stringConfig(config map[string]any, key string) string {
	value, _ := config[key].(string)
	return value
}

func boolPort(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func scriptKey(org, scriptID string) string { return org + "\x00" + scriptID }
func instanceRuntimeKey(org, scriptID string, revision int, instanceKey string) string {
	return scriptKey(org, scriptID) + fmt.Sprintf("\x00%d\x00%s", revision, instanceKey)
}

func contextKey(msg Message, nodeID, scope, key string) string {
	instanceKey := msg.InstanceKey
	if instanceKey == "" {
		instanceKey = "default"
	}
	prefix := instanceRuntimeKey(msg.OrgName, msg.ScriptID, msg.ActiveRevision, instanceKey) + "\x00"
	if scope == "node" {
		prefix += nodeID + "\x00"
	} else {
		prefix += "script\x00"
	}
	return prefix + key
}

func (e *Engine) getContext(msg Message, nodeID, scope, key string) (any, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	value, ok := e.context[contextKey(msg, nodeID, scope, key)]
	return cloneValue(value), ok
}
func (e *Engine) setContext(msg Message, nodeID, scope, key string, value any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.context[contextKey(msg, nodeID, scope, key)] = cloneValue(value)
}
func (e *Engine) deleteContext(msg Message, nodeID, scope, key string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.context, contextKey(msg, nodeID, scope, key))
}
func (e *Engine) incrementContext(msg Message, nodeID, scope, key string, amount float64) (float64, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	fullKey := contextKey(msg, nodeID, scope, key)
	current := 0.0
	if value, exists := e.context[fullKey]; exists {
		var ok bool
		current, ok = asFloat(value)
		if !ok {
			return 0, errors.New("context value is not numeric")
		}
	}
	current += amount
	e.context[fullKey] = current
	return current, nil
}

func (e *Engine) clearContextLocked(org, scriptID string) {
	prefix := scriptKey(org, scriptID) + "\x00"
	for key := range e.context {
		if strings.HasPrefix(key, prefix) {
			delete(e.context, key)
		}
	}
}

func newID(prefix string) string {
	var bytes [12]byte
	_, _ = rand.Read(bytes[:])
	return prefix + "_" + hex.EncodeToString(bytes[:])
}
