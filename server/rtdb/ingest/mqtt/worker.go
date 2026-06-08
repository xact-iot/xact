package mqtt

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"

	natsgo "github.com/nats-io/nats.go"
	"github.com/xact-iot/xact/rtdb/ingest"
)

// Metrics tracks processing metrics.
type Metrics struct {
	TotalProcessed    int64
	TotalLatency      int64 // nanoseconds, enqueue to processing completion
	TotalWorkerBusyNs int64 // nanoseconds spent actively processing
	PeakLatency       int64 // nanoseconds, reset on each SnapshotIngest call

	ActiveWorkers     int64
	PeakActiveWorkers int64
	MaxActiveWorkers  int64
	QueueLength       int
	QueueCap          int
	WorkerCount       int
	PeakQueueLength   int
	MaxQueueLength    int
	TotalQueueLenNs   int64
	lastQueueUpdate   time.Time
	mu                sync.Mutex
}

func (m *Metrics) IncrementActiveWorkers() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.ActiveWorkers++
	if m.ActiveWorkers > m.PeakActiveWorkers {
		m.PeakActiveWorkers = m.ActiveWorkers
	}
	if m.ActiveWorkers > m.MaxActiveWorkers {
		m.MaxActiveWorkers = m.ActiveWorkers
	}
}

func (m *Metrics) DecrementActiveWorkers() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.ActiveWorkers > 0 {
		m.ActiveWorkers--
	}
}

func (m *Metrics) RecordProcessing(latency, busy time.Duration) {
	ns := latency.Nanoseconds()
	atomic.AddInt64(&m.TotalProcessed, 1)
	atomic.AddInt64(&m.TotalLatency, ns)
	atomic.AddInt64(&m.TotalWorkerBusyNs, busy.Nanoseconds())
	for {
		old := atomic.LoadInt64(&m.PeakLatency)
		if ns <= old {
			break
		}
		if atomic.CompareAndSwapInt64(&m.PeakLatency, old, ns) {
			break
		}
	}
}

func (m *Metrics) UpdateQueueLength(length int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.updateQueueLenDurationLocked(time.Now())
	m.QueueLength = length
	if length > m.PeakQueueLength {
		m.PeakQueueLength = length
	}
	if length > m.MaxQueueLength {
		m.MaxQueueLength = length
	}
}

func (m *Metrics) updateQueueLenDurationLocked(now time.Time) {
	if !m.lastQueueUpdate.IsZero() {
		m.TotalQueueLenNs += int64(now.Sub(m.lastQueueUpdate)) * int64(m.QueueLength)
	}
	m.lastQueueUpdate = now
}

// SnapshotIngest returns a point-in-time sample and resets peak metrics.
func (m *Metrics) SnapshotIngest() ingest.IngestSnapshot {
	now := time.Now()
	m.mu.Lock()
	m.updateQueueLenDurationLocked(now)
	queueLen := m.QueueLength
	peakQueueLen := m.PeakQueueLength
	if queueLen > peakQueueLen {
		peakQueueLen = queueLen
	}
	activeWorkers := m.ActiveWorkers
	peakActiveWorkers := m.PeakActiveWorkers
	if activeWorkers > peakActiveWorkers {
		peakActiveWorkers = activeWorkers
	}
	maxQueueLen := m.MaxQueueLength
	if peakQueueLen > maxQueueLen {
		maxQueueLen = peakQueueLen
	}
	maxActiveWorkers := m.MaxActiveWorkers
	if peakActiveWorkers > maxActiveWorkers {
		maxActiveWorkers = peakActiveWorkers
	}
	totalQueueLenNs := m.TotalQueueLenNs
	m.PeakQueueLength = queueLen
	m.PeakActiveWorkers = activeWorkers
	m.mu.Unlock()

	return ingest.IngestSnapshot{
		QueueLen:          queueLen,
		QueueCap:          m.QueueCap,
		PeakQueueLen:      peakQueueLen,
		MaxQueueLen:       maxQueueLen,
		WorkerCount:       m.WorkerCount,
		ActiveWorkers:     activeWorkers,
		PeakActiveWorkers: peakActiveWorkers,
		MaxActiveWorkers:  maxActiveWorkers,
		TotalProcessed:    atomic.LoadInt64(&m.TotalProcessed),
		TotalLatencyNs:    atomic.LoadInt64(&m.TotalLatency),
		TotalWorkerBusyNs: atomic.LoadInt64(&m.TotalWorkerBusyNs),
		TotalQueueLenNs:   totalQueueLenNs,
		PeakLatencyNs:     atomic.SwapInt64(&m.PeakLatency, 0),
	}
}

