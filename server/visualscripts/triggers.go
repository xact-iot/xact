package visualscripts

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
)

type edgeState struct {
	initialized bool
	value       bool
	generation  uint64
}

type triggerSession struct {
	cancel      context.CancelFunc
	unregisters []func()
}

func (e *Engine) startTriggers(org, scriptID string, revision int, plan *compiledPlan) {
	e.stopTriggers(org, scriptID)
	ctx, cancel := context.WithCancel(e.rootCtx)
	session := &triggerSession{cancel: cancel}
	e.triggerMu.Lock()
	e.triggers[scriptKey(org, scriptID)] = session
	e.triggerMu.Unlock()

	for _, node := range plan.graph.Nodes {
		node := node
		var config map[string]any
		if json.Unmarshal(node.Config, &config) != nil {
			continue
		}
		switch node.Type {
		case "core.startup":
			delay, _ := durationConfig(config, "delay", 0)
			go func() {
				if waitContext(ctx, delay) {
					e.runAutonomous(ctx, org, scriptID, node, "startup", TagChange{Timestamp: time.Now().UTC(), Fields: map[string]any{"trigger": "startup"}})
				}
			}()
		case "core.timer":
			interval, err := durationConfig(config, "interval", time.Minute)
			if err != nil || interval <= 0 {
				continue
			}
			initialDelay, _ := durationConfig(config, "initialDelay", 0)
			jitter, _ := durationConfig(config, "jitter", 0)
			go e.runTimer(ctx, org, scriptID, node, interval, initialDelay, jitter)
		case "core.tag-changed", "core.rising-edge", "core.falling-edge":
			pattern := stringConfig(config, "pathPattern")
			registrationID := fmt.Sprintf("%s/%s/%d/%s", org, scriptID, revision, node.ID)
			unregister, err := e.services.TagRouter.Register(org, registrationID, pattern, func(change TagChange) {
				if node.Type == "core.tag-changed" {
					go e.runAutonomous(ctx, org, scriptID, node, change.InstanceKey, change)
					return
				}
				e.handleEdgeChange(ctx, org, scriptID, revision, node, config, change)
			})
			if err == nil {
				e.triggerMu.Lock()
				if e.triggers[scriptKey(org, scriptID)] == session {
					session.unregisters = append(session.unregisters, unregister)
					unregister = nil
				}
				e.triggerMu.Unlock()
				if unregister != nil {
					unregister()
				}
			}
			triggerOnStart, _ := config["triggerOnStart"].(bool)
			if node.Type == "core.tag-changed" && triggerOnStart {
				go e.runTagChangedOnStart(ctx, org, scriptID, node, pattern)
			}
		}
	}
}

func (e *Engine) runTagChangedOnStart(ctx context.Context, org, scriptID string, node GraphNode, pattern string) {
	if e.services.ReadTags == nil || ctx.Err() != nil {
		return
	}
	changes, err := e.services.ReadTags(ctx, org, pattern)
	if err != nil {
		return
	}
	for _, change := range changes {
		if ctx.Err() != nil {
			return
		}
		if change.Value == nil {
			continue
		}
		e.runAutonomous(ctx, org, scriptID, node, change.InstanceKey, change)
	}
}

func (e *Engine) stopTriggers(org, scriptID string) {
	e.triggerMu.Lock()
	sessions := make([]*triggerSession, 0)
	for key, session := range e.triggers {
		if org == "" || key == scriptKey(org, scriptID) {
			delete(e.triggers, key)
			sessions = append(sessions, session)
		}
	}
	if org == "" {
		e.edgeStates = make(map[string]edgeState)
	} else {
		prefix := scriptKey(org, scriptID) + "\x00"
		for key := range e.edgeStates {
			if strings.HasPrefix(key, prefix) {
				delete(e.edgeStates, key)
			}
		}
	}
	e.triggerMu.Unlock()
	for _, session := range sessions {
		session.cancel()
		for _, unregister := range session.unregisters {
			unregister()
		}
	}
}

