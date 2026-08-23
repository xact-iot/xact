package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/xact-iot/xact/rtdb/tree"
	"github.com/xact-iot/xact/visualscripts"
)

type visualScriptSnapshotPublisher struct{}

func (visualScriptSnapshotPublisher) TagValuePublish(string, []byte) error { return nil }

type visualScriptNotificationResolver struct {
	id  int
	err error
}

func (r visualScriptNotificationResolver) ResolveNotificationID(context.Context, string, string) (int, error) {
	return r.id, r.err
}

func TestResolveVisualScriptNotificationIDRejectsUnknownProfile(t *testing.T) {
	id, err := resolveVisualScriptNotificationID(context.Background(), visualScriptNotificationResolver{}, "acme", " Missing ")
	if id != 0 || err == nil || err.Error() != `notification profile "Missing" was not found` {
		t.Fatalf("resolve result = %d, %v", id, err)
	}
}

func TestResolveVisualScriptNotificationIDPropagatesLookupError(t *testing.T) {
	want := errors.New("database unavailable")
	id, err := resolveVisualScriptNotificationID(context.Background(), visualScriptNotificationResolver{err: want}, "acme", "Ops")
	if id != 0 || !errors.Is(err, want) {
		t.Fatalf("resolve result = %d, %v", id, err)
	}
}

func TestResolveVisualScriptNotificationIDReturnsExistingProfile(t *testing.T) {
	id, err := resolveVisualScriptNotificationID(context.Background(), visualScriptNotificationResolver{id: 42}, "acme", " Ops ")
	if id != 42 || err != nil {
		t.Fatalf("resolve result = %d, %v", id, err)
	}
}

func TestDispatchVisualScriptTagDecodesBroadcastValue(t *testing.T) {
	router := visualscripts.NewTagChangeRouter(10, 10)
	received := make(chan visualscripts.TagChange, 1)
	_, err := router.Register("acme", "test", "SITE.*.Status.Running", func(change visualscripts.TagChange) { received <- change })
	if err != nil {
		t.Fatal(err)
	}
	dispatchVisualScriptTag(router, "xact.internal.bcast.tagvalue.acme.SITE.Pump01.Status.Running", []byte(`{"Status.Running":{"value":true,"status":"good","timestamp":1750000000123}}`))
	select {
	case change := <-received:
		if change.OrgName != "acme" || change.TagPath != "SITE.Pump01.Status.Running" || change.DevicePath != "SITE" || change.InstanceKey != "Pump01" || change.Value != true {
			t.Fatalf("decoded change = %#v", change)
		}
		if change.Fields["status"] != "good" || !change.Timestamp.Equal(time.UnixMilli(1750000000123).UTC()) {
			t.Fatalf("decoded metadata = %#v", change)
		}
	case <-time.After(time.Second):
		t.Fatal("tag change was not dispatched")
	}
}

func TestTenantTagPathAlwaysUsesMessageOrganisation(t *testing.T) {
	path, err := tenantTagPath("acme", "/SITE/Pump01/Speed")
	if err != nil || path != "acme.SITE.Pump01.Speed" {
		t.Fatalf("tenant path = %q, %v", path, err)
	}
	if _, err := tenantTagPath("acme", "SITE.*.Speed"); err == nil {
		t.Fatal("Set Tag accepted a wildcard path")
	}
}

func TestVisualScriptTagSnapshotsReturnsDefinedMatchingValues(t *testing.T) {
	previousPublisher := tree.TagValuePublisher
	tree.TagValuePublisher = visualScriptSnapshotPublisher{}
	t.Cleanup(func() { tree.TagValuePublisher = previousPublisher })
	treeOps := tree.NewTreeWithOperations(nil)
	if err := treeOps.CreateNode("/acme/SITE/Pump01", ""); err != nil {
		t.Fatal(err)
	}
	if err := treeOps.UnlockNode("/acme/SITE/Pump01"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"level", "unset", "undefined"} {
		if err := treeOps.CreateTag("/acme/SITE/Pump01/"+name, tree.TypeFloat, tree.TagConfig{Name: name, Type: tree.TypeFloat}); err != nil {
			t.Fatal(err)
		}
	}
	if err := treeOps.SetLeafValue("acme.SITE.Pump01.level", float64(0)); err != nil {
		t.Fatal(err)
	}
	if err := treeOps.SetLeafValue("acme.SITE.Pump01.undefined", float64(12)); err != nil {
		t.Fatal(err)
	}
	undefined, err := treeOps.FindLeaf("acme.SITE.Pump01.undefined")
	if err != nil {
		t.Fatal(err)
	}
	undefined.SetState(tree.StatusUndefined)

	changes, err := visualScriptTagSnapshots(context.Background(), treeOps, "acme", "SITE.*.*")
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 {
		t.Fatalf("snapshots = %#v, want one defined value", changes)
	}
	change := changes[0]
	if change.TagPath != "SITE.Pump01.level" || change.InstanceKey != "Pump01/level" || change.Value != float64(0) || change.Fields["trigger"] != "start" {
		t.Fatalf("snapshot = %#v", change)
	}
}
