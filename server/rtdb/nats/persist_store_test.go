package nats

import (
	"context"
	"testing"
	"time"
)

func TestPersistStoreCoalescesAndFlushesLatestValue(t *testing.T) {
	es, err := newTestEmbeddedServer(testConfig{Port: -1, StoreDir: t.TempDir()})
	if err != nil {
		t.Fatalf("newTestEmbeddedServer: %v", err)
	}
	defer es.Shutdown()
	defer func() { persistStore = nil }()

	if err := PreparePersistStore(es.Conn()); err != nil {
		t.Fatalf("PreparePersistStore: %v", err)
	}
	store := GetPersistStore()
	if store == nil {
		t.Fatal("GetPersistStore returned nil")
	}

	if err := store.Put("default.device.metric", 1.0); err != nil {
		t.Fatalf("Put first: %v", err)
	}
	if err := store.Put("default.device.metric", 2.0); err != nil {
		t.Fatalf("Put second: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := StopPersistStore(ctx); err != nil {
		t.Fatalf("StopPersistStore: %v", err)
	}

	entry, err := store.Get("default.device.metric")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if entry == nil {
		t.Fatal("entry not found")
	}
	if got, ok := entry.Value.(float64); !ok || got != 2.0 {
		t.Fatalf("entry value = %#v, want 2.0", entry.Value)
	}
}

func TestPersistStoreStartupSnapshotIsScopedToRestore(t *testing.T) {
	es, err := newTestEmbeddedServer(testConfig{Port: -1, StoreDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer es.Shutdown()
	defer func() { persistStore = nil }()
	if err := PreparePersistStore(es.Conn()); err != nil {
		t.Fatal(err)
	}
	store := GetPersistStore()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer store.Stop(ctx)
	original := PersistEntry{Value: 49.28, Timestamp: 12345}
	if err := store.putNow("default.route.coordinates.0", original); err != nil {
		t.Fatal(err)
	}
	if err := store.BeginRestore(ctx); err != nil {
		t.Fatal(err)
	}
	// A cached snapshot must not do live reads, including for missing tags.
	if err := store.putNow("default.route.coordinates.0", PersistEntry{Value: 50.0}); err != nil {
		t.Fatal(err)
	}
	if err := store.putNow("default.new", PersistEntry{Value: 1.0}); err != nil {
		t.Fatal(err)
	}
	saved, err := store.Get("default.route.coordinates.0")
	if err != nil || saved == nil || saved.Value != original.Value || saved.Timestamp != original.Timestamp {
		t.Fatalf("snapshot = %#v, %v", saved, err)
	}
	missing, err := store.Get("default.new")
	if err != nil || missing != nil {
		t.Fatalf("snapshot missing key = %#v, %v", missing, err)
	}
	store.EndRestore()
	saved, err = store.Get("default.route.coordinates.0")
	if err != nil || saved == nil || saved.Value != 50.0 {
		t.Fatalf("live value = %#v, %v", saved, err)
	}
}