// WorkerPool manages a pool of goroutines for processing MQTT messages.
type WorkerPool struct {
	workers     int
	queue       chan Message
	enqueueWait time.Duration
	nc          *natsgo.Conn
	metrics     *Metrics
	wg          sync.WaitGroup
	stopCh      chan struct{}
}

// NewWorkerPool creates a new worker pool.
func NewWorkerPool(workers, queueSize int, enqueueWait time.Duration, nc *natsgo.Conn, metrics *Metrics) *WorkerPool {
	metrics.QueueCap = queueSize
	metrics.WorkerCount = workers
	return &WorkerPool{
		workers:     workers,
		queue:       make(chan Message, queueSize),
		enqueueWait: enqueueWait,
		nc:          nc,
		metrics:     metrics,
		stopCh:      make(chan struct{}),
	}
}

// Start launches all worker goroutines.
func (wp *WorkerPool) Start() {
	for i := 0; i < wp.workers; i++ {
		wp.wg.Add(1)
		go wp.worker()
	}
}

// Stop signals workers to stop and waits for them to finish.
func (wp *WorkerPool) Stop() {
	close(wp.stopCh)
	wp.wg.Wait()
}

// Submit enqueues a message, blocking until space is available or the pool stops.
func (wp *WorkerPool) Submit(msg Message) bool {
	return wp.SubmitContext(context.Background(), msg)
}

func (wp *WorkerPool) SubmitContext(ctx context.Context, msg Message) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	if msg.EnqueuedAt.IsZero() {
		msg.EnqueuedAt = time.Now()
	}
	select {
	case wp.queue <- msg:
		wp.metrics.UpdateQueueLength(len(wp.queue))
		return true
	case <-wp.stopCh:
		log.Printf("MQTT worker: stopped, cannot enqueue message from topic %s", msg.Topic)
		return false
	case <-ctx.Done():
		log.Printf("MQTT worker: queue busy, cannot enqueue message from topic %s: %v", msg.Topic, ctx.Err())
		return false
	}
}

func (wp *WorkerPool) worker() {
	defer wp.wg.Done()
	for {
		select {
		case <-wp.stopCh:
			return
		case msg := <-wp.queue:
			start := time.Now()
			if msg.EnqueuedAt.IsZero() {
				msg.EnqueuedAt = start
			}
			wp.metrics.UpdateQueueLength(len(wp.queue))
			wp.metrics.IncrementActiveWorkers()
			if wp.processMessage(msg) {
				if msg.Ack != nil {
					msg.Ack()
				}
				wp.metrics.RecordProcessing(time.Since(msg.EnqueuedAt), time.Since(start))
			}
			wp.metrics.DecrementActiveWorkers()
		}
	}
}

func (wp *WorkerPool) processMessage(msg Message) bool {
	if err := ingest.PublishIngest(wp.nc, msg.Tenant, msg.Zone, msg.DeviceType, msg.DeviceName, msg.TagData); err != nil {
		log.Printf("MQTT ingest: publish %s/%s/%s/%s: %v", msg.Tenant, msg.Zone, msg.DeviceType, msg.DeviceName, err)
		return false
	}
	return true
}
