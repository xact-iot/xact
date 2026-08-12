package visualscripts

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestInstanceQueuesEnforceLimitsAndIsolateWildcardDevices(t *testing.T) {
	graph := NormalizeGraph(GraphDocument{
		Settings: GraphSettings{MaxConcurrency: 1, QueueLimit: 1, ErrorPolicy: "stop-message", TraceLevel: "errors"},
		Nodes:    []GraphNode{{ID: "manual", Type: "core.manual", TypeVersion: 1, Config: json.RawMessage(`{}`)}},
	})
	revision := 1
	store := newRuntimeTestStore(Script{ID: "script", OrgName: "org", DesiredState: "running", LatestRevision: revision, ActiveRevision: &revision}, Revision{ScriptID: "script", OrgName: "org", Revision: revision, Graph: graph})
	engine := New(store)
	defer engine.Close()

	started := make(chan string, 4)
	engine.nodeHandler = func(ctx context.Context, node GraphNode, message Message) (string, Message, bool, error) {
		started <- message.InstanceKey
		<-ctx.Done()
		return "", message, false, ctx.Err()
	}

	first, err := engine.RunManual(context.Background(), "org", "script", RunRequest{InstanceKey: "Pump01"})
	if err != nil || first.Status != "queued" {
		t.Fatalf("first trigger = %#v, %v", first, err)
	}
	waitForInstanceStart(t, started, "Pump01")
	second, err := engine.RunManual(context.Background(), "org", "script", RunRequest{InstanceKey: "Pump01"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = engine.RunManual(context.Background(), "org", "script", RunRequest{InstanceKey: "Pump01"}); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("third Pump01 trigger error = %v, want queue full", err)
	}
	other, err := engine.RunManual(context.Background(), "org", "script", RunRequest{InstanceKey: "Pump02"})
	if err != nil {
		t.Fatal(err)
	}
	waitForInstanceStart(t, started, "Pump02")

	status, err := engine.Status(context.Background(), "org", "script")
	if err != nil || status.QueueDepth != 1 {
		t.Fatalf("queued status = %#v, %v", status, err)
	}
	if _, err = engine.SetDesiredState(context.Background(), "org", "script", "stopped"); err != nil {
		t.Fatal(err)
	}
	waitForRunStatus(t, store, first.RunID, "cancelled")
	waitForRunStatus(t, store, second.RunID, "cancelled")
	waitForRunStatus(t, store, other.RunID, "cancelled")
	status, err = engine.Status(context.Background(), "org", "script")
	if err != nil || status.QueueDepth != 0 {
		t.Fatalf("stopped status = %#v, %v", status, err)
	}
}

func TestScriptContextIsScopedToWildcardInstance(t *testing.T) {
	engine := New(newRuntimeTestStore(Script{}, Revision{}))
	defer engine.Close()
	first := Message{OrgName: "org", ScriptID: "script", ActiveRevision: 1, InstanceKey: "Pump01"}
	second := Message{OrgName: "org", ScriptID: "script", ActiveRevision: 1, InstanceKey: "Pump02"}
	engine.setContext(first, "node", "script", "attempts", 3.0)
	if value, ok := engine.getContext(first, "node", "script", "attempts"); !ok || value != 3.0 {
		t.Fatalf("first instance context = %#v, %v", value, ok)
	}
	if value, ok := engine.getContext(second, "node", "script", "attempts"); ok {
		t.Fatalf("second instance leaked context %#v", value)
	}
}

func TestTagChangeRunPropagatesResolvedInstanceContext(t *testing.T) {
	graph := NormalizeGraph(GraphDocument{
		Settings: GraphSettings{MaxConcurrency: 1, QueueLimit: 1, ErrorPolicy: "stop-message", TraceLevel: "errors"},
		Nodes:    []GraphNode{{ID: "trigger", Type: "core.manual", TypeVersion: 1, Config: json.RawMessage(`{}`)}},
	})
	revision := 1
	store := newRuntimeTestStore(Script{ID: "script", OrgName: "org", DesiredState: "running", LatestRevision: revision, ActiveRevision: &revision}, Revision{ScriptID: "script", OrgName: "org", Revision: revision, Graph: graph})
	engine := New(store)
	defer engine.Close()

	received := make(chan Message, 1)
	engine.nodeHandler = func(_ context.Context, _ GraphNode, message Message) (string, Message, bool, error) {
		received <- message
		return "out", message, false, nil
	}
	triggeredAt := time.Now().Add(-time.Second).UTC()
	run, err := engine.RunTagChange(context.Background(), "org", "script", "trigger", TagChange{
		InstanceKey: "AreaWest/Pump01",
		DevicePath:  "SITE.AreaWest.Pump01",
		TagPath:     "SITE.AreaWest.Pump01.Status.Running",
		Value:       true,
		Fields:      map[string]any{"quality": "good"},
		Timestamp:   triggeredAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case message := <-received:
		if message.InstanceKey != "AreaWest/Pump01" || message.DevicePath != "SITE.AreaWest.Pump01" || message.TagPath != "SITE.AreaWest.Pump01.Status.Running" {
			t.Fatalf("trigger context = %#v", message)
		}
		if !message.TriggerTimestamp.Equal(triggeredAt) || message.Value != true || message.Fields["quality"] != "good" {
			t.Fatalf("trigger payload = %#v", message)
		}
	case <-time.After(time.Second):
		t.Fatal("tag-change run did not start")
	}
	waitForRunStatus(t, store, run.RunID, "ok")
}

func TestClosedEngineRejectsNewTriggers(t *testing.T) {
	revision := 1
	graph := NormalizeGraph(GraphDocument{Nodes: []GraphNode{{ID: "manual", Type: "core.manual", TypeVersion: 1, Config: json.RawMessage(`{}`)}}})
	store := newRuntimeTestStore(Script{ID: "script", OrgName: "org", DesiredState: "running", LatestRevision: revision, ActiveRevision: &revision}, Revision{ScriptID: "script", OrgName: "org", Revision: revision, Graph: graph})
	engine := New(store)
	engine.Close()
	if _, err := engine.RunManual(context.Background(), "org", "script", RunRequest{}); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("closed engine trigger error = %v, want ErrNotRunning", err)
	}
}

func TestStartCurrentClearsPreviousRunTrace(t *testing.T) {
	revision := 1
	graph := NormalizeGraph(GraphDocument{Nodes: []GraphNode{{ID: "manual", Type: "core.manual", TypeVersion: 1, Config: json.RawMessage(`{}`)}}})
	store := newRuntimeTestStore(Script{ID: "script", OrgName: "org", DesiredState: "stopped", LatestRevision: revision}, Revision{ScriptID: "script", OrgName: "org", Revision: revision, Graph: graph})
	store.runs["old-run"] = Run{RunID: "old-run", OrgName: "org", ScriptID: "script", ActiveRevision: revision, Status: "ok"}
	engine := New(store)
	defer engine.Close()

	status, err := engine.StartCurrent(context.Background(), "org", "script")
	if err != nil {
		t.Fatal(err)
	}
	if status.RuntimeState != "running" {
		t.Fatalf("runtime state = %q, want running", status.RuntimeState)
	}
	if len(store.runs) != 0 {
		t.Fatalf("previous run trace was not cleared: %#v", store.runs)
	}
}

func waitForInstanceStart(t *testing.T, started <-chan string, want string) {
	t.Helper()
	select {
	case got := <-started:
		if got != want {
			t.Fatalf("started instance %q, want %q", got, want)
		}
	case <-time.After(time.Second):
		t.Fatalf("instance %q did not start", want)
	}
}

func waitForRunStatus(t *testing.T, store *runtimeTestStore, runID, want string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		store.mu.Lock()
		status := store.runs[runID].Status
		store.mu.Unlock()
		if status == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("run %s did not reach %s", runID, want)
}

type runtimeTestStore struct {
	mu       sync.Mutex
	script   Script
	revision Revision
	runs     map[string]Run
}

func newRuntimeTestStore(script Script, revision Revision) *runtimeTestStore {
	return &runtimeTestStore{script: script, revision: revision, runs: make(map[string]Run)}
}

func (s *runtimeTestStore) ListVisualScripts(context.Context, string) ([]Script, error) {
	return []Script{s.script}, nil
}
func (s *runtimeTestStore) ListActivatedVisualScripts(context.Context) ([]Script, error) {
	return nil, nil
}
func (s *runtimeTestStore) GetVisualScript(_ context.Context, org, id string) (*Script, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.script.OrgName != org || s.script.ID != id {
		return nil, nil
	}
	copy := s.script
	return &copy, nil
}
func (s *runtimeTestStore) CreateVisualScript(context.Context, string, *Script) error { return nil }
func (s *runtimeTestStore) UpdateVisualScript(context.Context, string, string, string, string, int) error {
	return nil
}
func (s *runtimeTestStore) DeleteVisualScript(context.Context, string, string) error { return nil }
func (s *runtimeTestStore) ListVisualScriptRevisions(context.Context, string, string) ([]Revision, error) {
	return []Revision{s.revision}, nil
}
func (s *runtimeTestStore) GetVisualScriptRevision(_ context.Context, org, id string, revision int) (*Revision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.revision.OrgName != org || s.revision.ScriptID != id || s.revision.Revision != revision {
		return nil, nil
	}
	copy := s.revision
	return &copy, nil
}
func (s *runtimeTestStore) CreateVisualScriptRevision(context.Context, string, string, int, *Revision) error {
	return nil
}
func (s *runtimeTestStore) SetVisualScriptActiveRevision(_ context.Context, org, id string, revision *int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.script.ActiveRevision = revision
	return nil
}
func (s *runtimeTestStore) SetVisualScriptDesiredState(_ context.Context, org, id, state string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.script.DesiredState = state
	return nil
}
func (s *runtimeTestStore) SetVisualScriptOptions(context.Context, string, string, bool, bool, int) error {
	return nil
}
func (s *runtimeTestStore) SetVisualScriptBackupRevision(context.Context, string, string, *int, int) error {
	return nil
}
func (s *runtimeTestStore) AppendVisualScriptRun(_ context.Context, run *Run) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs[run.RunID] = *run
	return nil
}
func (s *runtimeTestStore) CompleteVisualScriptRun(_ context.Context, run *Run) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs[run.RunID] = *run
	return nil
}
func (s *runtimeTestStore) CancelIncompleteVisualScriptRuns(context.Context, time.Time, string) error {
	return nil
}
func (s *runtimeTestStore) ClearVisualScriptRuns(_ context.Context, org, scriptID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for runID, run := range s.runs {
		if run.OrgName == org && run.ScriptID == scriptID {
			delete(s.runs, runID)
		}
	}
	return nil
}
func (s *runtimeTestStore) ListVisualScriptRuns(context.Context, string, string, int) ([]Run, error) {
	return nil, nil
}
func (s *runtimeTestStore) GetVisualScriptRun(_ context.Context, _, _, runID string) (*Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.runs[runID]
	if !ok {
		return nil, nil
	}
	return &run, nil
}
