package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"hash/fnv"
	"log"
	"os"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	natsgo "github.com/nats-io/nats.go"
)

// IngestSubject is the NATS subject prefix for raw ingest events.
// All cluster servers subscribe to this; whichever server receives
// external data (MQTT, REST) publishes here and then every server
// processes the event through its own local pipeline.
const IngestSubject = "xact.internal.ingest."
const IngestRequestSubject = "xact.internal.ingest_request."

const defaultIngestRelayQueueSize = 8196

// IngestEvent carries the routing metadata and parsed tag data for one
// device payload received by an ingress module.
type IngestEvent struct {
	Tenant            string  `json:"tenant"`
	Zone              string  `json:"zone"`
	DeviceType        string  `json:"device_type"`
	DeviceName        string  `json:"device_name"`
	TagData           TagData `json:"tag_data"`
	PublishedUnixNano int64   `json:"published_unix_nano,omitempty"`
	MetricsRecorded   bool    `json:"metrics_recorded,omitempty"`
}

type IngestRelayConfig struct {
	Workers              int
	QueueSize            int
	NATSPendingMsgLimit  int
	NATSPendingByteLimit int
	EnqueueTimeout       time.Duration
	DrainOnly            bool
}

func DefaultIngestRelayConfig() IngestRelayConfig {
	workers := envInt("INGEST_RELAY_WORKERS", envInt("INGEST_WORKERS", runtime.NumCPU()))
	if workers <= 0 {
		workers = 1
	}
	queueSize := envInt("INGEST_RELAY_QUEUE_SIZE", envInt("INGEST_QUEUE_SIZE", defaultIngestRelayQueueSize))
	if queueSize <= 0 {
		queueSize = defaultIngestRelayQueueSize
	}
	pendingMsgs := envInt("INGEST_NATS_PENDING_MSG_LIMIT", queueSize)
	pendingBytes := envInt("INGEST_NATS_PENDING_BYTE_LIMIT", 1024*1024*1024)
	enqueueTimeout := envDurationMs("INGEST_RELAY_ENQUEUE_TIMEOUT_MS", 30*time.Second)
	return IngestRelayConfig{
		Workers:              workers,
		QueueSize:            queueSize,
		NATSPendingMsgLimit:  pendingMsgs,
		NATSPendingByteLimit: pendingBytes,
		EnqueueTimeout:       enqueueTimeout,
		DrainOnly:            envBool("INGEST_RELAY_DRAIN_ONLY", false),
	}
}

func envInt(name string, fallback int) int {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		log.Printf("ingest relay: invalid %s=%q, using %d", name, raw, fallback)
		return fallback
	}
	return value
}

func envBool(name string, fallback bool) bool {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		log.Printf("ingest relay: invalid %s=%q, using %t", name, raw, fallback)
		return fallback
	}
	return value
}

func envDurationMs(name string, fallback time.Duration) time.Duration {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		log.Printf("ingest relay: invalid %s=%q, using %s", name, raw, fallback)
		return fallback
	}
	return time.Duration(value) * time.Millisecond
}

var (
	ErrIngestRejected = errors.New("ingest rejected")
	ErrIngestBusy     = errors.New("ingest busy")
)

type IngestResponse struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// PublishIngest requests admission into the shared ingest relay. It returns only
// after the server has accepted the event into its bounded ingest queue.
func PublishIngest(nc *natsgo.Conn, tenant, zone, deviceType, deviceName string, tagData TagData) error {
	if nc == nil {
		SharedIngestMetrics().RecordPublishError()
		return natsgo.ErrInvalidConnection
	}
	evt := IngestEvent{
		Tenant:            tenant,
		Zone:              zone,
		DeviceType:        deviceType,
		DeviceName:        deviceName,
		TagData:           tagData,
		PublishedUnixNano: time.Now().UnixNano(),
		MetricsRecorded:   true,
	}
	data, err := json.Marshal(evt)
	if err != nil {
		SharedIngestMetrics().RecordPublishError()
		return err
	}
	subject := IngestSubjectFor(IngestRequestSubject, tenant, zone, deviceType, deviceName)
	msg, err := nc.Request(subject, data, ingestRequestTimeout())
	if err != nil {
		SharedIngestMetrics().RecordPublishError()
		if errors.Is(err, natsgo.ErrTimeout) {
			return ErrIngestBusy
		}
		return err
	}
	var resp IngestResponse
	if err := json.Unmarshal(msg.Data, &resp); err != nil {
		SharedIngestMetrics().RecordPublishError()
		return err
	}
	if resp.Status != "accepted" {
		SharedIngestMetrics().RecordPublishError()
		if resp.Status == "busy" {
			return ErrIngestBusy
		}
		if resp.Error != "" {
			return errors.Join(ErrIngestRejected, errors.New(resp.Error))
		}
		return ErrIngestRejected
	}
	SharedIngestMetrics().RecordPublished()
	return nil
}

