package metrics

import (
	"fmt"
	"testing"
	"time"

	"github.com/xact-iot/xact/rtdb/ingest"
)

func TestClassifySharedIngestHealthByLag(t *testing.T) {
	if got := classifySharedIngestHealth(sharedIngestReport{MaxLagMs: 999}, 0); got != ingest.HealthHealthy {
		t.Fatalf("MaxLagMs 999 health = %d, want healthy", got)
	}
	if got := classifySharedIngestHealth(sharedIngestReport{MaxLagMs: 1000}, 0); got != ingest.HealthWarning {
		t.Fatalf("MaxLagMs 1000 health = %d, want warning", got)
	}
	if got := classifySharedIngestHealth(sharedIngestReport{MaxLagMs: 5000}, 0); got != ingest.HealthCritical {
		t.Fatalf("MaxLagMs 5000 health = %d, want critical", got)
	}
}

func TestClassifySharedIngestHealthByPendingGrowth(t *testing.T) {
	if got := classifySharedIngestHealth(sharedIngestReport{}, 1); got != ingest.HealthWarning {
		t.Fatalf("pending growth streak 1 health = %d, want warning", got)
	}
	if got := classifySharedIngestHealth(sharedIngestReport{}, 2); got != ingest.HealthCritical {
		t.Fatalf("pending growth streak 2 health = %d, want critical", got)
	}
}

func TestBuildSharedIngestReportComputesRatesAndAverages(t *testing.T) {
	var shared ingest.SharedMetrics
	start := time.Now().Add(-20 * time.Millisecond).UnixNano()
	shared.RecordPublished()
	shared.RecordPublished()
	shared.RecordStart(start, start+int64(time.Millisecond))
	shared.RecordFinish(start, start+int64(time.Millisecond), start+int64(11*time.Millisecond), false)
	shared.RecordStart(start, start+int64(2*time.Millisecond))
	shared.RecordFinish(start, start+int64(2*time.Millisecond), start+int64(22*time.Millisecond), true)

	state := sharedIngestReportState{
		lastSnap: ingest.SharedMetricsSnapshot{},
		lastAt:   time.Now().Add(-2 * time.Second),
	}
	snap := shared.Snapshot()
	state.lastSnap = ingest.SharedMetricsSnapshot{}

	report := buildSharedIngestReportFromSnapshot(snap, &state, time.Now(), 0, 0)
	if report.PublishedRatePerSec <= 0 {
		t.Fatalf("PublishedRatePerSec = %f, want > 0", report.PublishedRatePerSec)
	}
	if report.CompletedRatePerSec <= 0 {
		t.Fatalf("CompletedRatePerSec = %f, want > 0", report.CompletedRatePerSec)
	}
	if report.FailedRatePerSec <= 0 {
		t.Fatalf("FailedRatePerSec = %f, want > 0", report.FailedRatePerSec)
	}
	if report.AvgLagMs <= 0 {
		t.Fatalf("AvgLagMs = %f, want > 0", report.AvgLagMs)
	}
	if report.AvgProcessMs <= 0 {
		t.Fatalf("AvgProcessMs = %f, want > 0", report.AvgProcessMs)
	}
}

type fakePendingSampler struct {
	snap ingest.IngestSnapshot
	err  error
}

func (f fakePendingSampler) Pending() (int, int, error) {
	if f.err != nil {
		return 0, 0, f.err
	}
	return 7, 1024, nil
}

func (f fakePendingSampler) SnapshotIngest() ingest.IngestSnapshot {
	return f.snap
}

func TestCollectorHelpersPendingAndPublish(t *testing.T) {
	t.Setenv("SERVER_NAME", "collector-test")
	c := New(nil, fakePendingSampler{snap: ingest.IngestSnapshot{
		QueueLen: 2, QueueCap: 10, WorkerCount: 3, ActiveWorkers: 1,
		PeakQueueLen: 4, PeakActiveWorkers: 2, MaxQueueLen: 5, MaxActiveWorkers: 3,
	}})
	if c.serverName == "" || c.metricQueueLen() != 0 || c.metricQueueCap() != 0 || c.metricDropped() != 0 {
		t.Fatalf("collector initial state: name=%q q=%d/%d dropped=%d", c.serverName, c.metricQueueLen(), c.metricQueueCap(), c.metricDropped())
	}

	sub := fakePendingSampler{snap: ingest.IngestSnapshot{QueueLen: 1, QueueCap: 2}}
	c.SetIngestSubscription(sub)
	msgs, bytes := c.ingestPending()
	if msgs != 7 || bytes != 1024 {
		t.Fatalf("pending = %d/%d", msgs, bytes)
	}
	c.ingestSub = fakePendingSampler{err: fmt.Errorf("boom")}
	if msgs, bytes := c.ingestPending(); msgs != 0 || bytes != 0 {
		t.Fatalf("pending error = %d/%d", msgs, bytes)
	}

	c.publish(1, int64(time.Second), int64(2*time.Second), 4, int64(time.Second), 2, 2, int64(6*time.Millisecond), 9e6, sharedIngestReport{})
}

func TestSmallMathHelpersAndServerNameFallback(t *testing.T) {
	if positiveDelta(1, 2) != 0 || positiveDelta(5, 3) != 2 {
		t.Fatal("positiveDelta mismatch")
	}
	if maxInt(1, 2) != 2 || maxInt64(5, 3) != 5 {
		t.Fatal("max helpers mismatch")
	}
	if resolveServerName() == "" {
		t.Fatal("resolveServerName returned empty string")
	}
}
