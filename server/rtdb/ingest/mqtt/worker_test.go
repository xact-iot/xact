package mqtt

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestMetricsSnapshotTracksPeaksAndResets(t *testing.T) {
	var metrics Metrics

	metrics.UpdateQueueLength(1)
	metrics.UpdateQueueLength(4)
	metrics.UpdateQueueLength(2)
	metrics.IncrementActiveWorkers()
	metrics.IncrementActiveWorkers()
	metrics.DecrementActiveWorkers()

	snap := metrics.SnapshotIngest()
	if snap.QueueLen != 2 {
		t.Fatalf("QueueLen = %d, want 2", snap.QueueLen)
	}
	if snap.PeakQueueLen != 4 {
		t.Fatalf("PeakQueueLen = %d, want 4", snap.PeakQueueLen)
	}
	if snap.ActiveWorkers != 1 {
		t.Fatalf("ActiveWorkers = %d, want 1", snap.ActiveWorkers)
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

	snap = metrics.SnapshotIngest()
	if snap.PeakQueueLen != 2 {
		t.Fatalf("reset PeakQueueLen = %d, want current queue length 2", snap.PeakQueueLen)
	}
	if snap.PeakActiveWorkers != 1 {
		t.Fatalf("reset PeakActiveWorkers = %d, want current active workers 1", snap.PeakActiveWorkers)
	}
	if snap.MaxQueueLen != 4 {
		t.Fatalf("reset MaxQueueLen = %d, want 4", snap.MaxQueueLen)
	}
	if snap.MaxActiveWorkers != 2 {
		t.Fatalf("reset MaxActiveWorkers = %d, want 2", snap.MaxActiveWorkers)
	}
}

func TestTopicPatternsMatchParserShapes(t *testing.T) {
	if got := len(strings.Split(TopicPattern, "/")); got != 5 {
		t.Fatalf("TopicPattern has %d levels, want 5 for xact/data/{tenant}/{devicetype}/{devicename}", got)
	}
	if got := len(strings.Split(TopicPatternZoned, "/")); got != 7 {
		t.Fatalf("TopicPatternZoned has %d levels, want 7 for xact/data/{tenant}/zone/{zone}/{devicetype}/{devicename}", got)
	}

	tenant, zone, msgType, deviceType, deviceName, err := ParseTopic("xact/data/default/Templates/AirQualityStandard")
	if err != nil {
		t.Fatalf("ParseTopic zoneless template: %v", err)
	}
	if tenant != "default" || zone != "" || msgType != "data" || deviceType != "Templates" || deviceName != "AirQualityStandard" {
		t.Fatalf("ParseTopic zoneless template = %q %q %q %q %q", tenant, zone, msgType, deviceType, deviceName)
	}
}

func TestParseTopicVariantsAndErrors(t *testing.T) {
	tenant, zone, msgType, deviceType, deviceName, err := ParseTopic("xact/data/default/zone/north/Pump/P1")
	if err != nil {
		t.Fatalf("ParseTopic zoned: %v", err)
	}
	if tenant != "default" || zone != "north" || msgType != "data" || deviceType != "Pump" || deviceName != "P1" {
		t.Fatalf("zoned parse = %q %q %q %q %q", tenant, zone, msgType, deviceType, deviceName)
	}

	if _, _, _, _, _, err := ParseTopic("wrong/data/default/Pump/P1"); err == nil || !strings.Contains(err.Error(), "prefix") {
		t.Fatalf("expected prefix error, got %v", err)
	}
	if _, _, _, _, _, err := ParseTopic("xact/data/default/notzone/north/Pump/P1"); err == nil || !strings.Contains(err.Error(), "zoned") {
		t.Fatalf("expected zoned format error, got %v", err)
	}
	if _, _, _, _, _, err := ParseTopic("xact/data/default"); err == nil || !strings.Contains(err.Error(), "5 or 7") {
		t.Fatalf("expected part count error, got %v", err)
	}
}

func TestMQTTPayloadPathAndTimestampHelpers(t *testing.T) {
	data, err := ParsePayload([]byte(`{"templateName":"Template.One","ts":"1700000000000","temp":12.5}`))
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	if data.TemplateName != "Template.One" || data.TSUnixMilli != 1700000000000 || data.DirectTags["temp"] != 12.5 {
		t.Fatalf("unexpected payload: %+v", data)
	}

	if got := BuildRtdbPath("", "", "Pump", "P1", "Status", "Run"); got != "/default/Pump/P1/Status/Run" {
		t.Fatalf("zoneless path = %q", got)
	}
	if got := BuildRtdbPath("tenant", "north", "Pump", "P1", "Status", "Run"); got != "/tenant/zone/north/Pump/P1/Status/Run" {
		t.Fatalf("zoned path = %q", got)
	}

	msg := &Message{TagData: data}
	if got := msg.Timestamp(); got != 1700000000000 {
		t.Fatalf("timestamp = %d", got)
	}
	msg.TagData.TSUnixMilli = 0
	if got := msg.Timestamp(); got == 0 {
		t.Fatal("fallback timestamp should be non-zero")
	}
}

func TestMetricsRecordProcessingTracksLatencyAndBusyTime(t *testing.T) {
	var metrics Metrics

	metrics.RecordProcessing(10*time.Millisecond, 3*time.Millisecond)
	metrics.RecordProcessing(5*time.Millisecond, 2*time.Millisecond)

	snap := metrics.SnapshotIngest()
	if snap.TotalProcessed != 2 {
		t.Fatalf("TotalProcessed = %d, want 2", snap.TotalProcessed)
	}
	if snap.TotalLatencyNs != int64(15*time.Millisecond) {
		t.Fatalf("TotalLatencyNs = %d, want %d", snap.TotalLatencyNs, int64(15*time.Millisecond))
	}
	if snap.TotalWorkerBusyNs != int64(5*time.Millisecond) {
		t.Fatalf("TotalWorkerBusyNs = %d, want %d", snap.TotalWorkerBusyNs, int64(5*time.Millisecond))
	}
	if snap.PeakLatencyNs != int64(10*time.Millisecond) {
		t.Fatalf("PeakLatencyNs = %d, want %d", snap.PeakLatencyNs, int64(10*time.Millisecond))
	}

	snap = metrics.SnapshotIngest()
	if snap.PeakLatencyNs != 0 {
		t.Fatalf("reset PeakLatencyNs = %d, want 0", snap.PeakLatencyNs)
	}
}

func TestWorkerPoolSubmitContextWaitsInsteadOfDropping(t *testing.T) {
	var metrics Metrics
	pool := NewWorkerPool(1, 1, time.Second, nil, &metrics)

	if !pool.Submit(Message{Topic: "xact/data/default/VMS/Dev1"}) {
		t.Fatal("first submit failed")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	if pool.SubmitContext(ctx, Message{Topic: "xact/data/default/VMS/Dev2"}) {
		t.Fatal("second submit succeeded, want timeout while queue is full")
	}
}
