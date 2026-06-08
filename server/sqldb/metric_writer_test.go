package sqldb

import (
	"context"
	"sync"
	"testing"
	"time"
)

type captureMetricInserter struct {
	mu    sync.Mutex
	calls map[string][][]MetricEntry
}

func (m *captureMetricInserter) InsertMetrics(_ context.Context, orgName string, entries []MetricEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.calls == nil {
		m.calls = make(map[string][][]MetricEntry)
	}
	copied := append([]MetricEntry(nil), entries...)
	m.calls[orgName] = append(m.calls[orgName], copied)
	return nil
}

func TestMetricWriterBatchesAndGroupsByOrg(t *testing.T) {
	db := &captureMetricInserter{}
	writer := NewMetricWriter(db, MetricWriterConfig{
		QueueSize:      10,
		BatchSize:      3,
		FlushInterval:  time.Hour,
		InsertTimeout:  time.Second,
		EnqueueTimeout: time.Second,
	})

	now := time.Now()
	for i := range 5 {
		org := "alpha"
		if i == 4 {
			org = "beta"
		}
		if err := writer.Write(org, MetricEntry{
			DeviceName: "device",
			MetricName: "metric",
			Timestamp:  now.Add(time.Duration(i) * time.Second),
			Value:      float32(i),
		}); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := writer.Stop(ctx); err != nil {
		t.Fatalf("stop: %v", err)
	}

	db.mu.Lock()
	defer db.mu.Unlock()

	alphaCount := 0
	for _, batch := range db.calls["alpha"] {
		alphaCount += len(batch)
		if len(batch) > 3 {
			t.Fatalf("alpha batch length = %d, want <= 3", len(batch))
		}
	}
	if alphaCount != 4 {
		t.Fatalf("alpha entries = %d, want 4", alphaCount)
	}

	betaCount := 0
	for _, batch := range db.calls["beta"] {
		betaCount += len(batch)
	}
	if betaCount != 1 {
		t.Fatalf("beta entries = %d, want 1", betaCount)
	}
}

func TestMetricWriterTryWriteReportsFullQueue(t *testing.T) {
	db := &captureMetricInserter{}
	writer := NewMetricWriter(db, MetricWriterConfig{
		QueueSize:      1,
		BatchSize:      100,
		FlushInterval:  time.Hour,
		InsertTimeout:  time.Second,
		EnqueueTimeout: time.Second,
	})

	entry := MetricEntry{DeviceName: "device", MetricName: "metric", Timestamp: time.Now(), Value: 1}
	if err := writer.TryWrite("alpha", entry); err != nil {
		t.Fatalf("first TryWrite: %v", err)
	}
	if err := writer.TryWrite("alpha", entry); err != ErrMetricWriterQueueFull {
		t.Fatalf("second TryWrite = %v, want ErrMetricWriterQueueFull", err)
	}
	if got := writer.Dropped(); got != 1 {
		t.Fatalf("Dropped = %d, want 1", got)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := writer.Stop(ctx); err != nil {
		t.Fatalf("stop: %v", err)
	}
}