func PublishIngestCore(nc *natsgo.Conn, tenant, zone, deviceType, deviceName string, tagData TagData) error {
	if nc == nil {
		SharedIngestMetrics().RecordPublishError()
		return natsgo.ErrInvalidConnection
	}
	evt := IngestEvent{
		Tenant:            tenant,
		Zone:              zone,
		DeviceType:        deviceType,
		DeviceName:        deviceName,
		TagData:           tagData,
		PublishedUnixNano: time.Now().UnixNano(),
		MetricsRecorded:   true,
	}
	data, err := json.Marshal(evt)
	if err != nil {
		SharedIngestMetrics().RecordPublishError()
		return err
	}
	if err := nc.Publish(IngestSubjectFor(IngestSubject, tenant, zone, deviceType, deviceName), data); err != nil {
		SharedIngestMetrics().RecordPublishError()
		return err
	}
	SharedIngestMetrics().RecordPublished()
	return nil
}

func ingestRequestTimeout() time.Duration {
	return envDurationMs("INGEST_REQUEST_TIMEOUT_MS", 35*time.Second)
}

func IngestSubjectFor(prefix, tenant, zone, deviceType, deviceName string) string {
	subject := prefix + tenant
	if zone != "" {
		subject += ".zone." + zone
	}
	subject += "." + deviceType + "." + deviceName
	return subject
}

// SubscribeIngest subscribes to all ingest events on nc and calls handler
// for each one.  Returns the subscription so the caller can unsubscribe on
// shutdown.
func SubscribeIngest(nc *natsgo.Conn, handler func(IngestEvent) error) (*IngestSubscription, error) {
	return SubscribeIngestWithConfig(nc, handler, DefaultIngestRelayConfig())
}

func SubscribeIngestWithConfig(nc *natsgo.Conn, handler func(IngestEvent) error, cfg IngestRelayConfig) (*IngestSubscription, error) {
	var pool *IngestRelayPool
	if !cfg.DrainOnly {
		pool = newIngestRelayPool(cfg, handler)
		pool.Start()
	} else {
		log.Printf("ingest relay: drain-only mode enabled; ingest messages will be consumed without decode or processing")
	}
	coreSub, err := nc.Subscribe(IngestSubject+">", func(msg *natsgo.Msg) {
		if cfg.DrainOnly {
			return
		}
		var evt IngestEvent
		if err := json.Unmarshal(msg.Data, &evt); err != nil {
			log.Printf("ingest relay: malformed event: %v", err)
			return
		}
		if !evt.MetricsRecorded {
			SharedIngestMetrics().RecordExternalPublished()
			if evt.PublishedUnixNano == 0 {
				evt.PublishedUnixNano = time.Now().UnixNano()
			}
		}
		pool.Submit(IngestWork{Event: evt, EnqueuedAt: time.Now()})
	})
	if err != nil {
		if pool != nil {
			pool.Stop(context.Background())
		}
		return nil, err
	}
	if err := coreSub.SetPendingLimits(cfg.NATSPendingMsgLimit, cfg.NATSPendingByteLimit); err != nil {
		log.Printf("ingest relay: set pending limits: %v", err)
	}
	requestSub, err := nc.Subscribe(IngestRequestSubject+">", func(msg *natsgo.Msg) {
		if cfg.DrainOnly {
			replyIngest(msg, IngestResponse{Status: "accepted"})
			return
		}
		var evt IngestEvent
		if err := json.Unmarshal(msg.Data, &evt); err != nil {
			replyIngest(msg, IngestResponse{Status: "error", Error: "malformed event: " + err.Error()})
			return
		}
		if !evt.MetricsRecorded {
			if evt.PublishedUnixNano == 0 {
				evt.PublishedUnixNano = time.Now().UnixNano()
			}
		}
		ctx := context.Background()
		var cancel context.CancelFunc
		if cfg.EnqueueTimeout > 0 {
			ctx, cancel = context.WithTimeout(ctx, cfg.EnqueueTimeout)
			defer cancel()
		}
		if err := pool.SubmitContext(ctx, IngestWork{Event: evt, EnqueuedAt: time.Now()}); err != nil {
			replyIngest(msg, IngestResponse{Status: "busy", Error: err.Error()})
			return
		}
		if !evt.MetricsRecorded {
			SharedIngestMetrics().RecordExternalPublished()
		}
		replyIngest(msg, IngestResponse{Status: "accepted"})
	})
	if err != nil {
		coreSub.Unsubscribe()
		if pool != nil {
			pool.Stop(context.Background())
		}
		return nil, err
	}
	if err := requestSub.SetPendingLimits(cfg.NATSPendingMsgLimit, cfg.NATSPendingByteLimit); err != nil {
		log.Printf("ingest relay: set request pending limits: %v", err)
	}
	return &IngestSubscription{coreSub: coreSub, requestSub: requestSub, pool: pool}, nil
}

