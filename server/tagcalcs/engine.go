// Package tagcalcs implements the tag calc engine - periodic expression
// evaluation that writes computed values back into the RTDB tree.
package tagcalcs

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/expr-lang/expr/vm"
	xnats "github.com/xact-iot/xact/rtdb/nats"
	"github.com/xact-iot/xact/rtdb/tree"
	"github.com/xact-iot/xact/sqldb"
)

// compiledScript holds a loaded script together with its compiled bytecode.
type compiledScript struct {
	script  sqldb.TagCalc
	program *vm.Program
	lock    xnats.PubLock
}

// Engine loads tag calcs from the database, compiles their expressions, and
// schedules periodic evaluation. Each evaluation writes the result back into
// the RTDB tree as a regular tag value.
type Engine struct {
	db      sqldb.DB
	treeOps *tree.TreeWithOperations

	mu      sync.Mutex
	scripts map[int]*compiledScript // keyed by script ID
	timers  map[int]*time.Timer
}

// New creates a new Engine. Call Load to start it.
func New(db sqldb.DB, treeOps *tree.TreeWithOperations) *Engine {
	return &Engine{
		db:      db,
		treeOps: treeOps,
		scripts: make(map[int]*compiledScript),
		timers:  make(map[int]*time.Timer),
	}
}

// Load fetches all scripts for every organisation and schedules them.
// Safe to call multiple times (e.g. after a reload).
func (e *Engine) Load(ctx context.Context) error {
	orgs, err := e.db.ListOrganisations(ctx)
	if err != nil {
		return fmt.Errorf("tagcalcs: list orgs: %w", err)
	}
	for _, org := range orgs {
		scripts, err := e.db.ListTagCalcs(ctx, org.Name)
		if err != nil {
			log.Printf("tagcalcs: list scripts for %q: %v", org.Name, err)
			continue
		}
		for _, s := range scripts {
			if err := e.schedule(s); err != nil {
				log.Printf("tagcalcs: schedule %q/%q: %v", org.Name, s.Name, err)
			}
		}
	}
	return nil
}

// Reload reloads a single script (called after CRUD operations).
func (e *Engine) Reload(ctx context.Context, orgName string, scriptID int) {
	e.unschedule(scriptID)

	s, err := e.db.GetTagCalc(ctx, orgName, scriptID)
	if err != nil || s == nil {
		return
	}
	if err := e.schedule(*s); err != nil {
		log.Printf("tagcalcs: reload %q/%q: %v", orgName, s.Name, err)
	}
}

// Unschedule stops and removes a script (called on delete).
func (e *Engine) Unschedule(id int) {
	e.unschedule(id)
}

// schedule compiles a script and starts its evaluation timer.
func (e *Engine) schedule(s sqldb.TagCalc) error {
	prog, err := compileExpression(s.Expression)
	if err != nil {
		return err
	}

	lockKey := fmt.Sprintf("tagcalc.%s.%d.eval", s.OrgName, s.ID)
	cs := &compiledScript{
		script:  s,
		program: prog,
		lock:    xnats.NewPubLock(xnats.SubjectName(lockKey)),
	}

	e.mu.Lock()
	e.scripts[s.ID] = cs
	e.mu.Unlock()

	if s.Enabled {
		e.startTimer(cs)
	}
	return nil
}

func (e *Engine) unschedule(id int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if t, ok := e.timers[id]; ok {
		t.Stop()
		delete(e.timers, id)
	}
	delete(e.scripts, id)
}

func (e *Engine) startTimer(cs *compiledScript) {
	interval := time.Duration(cs.script.IntervalSeconds) * time.Second
	if interval <= 0 {
		interval = 60 * time.Second
	}
	t := time.AfterFunc(interval, func() {
		e.evaluate(cs)
		// Reschedule after evaluation - check under lock, then call startTimer outside it.
		e.mu.Lock()
		_, still := e.scripts[cs.script.ID]
		e.mu.Unlock()
		if still {
			e.startTimer(cs)
		}
	})
	e.mu.Lock()
	e.timers[cs.script.ID] = t
	e.mu.Unlock()
}

// evaluate runs the compiled expression and writes the result to the output tag.
func (e *Engine) evaluate(cs *compiledScript) {
	// Cluster de-duplication: only one server instance evaluates each script.
	rev, err := cs.lock.TryLock()
	if err != nil {
		return
	}
	defer cs.lock.Release(rev)

	env := &runtimeEnv{treeOps: e.treeOps, org: cs.script.OrgName}
	v := vm.VM{}
	out, err := v.Run(cs.program, env)
	if err != nil {
		log.Printf("tagcalcs: evaluate %q/%q: %v", cs.script.OrgName, cs.script.Name, err)
		return
	}

	outputPath := dotToSlash(cs.script.OrgName, cs.script.OutputTag)

	var writeErr error
	switch result := out.(type) {
	case []ListEntry:
		writeErr = e.writeListOutput(outputPath, result)
	default:
		var numeric float64
		numeric, writeErr = normaliseResult(out)
		if writeErr == nil {
			writeErr = e.writeOutput(outputPath, numeric)
		}
	}
	if writeErr != nil {
		log.Printf("tagcalcs: write %q → %q: %v", cs.script.Name, outputPath, writeErr)
	}
}

