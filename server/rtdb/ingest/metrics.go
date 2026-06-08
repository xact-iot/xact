package ingest

import (
	"sync/atomic"
	"time"
)

const (
	HealthHealthy  = 0
	HealthWarning  = 1
	HealthCritical = 2

	DefaultLagWarningNs  = int64(time.Second)
	DefaultLagCriticalNs = int64(5 * time.Second)
)

// SharedMetricsSnapshot is a point-in-time sample of the shared ingest relay.
type SharedMetricsSnapshot struct {
	TotalPublished             int64
	TotalPublishErrors         int64
	TotalStarted               int64
	TotalCompleted             int64
	TotalFailed                int64
	InFlight                   int64
	TotalPublishToStartNs      int64
	PeakPublishToStartNs       int64
	TotalEndToEndLatencyNs     int64
	PeakEndToEndLatencyNs      int64
	TotalProcessLatencyNs      int64
	PeakProcessLatencyNs       int64
	LastCompletedUnixNano      int64
	LastCompletedEventUnixNano int64
}

// SharedMetrics tracks ingest events after they enter the shared relay used by
// MQTT, REST, and any future ingress method that calls PublishIngest.
type SharedMetrics struct {
	totalPublished             int64
	totalPublishErrors         int64
	totalStarted               int64
	totalCompleted             int64
	totalFailed                int64
	inFlight                   int64
	totalPublishToStartNs      int64
	peakPublishToStartNs       int64
	totalEndToEndLatencyNs     int64
	peakEndToEndLatencyNs      int64
	totalProcessLatencyNs      int64
	peakProcessLatencyNs       int64
	lastCompletedUnixNano      int64
	lastCompletedEventUnixNano int64
}

var sharedMetrics SharedMetrics

// SharedIngestMetrics returns the process-wide shared ingest metrics.
func SharedIngestMetrics() *SharedMetrics {
	return &sharedMetrics
}

func (m *SharedMetrics) RecordPublished() {
	atomic.AddInt64(&m.totalPublished, 1)
}

func (m *SharedMetrics) RecordExternalPublished() {
	atomic.AddInt64(&m.totalPublished, 1)
}

func (m *SharedMetrics) RecordPublishError() {
	atomic.AddInt64(&m.totalPublishErrors, 1)
}

func (m *SharedMetrics) RecordStart(publishedUnixNano, startedUnixNano int64) {
	atomic.AddInt64(&m.totalStarted, 1)
	atomic.AddInt64(&m.inFlight, 1)
	if publishedUnixNano > 0 && startedUnixNano >= publishedUnixNano {
		latencyNs := startedUnixNano - publishedUnixNano
		atomic.AddInt64(&m.totalPublishToStartNs, latencyNs)
		updatePeakInt64(&m.peakPublishToStartNs, latencyNs)
	}
}

func (m *SharedMetrics) RecordFinish(publishedUnixNano, startedUnixNano, finishedUnixNano int64, failed bool) {
	if failed {
		atomic.AddInt64(&m.totalFailed, 1)
	} else {
		atomic.AddInt64(&m.totalCompleted, 1)
	}
	atomic.AddInt64(&m.inFlight, -1)

	if finishedUnixNano >= startedUnixNano {
		processNs := finishedUnixNano - startedUnixNano
		atomic.AddInt64(&m.totalProcessLatencyNs, processNs)
		updatePeakInt64(&m.peakProcessLatencyNs, processNs)
	}
	if publishedUnixNano > 0 && finishedUnixNano >= publishedUnixNano {
		endToEndNs := finishedUnixNano - publishedUnixNano
		atomic.AddInt64(&m.totalEndToEndLatencyNs, endToEndNs)
		updatePeakInt64(&m.peakEndToEndLatencyNs, endToEndNs)
		atomic.StoreInt64(&m.lastCompletedEventUnixNano, publishedUnixNano)
	}
	atomic.StoreInt64(&m.lastCompletedUnixNano, finishedUnixNano)
}

func (m *SharedMetrics) Snapshot() SharedMetricsSnapshot {
	return SharedMetricsSnapshot{
		TotalPublished:             atomic.LoadInt64(&m.totalPublished),
		TotalPublishErrors:         atomic.LoadInt64(&m.totalPublishErrors),
		TotalStarted:               atomic.LoadInt64(&m.totalStarted),
		TotalCompleted:             atomic.LoadInt64(&m.totalCompleted),
		TotalFailed:                atomic.LoadInt64(&m.totalFailed),
		InFlight:                   atomic.LoadInt64(&m.inFlight),
		TotalPublishToStartNs:      atomic.LoadInt64(&m.totalPublishToStartNs),
		PeakPublishToStartNs:       atomic.SwapInt64(&m.peakPublishToStartNs, 0),
		TotalEndToEndLatencyNs:     atomic.LoadInt64(&m.totalEndToEndLatencyNs),
		PeakEndToEndLatencyNs:      atomic.SwapInt64(&m.peakEndToEndLatencyNs, 0),
		TotalProcessLatencyNs:      atomic.LoadInt64(&m.totalProcessLatencyNs),
		PeakProcessLatencyNs:       atomic.SwapInt64(&m.peakProcessLatencyNs, 0),
		LastCompletedUnixNano:      atomic.LoadInt64(&m.lastCompletedUnixNano),
		LastCompletedEventUnixNano: atomic.LoadInt64(&m.lastCompletedEventUnixNano),
	}
}

func (m *SharedMetrics) resetForTest() {
	atomic.StoreInt64(&m.totalPublished, 0)
	atomic.StoreInt64(&m.totalPublishErrors, 0)
	atomic.StoreInt64(&m.totalStarted, 0)
	atomic.StoreInt64(&m.totalCompleted, 0)
	atomic.StoreInt64(&m.totalFailed, 0)
	atomic.StoreInt64(&m.inFlight, 0)
	atomic.StoreInt64(&m.totalPublishToStartNs, 0)
	atomic.StoreInt64(&m.peakPublishToStartNs, 0)
	atomic.StoreInt64(&m.totalEndToEndLatencyNs, 0)
	atomic.StoreInt64(&m.peakEndToEndLatencyNs, 0)
	atomic.StoreInt64(&m.totalProcessLatencyNs, 0)
	atomic.StoreInt64(&m.peakProcessLatencyNs, 0)
	atomic.StoreInt64(&m.lastCompletedUnixNano, 0)
	atomic.StoreInt64(&m.lastCompletedEventUnixNano, 0)
}

func updatePeakInt64(addr *int64, value int64) {
	for {
		old := atomic.LoadInt64(addr)
		if value <= old {
			return
		}
		if atomic.CompareAndSwapInt64(addr, old, value) {
			return
		}
	}
}
