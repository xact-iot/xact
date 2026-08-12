package sqlite_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/xact-iot/xact/sqldb/sqlite"
	"github.com/xact-iot/xact/visualscripts"
)

func TestVisualScriptRevisionDeployAndManualRun(t *testing.T) {
	ctx := context.Background()
	database, err := sqlite.NewSQLiteDB(ctx, filepath.Join(t.TempDir(), "visual-scripts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	store := database.(visualscripts.Store)
	engine := visualscripts.New(store)
	defer engine.Close()
	script := &visualscripts.Script{Name: "Temperature check", Description: "test", CreatedBy: 1, UpdatedBy: 1}
	if err := store.CreateVisualScript(ctx, "default", script); err != nil {
		t.Fatal(err)
	}
	graph := visualscripts.GraphDocument{Nodes: []visualscripts.GraphNode{
		{ID: "manual", Type: "core.manual", TypeVersion: 1, Config: json.RawMessage(`{}`)},
		{ID: "set", Type: "core.set-field", TypeVersion: 1, Config: json.RawMessage(`{"field":"temperature","value":21}`)},
		{ID: "compare", Type: "core.compare", TypeVersion: 1, Config: json.RawMessage(`{"field":"temperature","operator":">=","compareTo":20}`)},
		{ID: "debug", Type: "core.debug", TypeVersion: 1, Config: json.RawMessage(`{}`)},
	}, Edges: []visualscripts.GraphEdge{
		{ID: "e1", From: visualscripts.EdgeEndpoint{NodeID: "manual", Port: "out"}, To: visualscripts.EdgeEndpoint{NodeID: "set", Port: "in"}},
		{ID: "e2", From: visualscripts.EdgeEndpoint{NodeID: "set", Port: "out"}, To: visualscripts.EdgeEndpoint{NodeID: "compare", Port: "in"}},
		{ID: "e3", From: visualscripts.EdgeEndpoint{NodeID: "compare", Port: "true"}, To: visualscripts.EdgeEndpoint{NodeID: "debug", Port: "in"}},
	}}
	validation := engine.Validate(graph)
	if !validation.Valid {
		t.Fatalf("invalid fixture: %#v", validation.Diagnostics)
	}
	revision := &visualscripts.Revision{SchemaVersion: 1, Graph: validation.Graph, GraphHash: validation.GraphHash, ValidationStatus: "valid", Diagnostics: validation.Diagnostics, CreatedBy: 1}
	if err := store.CreateVisualScriptRevision(ctx, "default", script.ID, 0, revision); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateVisualScriptRevision(ctx, "default", script.ID, 0, &visualscripts.Revision{}); err != visualscripts.ErrConflict {
		t.Fatalf("expected conflict, got %v", err)
	}
	if err := store.SetVisualScriptOptions(ctx, "default", script.ID, true, true, 1); err != nil {
		t.Fatal(err)
	}
	if err := store.SetVisualScriptBackupRevision(ctx, "default", script.ID, &revision.Revision, 1); err != nil {
		t.Fatal(err)
	}
	stale := &visualscripts.Run{RunID: "run-before-restart", OrgName: "default", ScriptID: script.ID, ActiveRevision: revision.Revision, TriggerNodeID: "manual", InstanceKey: "Pump00", StartedAt: time.Now().Add(-time.Minute).UTC(), Status: "running"}
	if err := store.AppendVisualScriptRun(ctx, stale); err != nil {
		t.Fatal(err)
	}
	stored, err := store.GetVisualScript(ctx, "default", script.ID)
	if err != nil || stored == nil || !stored.Simulation || !stored.Activate || !stored.HasBackup {
		t.Fatalf("script options not retained: %#v, %v", stored, err)
	}
	if activationErrors := engine.StartActivated(ctx); len(activationErrors) != 0 {
		t.Fatalf("activated script failed to start: %v", activationErrors)
	}
	recovered, err := store.GetVisualScriptRun(ctx, "default", script.ID, stale.RunID)
	if err != nil || recovered != nil {
		t.Fatalf("run trace was not cleared when the activated script started: %#v, %v", recovered, err)
	}
	status, err := engine.Status(ctx, "default", script.ID)
	if err != nil {
		t.Fatal(err)
	}
	if status.RuntimeState != "running" {
		t.Fatalf("current script did not start: %#v", status)
	}
	run, err := engine.RunManual(ctx, "default", script.ID, visualscripts.RunRequest{Value: 0})
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "queued" || run.InstanceKey != "manual" {
		t.Fatalf("unexpected accepted run: %#v", run)
	}
	var detail *visualscripts.Run
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		detail, err = store.GetVisualScriptRun(ctx, "default", script.ID, run.RunID)
		if err != nil {
			t.Fatal(err)
		}
		if detail != nil && (detail.Status == "ok" || detail.Status == "error" || detail.Status == "cancelled") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if detail == nil || detail.Status != "ok" || detail.NodesExecuted != 4 || detail.InstanceKey != "manual" || len(detail.Trace) != 4 {
		t.Fatalf("trace not retained: %#v", detail)
	}
	other, err := store.GetVisualScript(ctx, "other", script.ID)
	if err != nil {
		t.Fatal(err)
	}
	if other != nil {
		t.Fatal("tenant lookup leaked a script")
	}
}
