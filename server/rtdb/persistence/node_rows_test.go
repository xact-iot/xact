package persistence

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xact-iot/xact/rtdb/tree"
	"github.com/xact-iot/xact/sqldb"
	"github.com/xact-iot/xact/sqldb/sqlite"
)

type recordingNodeDB struct {
	sqldb.DB
	store            sqldb.TreeConfigStore
	mu               sync.Mutex
	batches          []sqldb.TreeConfigBatch
	fail             bool
	entered, release chan struct{}
}

func (db *recordingNodeDB) LoadTreeNodes(ctx context.Context, org string) ([]sqldb.TreeConfigNode, bool, error) {
	return db.store.LoadTreeNodes(ctx, org)
}

func (db *recordingNodeDB) SaveTreeNodes(ctx context.Context, org string, batch sqldb.TreeConfigBatch) error {
	db.mu.Lock()
	db.batches = append(db.batches, batch)
	fail, entered, release := db.fail, db.entered, db.release
	db.fail = false
	db.mu.Unlock()
	if entered != nil {
		entered <- struct{}{}
		<-release
	}
	if fail {
		return fmt.Errorf("temporary node database failure")
	}
	return db.store.SaveTreeNodes(ctx, org, batch)
}

func (db *recordingNodeDB) lastBatch() sqldb.TreeConfigBatch {
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.batches[len(db.batches)-1]
}