func replyIngest(msg *natsgo.Msg, resp IngestResponse) {
	if msg.Reply == "" {
		return
	}
	data, err := json.Marshal(resp)
	if err != nil {
		return
	}
	if err := msg.Respond(data); err != nil {
		log.Printf("ingest relay: respond: %v", err)
	}
}

type IngestSubscription struct {
	coreSub    *natsgo.Subscription
	requestSub *natsgo.Subscription
	pool       *IngestRelayPool
}

func (s *IngestSubscription) Pending() (int, int, error) {
	if s == nil {
		return 0, 0, nil
	}
	var totalMsgs, totalBytes int
	for _, sub := range []*natsgo.Subscription{s.coreSub, s.requestSub} {
		if sub == nil {
			continue
		}
		msgs, bytes, err := sub.Pending()
		if err != nil {
			return totalMsgs, totalBytes, err
		}
		totalMsgs += msgs
		totalBytes += bytes
	}
	return totalMsgs, totalBytes, nil
}

func (s *IngestSubscription) Unsubscribe() error {
	if s == nil {
		return nil
	}
	var err error
	if s.coreSub != nil {
		err = s.coreSub.Unsubscribe()
	}
	if s.requestSub != nil {
		if subErr := s.requestSub.Unsubscribe(); err == nil {
			err = subErr
		}
	}
	if s.pool != nil {
		s.pool.Stop(context.Background())
	}
	return err
}

func (s *IngestSubscription) Stop(ctx context.Context) error {
	if s == nil {
		return nil
	}
	var err error
	if s.coreSub != nil {
		err = s.coreSub.Unsubscribe()
	}
	if s.requestSub != nil {
		if subErr := s.requestSub.Unsubscribe(); err == nil {
			err = subErr
		}
	}
	if s.pool != nil {
		s.pool.Stop(ctx)
	}
	return err
}

func (s *IngestSubscription) SnapshotIngest() IngestSnapshot {
	if s == nil || s.pool == nil {
		return IngestSnapshot{}
	}
	return s.pool.SnapshotIngest()
}

type IngestWork struct {
	Event      IngestEvent
	EnqueuedAt time.Time
}

type IngestRelayPool struct {
	workers int
	queues  []chan IngestWork
	handler func(IngestEvent) error

	stopCh chan struct{}
	doneCh chan struct{}
	once   sync.Once
	wg     sync.WaitGroup

	metrics relayMetrics
}

func newIngestRelayPool(cfg IngestRelayConfig, handler func(IngestEvent) error) *IngestRelayPool {
	if cfg.Workers <= 0 {
		cfg.Workers = 1
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = defaultIngestRelayQueueSize
	}
	queues := make([]chan IngestWork, cfg.Workers)
	for i := range queues {
		queues[i] = make(chan IngestWork, queueCapacityForWorker(cfg.QueueSize, cfg.Workers, i))
	}
	p := &IngestRelayPool{
		workers: cfg.Workers,
		queues:  queues,
		handler: handler,
		stopCh:  make(chan struct{}),
		doneCh:  make(chan struct{}),
	}
	p.metrics.QueueCap = totalQueueCap(queues)
	p.metrics.WorkerCount = cfg.Workers
	return p
}

