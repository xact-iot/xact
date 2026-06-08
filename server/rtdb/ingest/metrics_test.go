package ingest

import (
	"testing"
	"time"
)

func TestSharedMetricsSnapshotTracksPeaksAndResets(t *testing.T) {
	var metrics SharedMetrics
	published := time.Now().Add(-20 * time.Millisecond).UnixNano()
	started := published + int64(5*time.Millisecond)
	finished := published + int64(20*time.Millisecond)

	metrics.RecordPublished()
	metrics.RecordStart(published, started)
	metrics.RecordFinish(published, started, finished, false)

	snap := metrics.Snapshot()
	if snap.TotalPublished != 1 {
		t.Fatalf("TotalPublished = %d, want 1", snap.TotalPublished)
	}
	if snap.TotalStarted != 1 {
		t.Fatalf("TotalStarted = %d, want 1", snap.TotalStarted)
	}
	if snap.TotalCompleted != 1 {
		t.Fatalf("TotalCompleted = %d, want 1", snap.TotalCompleted)
	}
	if snap.InFlight != 0 {
		t.Fatalf("InFlight = %d, want 0", snap.InFlight)
	}
	if snap.TotalPublishToStartNs != int64(5*time.Millisecond) {
		t.Fatalf("TotalPublishToStartNs = %d, want %d", snap.TotalPublishToStartNs, int64(5*time.Millisecond))
	}
	if snap.TotalEndToEndLatencyNs != int64(20*time.Millisecond) {
		t.Fatalf("TotalEndToEndLatencyNs = %d, want %d", snap.TotalEndToEndLatencyNs, int64(20*time.Millisecond))
	}
	if snap.TotalProcessLatencyNs != int64(15*time.Millisecond) {
		t.Fatalf("TotalProcessLatencyNs = %d, want %d", snap.TotalProcessLatencyNs, int64(15*time.Millisecond))
	}
	if snap.PeakEndToEndLatencyNs != int64(20*time.Millisecond) {
		t.Fatalf("PeakEndToEndLatencyNs = %d, want %d", snap.PeakEndToEndLatencyNs, int64(20*time.Millisecond))
	}

	snap = metrics.Snapshot()
	if snap.PeakEndToEndLatencyNs != 0 {
		t.Fatalf("reset PeakEndToEndLatencyNs = %d, want 0", snap.PeakEndToEndLatencyNs)
	}
}

func TestSharedMetricsRecordsFailures(t *testing.T) {
	var metrics SharedMetrics
	published := time.Now().Add(-10 * time.Millisecond).UnixNano()
	started := published + int64(time.Millisecond)
	finished := published + int64(10*time.Millisecond)

	metrics.RecordStart(published, started)
	metrics.RecordFinish(published, started, finished, true)

	snap := metrics.Snapshot()
	if snap.TotalFailed != 1 {
		t.Fatalf("TotalFailed = %d, want 1", snap.TotalFailed)
	}
	if snap.TotalCompleted != 0 {
		t.Fatalf("TotalCompleted = %d, want 0", snap.TotalCompleted)
	}
	if snap.InFlight != 0 {
		t.Fatalf("InFlight = %d, want 0", snap.InFlight)
	}
}
