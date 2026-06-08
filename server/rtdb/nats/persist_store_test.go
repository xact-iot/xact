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