func (e *Engine) runTimer(ctx context.Context, org, scriptID string, node GraphNode, interval, initialDelay, jitter time.Duration) {
	delay := interval
	if initialDelay > 0 {
		delay = initialDelay
	}
	for waitContext(ctx, delay+jitterDuration(jitter)) {
		now := time.Now().UTC()
		e.runAutonomous(ctx, org, scriptID, node, "timer", TagChange{Timestamp: now, Fields: map[string]any{"trigger": "timer", "scheduledAt": now.Format(time.RFC3339Nano)}})
		delay = interval
	}
}

func (e *Engine) runAutonomous(ctx context.Context, org, scriptID string, node GraphNode, instanceKey string, change TagChange) {
	if ctx.Err() != nil {
		return
	}
	if strings.TrimSpace(instanceKey) == "" {
		instanceKey = node.ID
	}
	_, _ = e.enqueueTrigger(ctx, org, scriptID, RunRequest{TriggerNodeID: node.ID, InstanceKey: instanceKey, DevicePath: change.DevicePath, TagPath: change.TagPath, Value: change.Value, Fields: change.Fields}, node.Type, change.Timestamp)
}

func (e *Engine) handleEdgeChange(ctx context.Context, org, scriptID string, revision int, node GraphNode, config map[string]any, change TagChange) {
	value, ok := coerceBoolean(change.Value, stringConfig(config, "coercion"))
	if !ok {
		return
	}
	stateKey := fmt.Sprintf("%s\x00%d\x00%s\x00%s", scriptKey(org, scriptID), revision, node.ID, change.InstanceKey)
	e.triggerMu.Lock()
	state := e.edgeStates[stateKey]
	state.generation++
	generation := state.generation
	previous := state.value
	wasInitialized := state.initialized
	state.initialized = true
	state.value = value
	e.edgeStates[stateKey] = state
	e.triggerMu.Unlock()
	if !wasInitialized {
		return
	}
	targetTransition := (node.Type == "core.rising-edge" && !previous && value) || (node.Type == "core.falling-edge" && previous && !value)
	if !targetTransition {
		return
	}
	debounce, _ := durationConfig(config, "debounce", 0)
	go func() {
		if !waitContext(ctx, debounce) {
			return
		}
		e.triggerMu.Lock()
		latest := e.edgeStates[stateKey]
		stable := latest.generation == generation && latest.value == value
		e.triggerMu.Unlock()
		if stable {
			e.runAutonomous(ctx, org, scriptID, node, change.InstanceKey, change)
		}
	}()
}

func coerceBoolean(value any, policy string) (bool, bool) {
	if boolean, ok := value.(bool); ok {
		return boolean, true
	}
	switch policy {
	case "numeric":
		number, ok := finiteFloat(value)
		return number != 0, ok
	case "truthy":
		switch item := value.(type) {
		case nil:
			return false, true
		case string:
			return strings.TrimSpace(item) != "" && !strings.EqualFold(item, "false") && item != "0", true
		default:
			if number, ok := finiteFloat(item); ok {
				return number != 0, true
			}
			return true, true
		}
	default:
		return false, false
	}
}

func durationConfig(config map[string]any, key string, fallback time.Duration) (time.Duration, error) {
	value, ok := config[key]
	if !ok || strings.TrimSpace(fmt.Sprint(value)) == "" {
		return fallback, nil
	}
	return time.ParseDuration(fmt.Sprint(value))
}

func waitContext(ctx context.Context, duration time.Duration) bool {
	if duration <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func jitterDuration(maximum time.Duration) time.Duration {
	if maximum <= 0 {
		return 0
	}
	// Time-based phase mixing is sufficient for optional scheduling jitter; it
	// is not used for identifiers or security decisions.
	fraction := math.Mod(float64(time.Now().UnixNano())*0.61803398875, float64(maximum))
	if fraction < 0 {
		fraction = -fraction
	}
	return time.Duration(fraction)
}
