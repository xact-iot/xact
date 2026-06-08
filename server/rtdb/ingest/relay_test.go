package ingest

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	natsgo "github.com/nats-io/nats.go"
)

type testNATSServer struct {
	server *natsserver.Server
	nc     *natsgo.Conn
}

func newTestNATSServer(t *testing.T) *testNATSServer {
	t.Helper()
	s, err := natsserver.NewServer(&natsserver.Options{
		Port:  -1,
		NoLog: true,
	})
	if err != nil {
		t.Fatalf("create NATS server: %v", err)
	}
	go s.Start()
	if !s.ReadyForConnections(10 * time.Second) {
		s.Shutdown()
		t.Fatal("NATS server failed to start")
	}
	nc, err := natsgo.Connect(s.ClientURL())
	if err != nil {
		s.Shutdown()
		t.Fatalf("connect NATS: %v", err)
	}
	return &testNATSServer{server: s, nc: nc}
}

func (s *testNATSServer) shutdown() {
	if s.nc != nil {
		s.nc.Close()
	}
	if s.server != nil {
		s.server.Shutdown()
	}
}

func TestPublishIngestStampsEventAndRecordsPublished(t *testing.T) {
	SharedIngestMetrics().resetForTest()
	server := newTestNATSServer(t)
	defer server.shutdown()

	received := make(chan IngestEvent, 1)
	sub, err := SubscribeIngestWithConfig(server.nc, func(evt IngestEvent) error {
		received <- evt
		return nil
	}, IngestRelayConfig{Workers: 1, QueueSize: 4, NATSPendingMsgLimit: 1024, NATSPendingByteLimit: 1024 * 1024})
	if err != nil {
		t.Fatalf("SubscribeIngestWithConfig: %v", err)
	}
	defer sub.Unsubscribe()
	if err := server.nc.Flush(); err != nil {
		t.Fatalf("flush subscribe: %v", err)
	}

	before := time.Now().UnixNano()
	err = PublishIngest(server.nc, "TestOrg", "", "VMS", "Dev1", TagData{
		DirectTags: map[string]any{"temperature": 12.3},
	})
	if err != nil {
		t.Fatalf("PublishIngest: %v", err)
	}

	select {
	case evt := <-received:
		if evt.PublishedUnixNano < before {
			t.Fatalf("PublishedUnixNano = %d, want >= %d", evt.PublishedUnixNano, before)
		}
		if evt.Tenant != "TestOrg" || evt.DeviceType != "VMS" || evt.DeviceName != "Dev1" {
			t.Fatalf("unexpected event routing: %+v", evt)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for ingest event")
	}

	snap := SharedIngestMetrics().Snapshot()
	if snap.TotalPublished != 1 {
		t.Fatalf("TotalPublished = %d, want 1", snap.TotalPublished)
	}
	if snap.TotalPublishErrors != 0 {
		t.Fatalf("TotalPublishErrors = %d, want 0", snap.TotalPublishErrors)
	}
}

func TestPublishIngestRecordsPublishErrors(t *testing.T) {
	SharedIngestMetrics().resetForTest()
	err := PublishIngest(nil, "TestOrg", "", "VMS", "Dev1", TagData{})
	if err == nil {
		t.Fatal("PublishIngest nil connection error = nil, want error")
	}
	snap := SharedIngestMetrics().Snapshot()
	if snap.TotalPublishErrors != 1 {
		t.Fatalf("TotalPublishErrors = %d, want 1", snap.TotalPublishErrors)
	}
}

func TestSubscribeIngestRecordsSuccessAndFailure(t *testing.T) {
	SharedIngestMetrics().resetForTest()
	server := newTestNATSServer(t)
	defer server.shutdown()

	handled := make(chan error, 2)
	sub, err := SubscribeIngest(server.nc, func(evt IngestEvent) error {
		if strings.Contains(evt.DeviceName, "bad") {
			err := errors.New("boom")
			handled <- err
			return err
		}
		handled <- nil
		return nil
	})
	if err != nil {
		t.Fatalf("SubscribeIngest: %v", err)
	}
	defer sub.Unsubscribe()
	if err := server.nc.Flush(); err != nil {
		t.Fatalf("flush subscribe: %v", err)
	}

	for _, deviceName := range []string{"good", "bad"} {
		data, err := json.Marshal(IngestEvent{
			Tenant:            "TestOrg",
			DeviceType:        "VMS",
			DeviceName:        deviceName,
			TagData:           TagData{DirectTags: map[string]any{"temperature": 12.3}},
			PublishedUnixNano: time.Now().Add(-2 * time.Millisecond).UnixNano(),
		})
		if err != nil {
			t.Fatalf("marshal event: %v", err)
		}
		if err := server.nc.Publish(IngestSubject+"TestOrg.VMS."+deviceName, data); err != nil {
			t.Fatalf("publish event: %v", err)
		}
	}

	for i := 0; i < 2; i++ {
		select {
		case <-handled:
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for handler")
		}
	}

	snap := SharedIngestMetrics().Snapshot()
	if snap.TotalStarted != 2 {
		t.Fatalf("TotalStarted = %d, want 2", snap.TotalStarted)
	}
	if snap.TotalPublished != 2 {
		t.Fatalf("TotalPublished = %d, want 2", snap.TotalPublished)
	}
	if snap.TotalCompleted != 1 {
		t.Fatalf("TotalCompleted = %d, want 1", snap.TotalCompleted)
	}
	if snap.TotalFailed != 1 {
		t.Fatalf("TotalFailed = %d, want 1", snap.TotalFailed)
	}
	if snap.InFlight != 0 {
		t.Fatalf("InFlight = %d, want 0", snap.InFlight)
	}
	if snap.PeakEndToEndLatencyNs <= 0 {
		t.Fatalf("PeakEndToEndLatencyNs = %d, want > 0", snap.PeakEndToEndLatencyNs)
	}
}

func TestSubscribeIngestWorkerPoolPreservesDeviceOrder(t *testing.T) {
	SharedIngestMetrics().resetForTest()
	server := newTestNATSServer(t)
	defer server.shutdown()

	const count = 8
	handled := make(chan int, count)
	sub, err := SubscribeIngestWithConfig(server.nc, func(evt IngestEvent) error {
		seq, _ := evt.TagData.DirectTags["seq"].(float64)
		handled <- int(seq)
		return nil
	}, IngestRelayConfig{Workers: 4, QueueSize: count, NATSPendingMsgLimit: 1024, NATSPendingByteLimit: 1024 * 1024})
	if err != nil {
		t.Fatalf("SubscribeIngestWithConfig: %v", err)
	}
	defer sub.Unsubscribe()
	if err := server.nc.Flush(); err != nil {
		t.Fatalf("flush subscribe: %v", err)
	}

	for i := 0; i < count; i++ {
		data, err := json.Marshal(IngestEvent{
			Tenant:            "TestOrg",
			DeviceType:        "VMS",
			DeviceName:        "same-device",
			TagData:           TagData{DirectTags: map[string]any{"seq": i}},
			PublishedUnixNano: time.Now().UnixNano(),
		})
		if err != nil {
			t.Fatalf("marshal event: %v", err)
		}
		if err := server.nc.Publish(IngestSubject+"TestOrg.VMS.same-device", data); err != nil {
			t.Fatalf("publish event: %v", err)
		}
	}

	for want := 0; want < count; want++ {
		select {
		case got := <-handled:
			if got != want {
				t.Fatalf("handled sequence %d = %d, want %d", want, got, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for handler")
		}
	}
}

func TestSubscribeIngestDrainOnlyDoesNotCallHandler(t *testing.T) {
	SharedIngestMetrics().resetForTest()
	server := newTestNATSServer(t)
	defer server.shutdown()

	handled := make(chan struct{}, 1)
	sub, err := SubscribeIngestWithConfig(server.nc, func(evt IngestEvent) error {
		handled <- struct{}{}
		return nil
	}, IngestRelayConfig{DrainOnly: true, Workers: 1, QueueSize: 1, NATSPendingMsgLimit: 1024, NATSPendingByteLimit: 1024 * 1024})
	if err != nil {
		t.Fatalf("SubscribeIngestWithConfig: %v", err)
	}
	defer sub.Unsubscribe()
	if err := server.nc.Flush(); err != nil {
		t.Fatalf("flush subscribe: %v", err)
	}

	data, err := json.Marshal(IngestEvent{
		Tenant:            "TestOrg",
		DeviceType:        "VMS",
		DeviceName:        "drain-only",
		TagData:           TagData{DirectTags: map[string]any{"temperature": 12.3}},
		PublishedUnixNano: time.Now().UnixNano(),
	})
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	if err := server.nc.Publish(IngestSubject+"TestOrg.VMS.drain-only", data); err != nil {
		t.Fatalf("publish event: %v", err)
	}
	if err := server.nc.Flush(); err != nil {
		t.Fatalf("flush publish: %v", err)
	}

	select {
	case <-handled:
		t.Fatal("handler was called in drain-only mode")
	case <-time.After(50 * time.Millisecond):
	}

	snap := SharedIngestMetrics().Snapshot()
	if snap.TotalStarted != 0 || snap.TotalCompleted != 0 {
		t.Fatalf("metrics = started %d completed %d, want zero", snap.TotalStarted, snap.TotalCompleted)
	}
}

func TestIngestRelayQueueSizeIsTotalCapacity(t *testing.T) {
	pool := newIngestRelayPool(IngestRelayConfig{Workers: 8, QueueSize: 8196}, func(evt IngestEvent) error {
		return nil
	})
	if got := pool.metrics.QueueCap; got != 8196 {
		t.Fatalf("QueueCap = %d, want 8196", got)
	}
	for i, q := range pool.queues {
		if cap(q) < 1 {
			t.Fatalf("queue %d cap = %d, want >= 1", i, cap(q))
		}
	}
}

func TestDefaultIngestRelayConfigFromEnv(t *testing.T) {
	t.Setenv("INGEST_RELAY_WORKERS", "0")
	t.Setenv("INGEST_RELAY_QUEUE_SIZE", "-1")
	t.Setenv("INGEST_NATS_PENDING_MSG_LIMIT", "123")
	t.Setenv("INGEST_NATS_PENDING_BYTE_LIMIT", "456")
	t.Setenv("INGEST_RELAY_ENQUEUE_TIMEOUT_MS", "250")
	t.Setenv("INGEST_RELAY_DRAIN_ONLY", "true")

	cfg := DefaultIngestRelayConfig()
	if cfg.Workers != 1 {
		t.Fatalf("Workers = %d, want fallback 1", cfg.Workers)
	}
	if cfg.QueueSize != defaultIngestRelayQueueSize {
		t.Fatalf("QueueSize = %d, want default %d", cfg.QueueSize, defaultIngestRelayQueueSize)
	}
	if cfg.NATSPendingMsgLimit != 123 || cfg.NATSPendingByteLimit != 456 {
		t.Fatalf("pending limits = %d/%d", cfg.NATSPendingMsgLimit, cfg.NATSPendingByteLimit)
	}
	if cfg.EnqueueTimeout != 250*time.Millisecond || !cfg.DrainOnly {
		t.Fatalf("timeout/drain = %s/%t", cfg.EnqueueTimeout, cfg.DrainOnly)
	}
}

func TestRelayEnvFallbackBranches(t *testing.T) {
	t.Setenv("INGEST_TEST_INT", "bad")
	if got := envInt("INGEST_TEST_INT", 9); got != 9 {
		t.Fatalf("envInt invalid = %d", got)
	}
	t.Setenv("INGEST_TEST_BOOL", "bad")
	if got := envBool("INGEST_TEST_BOOL", true); !got {
		t.Fatalf("envBool invalid = %t", got)
	}
	t.Setenv("INGEST_TEST_DURATION", "bad")
	if got := envDurationMs("INGEST_TEST_DURATION", 3*time.Second); got != 3*time.Second {
		t.Fatalf("envDurationMs invalid = %s", got)
	}
	if got := maxInt(5, 2); got != 5 {
		t.Fatalf("maxInt = %d", got)
	}
}

func TestIngestRelayMetricsTracksWindowPeaksAndMaxima(t *testing.T) {
	var metrics relayMetrics

	metrics.UpdateQueueLength(1)
	metrics.UpdateQueueLength(4)
	metrics.UpdateQueueLength(2)
	metrics.IncrementActiveWorkers()
	metrics.IncrementActiveWorkers()
	metrics.DecrementActiveWorkers()

	snap := metrics.Snapshot(2)
	if snap.PeakQueueLen != 4 {
		t.Fatalf("PeakQueueLen = %d, want 4", snap.PeakQueueLen)
	}
	if snap.PeakActiveWorkers != 2 {
		t.Fatalf("PeakActiveWorkers = %d, want 2", snap.PeakActiveWorkers)
	}
	if snap.MaxQueueLen != 4 {
		t.Fatalf("MaxQueueLen = %d, want 4", snap.MaxQueueLen)
	}
	if snap.MaxActiveWorkers != 2 {
		t.Fatalf("MaxActiveWorkers = %d, want 2", snap.MaxActiveWorkers)
	}

	snap = metrics.Snapshot(2)
	if snap.PeakQueueLen != 2 {
		t.Fatalf("reset PeakQueueLen = %d, want 2", snap.PeakQueueLen)
	}
	if snap.PeakActiveWorkers != 1 {
		t.Fatalf("reset PeakActiveWorkers = %d, want 1", snap.PeakActiveWorkers)
	}
	if snap.MaxQueueLen != 4 {
		t.Fatalf("reset MaxQueueLen = %d, want 4", snap.MaxQueueLen)
	}
	if snap.MaxActiveWorkers != 2 {
		t.Fatalf("reset MaxActiveWorkers = %d, want 2", snap.MaxActiveWorkers)
	}
}