func queueCapacityForWorker(totalCap, workers, workerIndex int) int {
	if workers <= 1 {
		return maxInt(totalCap, 1)
	}
	base := totalCap / workers
	remainder := totalCap % workers
	if workerIndex < remainder {
		base++
	}
	return maxInt(base, 1)
}

func totalQueueCap(queues []chan IngestWork) int {
	total := 0
	for _, q := range queues {
		total += cap(q)
	}
	return total
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (p *IngestRelayPool) Start() {
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.worker(p.queues[i])
	}
	go func() {
		p.wg.Wait()
		close(p.doneCh)
	}()
}

func (p *IngestRelayPool) Stop(ctx context.Context) {
	p.once.Do(func() {
		close(p.stopCh)
	})
	select {
	case <-p.doneCh:
	case <-ctx.Done():
	}
}

func (p *IngestRelayPool) Submit(work IngestWork) bool {
	return p.SubmitContext(context.Background(), work) == nil
}

func (p *IngestRelayPool) SubmitContext(ctx context.Context, work IngestWork) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if work.EnqueuedAt.IsZero() {
		work.EnqueuedAt = time.Now()
	}
	q := p.queues[p.partition(work.Event)]
	select {
	case q <- work:
		p.metrics.UpdateQueueLength(p.QueueLen())
		return nil
	case <-p.stopCh:
		return ErrIngestRejected
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *IngestRelayPool) QueueLen() int {
	total := 0
	for _, q := range p.queues {
		total += len(q)
	}
	return total
}

func (p *IngestRelayPool) SnapshotIngest() IngestSnapshot {
	return p.metrics.Snapshot(p.QueueLen())
}

func (p *IngestRelayPool) partition(evt IngestEvent) int {
	if len(p.queues) == 1 {
		return 0
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(evt.Tenant))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(evt.Zone))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(evt.DeviceType))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(evt.DeviceName))
	return int(h.Sum32() % uint32(len(p.queues)))
}

func (p *IngestRelayPool) worker(queue <-chan IngestWork) {
	defer p.wg.Done()
	for {
		select {
		case <-p.stopCh:
			return
		case work := <-queue:
			start := time.Now()
			p.metrics.UpdateQueueLength(p.QueueLen())
			p.metrics.IncrementActiveWorkers()
			SharedIngestMetrics().RecordStart(work.Event.PublishedUnixNano, start.UnixNano())
			err := p.handler(work.Event)
			finished := time.Now()
			SharedIngestMetrics().RecordFinish(work.Event.PublishedUnixNano, start.UnixNano(), finished.UnixNano(), err != nil)
			p.metrics.RecordProcessing(finished.Sub(work.EnqueuedAt), finished.Sub(start))
			p.metrics.DecrementActiveWorkers()
		}
	}
}

type relayMetrics struct {
	TotalProcessed    int64
	TotalLatency      int64
	TotalWorkerBusyNs int64
	PeakLatency       int64

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

func (m *relayMetrics) IncrementActiveWorkers() {
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

func (m *relayMetrics) DecrementActiveWorkers() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ActiveWorkers > 0 {
		m.ActiveWorkers--
	}
}

func (m *relayMetrics) RecordProcessing(latency, busy time.Duration) {
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

func (m *relayMetrics) UpdateQueueLength(length int) {
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

func (m *relayMetrics) updateQueueLenDurationLocked(now time.Time) {
	if !m.lastQueueUpdate.IsZero() {
		m.TotalQueueLenNs += int64(now.Sub(m.lastQueueUpdate)) * int64(m.QueueLength)
	}
	m.lastQueueUpdate = now
}

func (m *relayMetrics) Snapshot(queueLen int) IngestSnapshot {
	now := time.Now()
	m.mu.Lock()
	m.updateQueueLenDurationLocked(now)
	if queueLen > m.QueueLength {
		m.QueueLength = queueLen
	}
	queueLen = m.QueueLength
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

	return IngestSnapshot{
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