// writeOutput creates the output tag (if absent) and sets its value.
func (e *Engine) writeOutput(path string, value float64) error {
	if _, err := e.treeOps.FindLeaf(path); err != nil {
		if _, nodeErr := e.treeOps.FindNode(path); nodeErr == nil {
			return fmt.Errorf("output path %q is a node, not a numeric tag", path)
		}
		// Tag does not exist yet - create it once.
		parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
		name := parts[len(parts)-1]
		if err := e.treeOps.CreateTag(path, tree.TypeFloat, tree.TagConfig{Name: name}); err != nil {
			return fmt.Errorf("create output tag: %w", err)
		}
	}
	return e.treeOps.SetLeafValue(path, value)
}

func (e *Engine) writeListOutput(path string, entries []ListEntry) error {
	if _, err := e.treeOps.FindLeaf(path); err == nil {
		return fmt.Errorf("output path %q is a tag, not an array node", path)
	}

	if err := e.treeOps.CreateNode(path, ""); err != nil {
		return fmt.Errorf("create output array node: %w", err)
	}
	node, err := e.treeOps.FindNode(path)
	if err != nil {
		return fmt.Errorf("find output array node: %w", err)
	}
	if !node.GetIsArray() {
		node.SetIsArray(true)
		e.treeOps.NotifyChange(path, node)
	}

	for i, entry := range entries {
		elementPath := path + "/" + strconv.Itoa(i)
		if err := e.treeOps.CreateNode(elementPath, ""); err != nil {
			return fmt.Errorf("create list element %d: %w", i, err)
		}
		if err := e.writeStringField(elementPath+"/deviceName", entry.DeviceName); err != nil {
			return err
		}
		if err := e.writeStringField(elementPath+"/deviceDescriptor", entry.DeviceDescriptor); err != nil {
			return err
		}
		if err := e.writeStringField(elementPath+"/tagName", entry.TagName); err != nil {
			return err
		}
		if err := e.writeFloatField(elementPath+"/tagValue", entry.TagValue); err != nil {
			return err
		}
	}

	return e.pruneListOutput(path, len(entries))
}

func (e *Engine) writeStringField(path, value string) error {
	return e.writeScalarField(path, tree.TypeString, value)
}

func (e *Engine) writeFloatField(path string, value float64) error {
	return e.writeScalarField(path, tree.TypeFloat, value)
}

func (e *Engine) writeScalarField(path string, scalarType tree.ScalarType, value any) error {
	if node, err := e.treeOps.FindNodeOrLeaf(path); err == nil {
		if leaf, ok := node.(tree.Leaf); ok {
			if leaf.ValueType() == scalarType {
				return e.treeOps.SetLeafValue(path, value)
			}
			if err := e.treeOps.DeleteTag(path); err != nil {
				return fmt.Errorf("replace list field %q: %w", path, err)
			}
		} else if node.IsNode() {
			if err := e.treeOps.DeleteNode(path); err != nil {
				return fmt.Errorf("replace list field node %q: %w", path, err)
			}
		}
	}

	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	name := parts[len(parts)-1]
	if err := e.treeOps.CreateTag(path, scalarType, tree.TagConfig{Name: name}); err != nil {
		return fmt.Errorf("create list field %q: %w", path, err)
	}
	return e.treeOps.SetLeafValue(path, value)
}

func (e *Engine) pruneListOutput(path string, keep int) error {
	node, err := e.treeOps.FindNode(path)
	if err != nil {
		return err
	}
	children := node.GetChildren()
	names := make([]string, 0, len(children))
	for name := range children {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		idx, err := strconv.Atoi(name)
		if err != nil || idx < keep {
			continue
		}
		childPath := path + "/" + name
		if child, ok := children[name]; ok && child.IsNode() {
			if err := e.treeOps.DeleteNode(childPath); err != nil {
				return err
			}
		} else {
			if err := e.treeOps.DeleteTag(childPath); err != nil {
				return err
			}
		}
	}
	return nil
}

// dotToSlash converts an org-relative dot-notation output tag path to a tree path.
func dotToSlash(org, dotPath string) string {
	return "/" + org + "/" + strings.ReplaceAll(dotPath, ".", "/")
}

// EvaluateNow evaluates a script immediately and returns the result without
// writing to the tree. Used by the test endpoint.
func (e *Engine) EvaluateNow(orgName, expression string) (float64, error) {
	out, err := e.EvaluateAny(orgName, expression)
	if err != nil {
		return 0, err
	}
	return normaliseResult(out)
}

// EvaluateAny evaluates a script immediately and returns the raw expression
// result. It is used to validate expressions that return non-numeric values
// such as listHighest/listLowest.
func (e *Engine) EvaluateAny(orgName, expression string) (any, error) {
	prog, err := compileExpression(expression)
	if err != nil {
		return 0, err
	}
	env := &runtimeEnv{treeOps: e.treeOps, org: orgName}
	v := vm.VM{}
	out, err := v.Run(prog, env)
	if err != nil {
		return 0, err
	}
	return out, nil
}

// Stop cancels all pending timers.
func (e *Engine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()
	for id, t := range e.timers {
		t.Stop()
		delete(e.timers, id)
	}
}
