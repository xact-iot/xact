package sqldb

import (
	"context"
	"errors"
	"log"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

var ErrMetricWriterStopped = errors.New("metric writer stopped")
var ErrMetricWriterQueueFull = errors.New("metric writer queue full")

// MetricInserter is the subset of DB needed by MetricWriter.
type MetricInserter interface {
	InsertMetrics(ctx context.Context, orgName string, entries []MetricEntry) error
}

type MetricWriterConfig struct {
	QueueSize      int
	BatchSize      int
	FlushInterval  time.Duration
	InsertTimeout  time.Duration
	EnqueueTimeout time.Duration
}

func DefaultMetricWriterConfig() MetricWriterConfig {
	return MetricWriterConfig{
		QueueSize:      8192,
		BatchSize:      500,
		FlushInterval:  time.Second,
		InsertTimeout:  5 * time.Second,
		EnqueueTimeout: 50 * time.Millisecond,
	}
}

func MetricWriterConfigFromEnv() MetricWriterConfig {
	cfg := DefaultMetricWriterConfig()
	cfg.QueueSize = metricWriterEnvInt("METRIC_WRITER_QUEUE_SIZE", cfg.QueueSize)
	cfg.BatchSize = metricWriterEnvInt("METRIC_WRITER_BATCH_SIZE", cfg.BatchSize)
	cfg.FlushInterval = metricWriterEnvDurationMs("METRIC_WRITER_FLUSH_INTERVAL_MS", cfg.FlushInterval)
	cfg.InsertTimeout = metricWriterEnvDurationMs("METRIC_WRITER_INSERT_TIMEOUT_MS", cfg.InsertTimeout)
	cfg.EnqueueTimeout = metricWriterEnvDurationMs("METRIC_WRITER_ENQUEUE_TIMEOUT_MS", cfg.EnqueueTimeout)
	return cfg
}

func metricWriterEnvInt(name string, fallback int) int {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		log.Printf("metric writer: invalid %s=%q, using %d", name, raw, fallback)
		return fallback
	}
	return value
}

func metricWriterEnvDurationMs(name string, fallback time.Duration) time.Duration {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		log.Printf("metric writer: invalid %s=%q, using %s", name, raw, fallback)
		return fallback
	}
	return time.Duration(value) * time.Millisecond
}

type metricWrite struct {
	org   string
	entry MetricEntry
}

// MetricWriter batches time-series metric inserts off the ingest hot path.
type MetricWriter struct {
	db     MetricInserter
	cfg    MetricWriterConfig
	input  chan metricWrite
	stopCh chan struct{}
	done   chan struct{}

	stopOnce sync.Once
	stopped  atomic.Bool
	dropped  int64
}

func NewMetricWriter(db MetricInserter, cfg MetricWriterConfig) *MetricWriter {
	if db == nil {
		return nil
	}
	cfg = normalizeMetricWriterConfig(cfg)
	w := &MetricWriter{
		db:     db,
		cfg:    cfg,
		input:  make(chan metricWrite, cfg.QueueSize),
		stopCh: make(chan struct{}),
		done:   make(chan struct{}),
	}
	go w.run()
	return w
}

func normalizeMetricWriterConfig(cfg MetricWriterConfig) MetricWriterConfig {
	defaults := DefaultMetricWriterConfig()
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = defaults.QueueSize
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = defaults.BatchSize
	}
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = defaults.FlushInterval
	}
	if cfg.InsertTimeout <= 0 {
		cfg.InsertTimeout = defaults.InsertTimeout
	}
	if cfg.EnqueueTimeout <= 0 {
		cfg.EnqueueTimeout = defaults.EnqueueTimeout
	}
	return cfg
}

func (w *MetricWriter) Write(org string, entry MetricEntry) error {
	if w == nil {
		return ErrMetricWriterStopped
	}
	ctx := context.Background()
	var cancel context.CancelFunc
	if w.cfg.EnqueueTimeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, w.cfg.EnqueueTimeout)
		defer cancel()
	}
	return w.Enqueue(ctx, org, entry)
}

func (w *MetricWriter) Enqueue(ctx context.Context, org string, entry MetricEntry) error {
	if w == nil || w.stopped.Load() {
		return ErrMetricWriterStopped
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case w.input <- metricWrite{org: org, entry: entry}:
		return nil
	case <-w.stopCh:
		return ErrMetricWriterStopped
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *MetricWriter) TryWrite(org string, entry MetricEntry) error {
	if w == nil || w.stopped.Load() {
		return ErrMetricWriterStopped
	}
	select {
	case w.input <- metricWrite{org: org, entry: entry}:
		return nil
	case <-w.stopCh:
		return ErrMetricWriterStopped
	default:
		atomic.AddInt64(&w.dropped, 1)
		return ErrMetricWriterQueueFull
	}
}

func (w *MetricWriter) QueueLen() int {
	if w == nil {
		return 0
	}
	return len(w.input)
}

func (w *MetricWriter) QueueCap() int {
	if w == nil {
		return 0
	}
	return cap(w.input)
}

func (w *MetricWriter) Dropped() int64 {
	if w == nil {
		return 0
	}
	return atomic.LoadInt64(&w.dropped)
}

func (w *MetricWriter) Stop(ctx context.Context) error {
	if w == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	w.stopOnce.Do(func() {
		w.stopped.Store(true)
		close(w.stopCh)
	})
	select {
	case <-w.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *MetricWriter) run() {
	defer close(w.done)

	ticker := time.NewTicker(w.cfg.FlushInterval)
	defer ticker.Stop()

	pending := make(map[string][]MetricEntry)
	total := 0
	add := func(item metricWrite) {
		if item.org == "" {
			return
		}
		pending[item.org] = append(pending[item.org], item.entry)
		total++
	}
	flush := func() {
		if total == 0 {
			return
		}
		if err := w.flushAll(pending); err != nil {
			log.Printf("metric writer: flush failed: %v", err)
		}
		total = 0
	}

	for {
		select {
		case item := <-w.input:
			add(item)
			if total >= w.cfg.BatchSize || len(pending[item.org]) >= w.cfg.BatchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-w.stopCh:
			draining := true
			for draining {
				select {
				case item := <-w.input:
					add(item)
				default:
					draining = false
				}
			}
			flush()
			return
		}
	}
}

func (w *MetricWriter) flushAll(pending map[string][]MetricEntry) error {
	var firstErr error
	for org, entries := range pending {
		if len(entries) == 0 {
			delete(pending, org)
			continue
		}
		for start := 0; start < len(entries); start += w.cfg.BatchSize {
			end := start + w.cfg.BatchSize
			if end > len(entries) {
				end = len(entries)
			}
			ctx, cancel := context.WithTimeout(context.Background(), w.cfg.InsertTimeout)
			err := w.db.InsertMetrics(ctx, org, entries[start:end])
			cancel()
			if err != nil && firstErr == nil {
				firstErr = err
			}
		}
		delete(pending, org)
	}
	return firstErr
}