func nodeTestDB(t *testing.T) *recordingNodeDB {
	t.Helper()
	db, err := sqlite.NewSQLiteDB(context.Background(), filepath.Join(t.TempDir(), "tree.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return &recordingNodeDB{DB: db, store: db.(sqldb.TreeConfigStore)}
}

func checked(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func watchNodeManager(t *testing.T, db *recordingNodeDB) (*Manager, *tree.TreeWithOperations) {
	t.Helper()
	ops := tree.NewTreeWithOperations(nil)
	mgr := NewManager(db, ops, "default", time.Hour)
	_, err := mgr.Restore(context.Background())
	checked(t, err)
	ops.SetOnStructureChange(mgr.MarkStructureDirty)
	t.Cleanup(func() { checked(t, mgr.Stop(context.Background())) })
	return mgr, ops
}

func rowsByPath(t *testing.T, db *recordingNodeDB) map[string]sqldb.TreeConfigNode {
	t.Helper()
	rows, initialized, err := db.LoadTreeNodes(context.Background(), "default")
	checked(t, err)
	if !initialized {
		t.Fatal("node snapshot not initialized")
	}
	result := map[string]sqldb.TreeConfigNode{}
	for _, row := range rows {
		result[row.Path] = row
	}
	return result
}

func TestNodeManagerMigratesLegacyAndSavesOnlyChangedGroup(t *testing.T) {
	db := nodeTestDB(t)
	legacy := tree.NewTreeWithOperations(nil)
	checked(t, legacy.CreateNode("default.static.coordinates", ""))
	for i := 0; i < 400; i++ {
		name := fmt.Sprintf("p%d", i)
		checked(t, legacy.CreateTag("default.static.coordinates."+name, tree.TypeFloat, tree.TagConfig{Name: name}))
	}
	checked(t, legacy.CreateDeviceNode("default.BUSES.18303", ""))
	config, err := SerializeTree(legacy.Root)
	checked(t, err)
	data, err := json.Marshal(config)
	checked(t, err)
	checked(t, db.SaveConfig(context.Background(), "default", configName, data))
	mgr, ops := watchNodeManager(t, db)
	if !db.lastBatch().Replace {
		t.Fatal("legacy migration was not atomic replacement")
	}
	before := rowsByPath(t, db)
	leaf, err := ops.FindLeaf("default.BUSES.18303.meta.lat")
	checked(t, err)
	shared := leaf.GetShared()
	shared.Description = "Latitude"
	shared.Units = "degrees"
	shared.Deadband = 0.001
	leaf.SetShared(shared)
	for i := 0; i < 10; i++ {
		ops.NotifyChange("/default/BUSES/18303/meta/lat", leaf)
	}
	checked(t, mgr.Save(context.Background()))
	batch := db.lastBatch()
	if batch.Replace || len(batch.Upserts) != 1 || batch.Upserts[0].Path != "default.BUSES.18303.meta" {
		t.Fatalf("expected one incremental group, got %+v", batch)
	}
	after := rowsByPath(t, db)
	if !reflect.DeepEqual(before["default.static.coordinates"], after["default.static.coordinates"]) {
		t.Fatal("unchanged static group rewritten")
	}
	old, err := db.LoadConfig(context.Background(), "default", configName)
	checked(t, err)
	if !bytes.Equal(old, data) {
		t.Fatal("legacy rollback snapshot overwritten")
	}
	count := len(db.batches)
	checked(t, ops.SetLeafValue("default.BUSES.18303.meta.lat", 49.5))
	checked(t, mgr.Save(context.Background()))
	if len(db.batches) != count {
		t.Fatal("live value update triggered configuration save")
	}
	restored := tree.NewTreeWithOperations(nil)
	_, err = NewManager(db, restored, "default", time.Hour).Restore(context.Background())
	checked(t, err)
	rleaf, err := restored.FindLeaf("default.BUSES.18303.meta.lat")
	checked(t, err)
	if rleaf.GetShared().Description != "Latitude" || rleaf.GetShared().Deadband != 0.001 {
		t.Fatal("tag configuration did not restore")
	}
}

func TestNodeManagerDeleteRecreateRenameAndEmptyTree(t *testing.T) {
	db := nodeTestDB(t)
	mgr, ops := watchNodeManager(t, db)
	checked(t, ops.CreateDeviceNode("default.BUSES.bus_1", ""))
	checked(t, ops.CreateNode("default.BUSES.bus_1.old.child", ""))
	checked(t, ops.CreateDeviceNode("default.BUSES.bus_10", ""))
	checked(t, mgr.Save(context.Background()))
	checked(t, ops.DeleteNode("default.BUSES.bus_1"))
	checked(t, ops.CreateDeviceNode("default.BUSES.bus_1", ""))
	checked(t, mgr.Save(context.Background()))
	rows := rowsByPath(t, db)
	for path := range rows {
		if strings.Contains(path, "bus_1.old") {
			t.Fatal("obsolete descendant survived recreation")
		}
	}
	if _, found := rows["default.BUSES.bus_10"]; !found {
		t.Fatal("sibling deleted by prefix collision")
	}
	if len(db.lastBatch().DeletePaths) != 1 {
		t.Fatalf("cascade tombstones not coalesced: %v", db.lastBatch().DeletePaths)
	}
	checked(t, ops.DeleteTag("default.BUSES.bus_1.meta.lat"))
	checked(t, mgr.Save(context.Background()))
	var leaves []LeafConfig
	checked(t, json.Unmarshal(rowsByPath(t, db)["default.BUSES.bus_1.meta"].Tags, &leaves))
	for _, leaf := range leaves {
		if leaf.Name == "lat" {
			t.Fatal("deleted leaf retained in containing node")
		}
	}
	checked(t, ops.RenameChild("default.BUSES", "bus_1", "renamed"))
	checked(t, mgr.Save(context.Background()))
	rows = rowsByPath(t, db)
	if _, found := rows["default.BUSES.bus_1"]; found {
		t.Fatal("old renamed path survived")
	}
	if _, found := rows["default.BUSES.renamed.meta"]; !found {
		t.Fatal("renamed descendant missing")
	}
	// An empty initialized snapshot must never fall back to an older blob.
	checked(t, db.SaveConfig(context.Background(), "default", configName, json.RawMessage(`{"nodes":[{"path":".stale"}]}`)))
	checked(t, ops.DeleteNode("default"))
	checked(t, mgr.Stop(context.Background()))
	if len(rowsByPath(t, db)) != 0 {
		t.Fatal("whole-tree deletion left rows")
	}
	restored := tree.NewTreeWithOperations(nil)
	ok, err := NewManager(db, restored, "default", time.Hour).Restore(context.Background())
	checked(t, err)
	if !ok || len(restored.Root.GetChildren()) != 0 {
		t.Fatal("empty tree resurrected legacy blob")
	}
}

func TestNodeManagerRetainsNewEditsAndFailedDeletionDuringSave(t *testing.T) {
	db := nodeTestDB(t)
	mgr, ops := watchNodeManager(t, db)
	checked(t, ops.CreateNode("default.old.child", ""))
	checked(t, mgr.Save(context.Background()))
	checked(t, ops.DeleteNode("default.old"))
	db.mu.Lock()
	db.fail = true
	db.entered = make(chan struct{}, 1)
	db.release = make(chan struct{}, 1)
	db.mu.Unlock()
	finished := make(chan error, 1)
	go func() { finished <- mgr.Save(context.Background()) }()
	<-db.entered
	checked(t, ops.CreateNode("default.old.new", ""))
	db.release <- struct{}{}
	if err := <-finished; err == nil {
		t.Fatal("expected first save failure")
	}
	db.mu.Lock()
	db.entered = nil
	db.release = nil
	db.mu.Unlock()
	checked(t, mgr.Stop(context.Background()))
	rows := rowsByPath(t, db)
	if _, found := rows["default.old.child"]; found {
		t.Fatal("failed delete lost on retry")
	}
	if _, found := rows["default.old.new"]; !found {
		t.Fatal("edit arriving during failed save lost")
	}
	if db.lastBatch().Replace {
		t.Fatal("retry fell back to whole-tree save")
	}
}

func TestNodeManagerRetainsEditsDuringSuccessfulSave(t *testing.T) {
	db := nodeTestDB(t)
	mgr, ops := watchNodeManager(t, db)
	checked(t, ops.CreateNode("default.first", ""))
	checked(t, mgr.Save(context.Background()))
	checked(t, ops.CreateNode("default.second", ""))
	db.mu.Lock()
	db.entered = make(chan struct{}, 1)
	db.release = make(chan struct{}, 1)
	db.mu.Unlock()
	finished := make(chan error, 1)
	go func() { finished <- mgr.Save(context.Background()) }()
	<-db.entered
	checked(t, ops.CreateNode("default.third", ""))
	db.release <- struct{}{}
	checked(t, <-finished)
	db.mu.Lock()
	db.entered = nil
	db.release = nil
	db.mu.Unlock()
	checked(t, mgr.Stop(context.Background()))
	if _, found := rowsByPath(t, db)["default.third"]; !found {
		t.Fatal("change arriving during successful save lost")
	}
	if batch := db.lastBatch(); len(batch.Upserts) != 1 || batch.Upserts[0].Path != "default.third" {
		t.Fatal("next batch included unrelated nodes")
	}
}

func TestNodeRowsRestoreTemplatesArraysAndLocks(t *testing.T) {
	db := nodeTestDB(t)
	mgr, ops := watchNodeManager(t, db)
	checked(t, ops.CreateNode("default.Templates.Bus.env", ""))
	checked(t, ops.CreateTag("default.Templates.Bus.env.mode", tree.TypeEnum,
		tree.TagConfig{Name: "mode"}, tree.TagShared{Description: "Mode", EnumValues: map[int]string{1: "running", 2: "stopped"}}))
	checked(t, ops.CreateDeviceNode("default.BUSES.18303", "Templates.Bus"))
	checked(t, ops.CreateNode("default.BUSES.18303.samples", ""))
	array, err := ops.FindNode("default.BUSES.18303.samples")
	checked(t, err)
	array.SetIsArray(true)
	ops.NotifyChange("default.BUSES.18303.samples", array)
	checked(t, ops.LockNode("default.BUSES.18303"))
	device, err := ops.FindNode("default.BUSES.18303")
	checked(t, err)
	ops.NotifyChange("default.BUSES.18303", device)
	checked(t, mgr.Save(context.Background()))
	restored := tree.NewTreeWithOperations(nil)
	ok, err := NewManager(db, restored, "default", time.Hour).Restore(context.Background())
	checked(t, err)
	if !ok {
		t.Fatal("node rows not restored")
	}
	rdevice, err := restored.FindNode("default.BUSES.18303")
	checked(t, err)
	if !rdevice.IsLocked() || rdevice.GetNodeType() != tree.NodeTypeDevice || rdevice.GetTemplateName() != "Templates.Bus" {
		t.Fatal("device metadata lost")
	}
	rarray, err := restored.FindNode("default.BUSES.18303.samples")
	checked(t, err)
	if !rarray.GetIsArray() {
		t.Fatal("array flag lost")
	}
	instance, err := restored.FindLeaf("default.BUSES.18303.env.mode")
	checked(t, err)
	template, err := restored.FindLeaf("default.Templates.Bus.env.mode")
	checked(t, err)
	if instance.GetTemplate() != template || instance.GetConfig().TemplateName != "Templates.Bus" {
		t.Fatal("template link lost")
	}
	if template.GetShared().EnumValues[2] != "stopped" {
		t.Fatal("enum definition lost")
	}
}
