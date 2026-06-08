// Package metrics collects server health stats and publishes them to the RTDB.
package metrics

import (
	"log"
	"os"
	"runtime"
	"time"

	natsgo "github.com/nats-io/nats.go"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/process"
	"github.com/xact-iot/xact/rtdb/ingest"
	xnats "github.com/xact-iot/xact/rtdb/nats"
	"github.com/xact-iot/xact/sqldb"
)

// Sampler is anything that can provide ingest queue stats.
// *mqtt.Client satisfies this interface.
type Sampler = ingest.IngestSampler

// Collector samples CPU, memory, goroutines, and ingest queue stats once per
// minute and publishes them to the RTDB under default/system/<serverName>.
type Collector struct {
	nc         *natsgo.Conn
	serverName string
	sampler    Sampler // optional; nil means no ingest stats
	relayQ     Sampler // optional; shared ingest relay worker pool stats
	metricQ    *sqldb.MetricWriter
	ingestSub  pendingSubscription
	process    *process.Process
	stopCh     chan struct{}
	done       chan struct{}
}

type pendingSubscription interface {
	Pending() (int, int, error)
}

// New creates a Collector. sampler may be nil if there is no MQTT client.
func New(nc *natsgo.Conn, sampler Sampler) *Collector {
	proc, err := process.NewProcess(int32(os.Getpid()))
	if err != nil {
		log.Printf("metrics: process.NewProcess: %v", err)
	}
	return &Collector{
		nc:         nc,
		serverName: resolveServerName(),
		sampler:    sampler,
		process:    proc,
		stopCh:     make(chan struct{}),
		done:       make(chan struct{}),
	}
}

// SetIngestSubscription lets the collector include NATS pending counts for the
// shared ingest subscription.
func (c *Collector) SetIngestSubscription(sub interface {
	pendingSubscription
	ingest.IngestSampler
}) {
	c.ingestSub = sub
	c.relayQ = sub
}

func (c *Collector) SetMetricWriter(writer *sqldb.MetricWriter) {
	c.metricQ = writer
}

// Start launches the background collection loop.
func (c *Collector) Start() {
	go c.run()
}

// Stop signals the loop to stop and waits for it to exit.
func (c *Collector) Stop() {
	close(c.stopCh)
	<-c.done
}

func (c *Collector) run() {
	defer close(c.done)

	// Prime the CPU sampler so the first real measurement is a valid delta.
	cpu.Percent(0, true) //nolint:errcheck
	if c.process != nil {
		c.process.Percent(0) //nolint:errcheck
	}

	// Accumulate ingest samples within each 1-minute window.
	var (
		sampledWindows    int
		windowDurationNs  int64
		totalQueueLenNs   int64
		maxQueueLen       float64
		totalWorkerBusyNs int64
		maxWorkers        float64
		totalProcessed    int64
		totalLatencyNs    int64
		maxLatencyNs      float64
		lastProcessed     int64
		lastLatencyNs     int64
		lastWorkerBusyNs  int64
		lastQueueLenNs    int64
		lastSampleAt      time.Time
	)
	sharedState := sharedIngestReportState{
		lastSnap: ingest.SharedIngestMetrics().Snapshot(),
		lastAt:   time.Now(),
	}
	resetIngest := func() {
		sampledWindows = 0
		windowDurationNs = 0
		totalQueueLenNs, maxQueueLen = 0, 0
		totalWorkerBusyNs, maxWorkers = 0, 0
		totalProcessed = 0
		totalLatencyNs, maxLatencyNs = 0, 0
		lastSampleAt = time.Now()
	}

	// Sample MQTT worker queue every 5 seconds; publish system stats and shared
	// ingest health every 10 seconds.
	ingestTicker := time.NewTicker(5 * time.Second)
	reportTicker := time.NewTicker(10 * time.Second)
	defer ingestTicker.Stop()
	defer reportTicker.Stop()

	time.Sleep(2 * time.Second)
	sharedReport := c.buildSharedIngestReport(&sharedState)
	c.publish(sampledWindows, windowDurationNs, totalQueueLenNs, maxQueueLen,
		totalWorkerBusyNs, maxWorkers, totalProcessed, totalLatencyNs, maxLatencyNs, sharedReport)
	lastSampleAt = time.Now()

	for {
		select {
		case <-c.stopCh:
			return

		case <-ingestTicker.C:
			if c.sampler == nil {
				continue
			}
			now := time.Now()
			if !lastSampleAt.IsZero() {
				windowDurationNs += now.Sub(lastSampleAt).Nanoseconds()
			}
			lastSampleAt = now

			snap := c.sampler.SnapshotIngest()
			sampledWindows++
			queueLenDelta := snap.TotalQueueLenNs - lastQueueLenNs
			if queueLenDelta < 0 {
				queueLenDelta = 0
			}
			lastQueueLenNs = snap.TotalQueueLenNs
			totalQueueLenNs += queueLenDelta

			peakQueueLen := float64(maxInt(snap.QueueLen, snap.PeakQueueLen))
			if peakQueueLen > maxQueueLen {
				maxQueueLen = peakQueueLen
			}

			workerBusyDelta := snap.TotalWorkerBusyNs - lastWorkerBusyNs
			if workerBusyDelta < 0 {
				workerBusyDelta = 0
			}
			lastWorkerBusyNs = snap.TotalWorkerBusyNs
			totalWorkerBusyNs += workerBusyDelta

			peakWorkers := float64(maxInt64(snap.ActiveWorkers, snap.PeakActiveWorkers))
			if peakWorkers > maxWorkers {
				maxWorkers = peakWorkers
			}

			processedDelta := snap.TotalProcessed - lastProcessed
			if processedDelta < 0 {
				processedDelta = 0
			}
			lastProcessed = snap.TotalProcessed
			totalProcessed += processedDelta

			latencyDelta := snap.TotalLatencyNs - lastLatencyNs
			if latencyDelta < 0 {
				latencyDelta = 0
			}
			lastLatencyNs = snap.TotalLatencyNs
			totalLatencyNs += latencyDelta

			peakNs := float64(snap.PeakLatencyNs)
			if peakNs > maxLatencyNs {
				maxLatencyNs = peakNs
			}

		case <-reportTicker.C:
			sharedReport := c.buildSharedIngestReport(&sharedState)
			c.publish(sampledWindows, windowDurationNs, totalQueueLenNs, maxQueueLen,
				totalWorkerBusyNs, maxWorkers, totalProcessed, totalLatencyNs, maxLatencyNs, sharedReport)
			resetIngest()
		}
	}
}

func (c *Collector) publish(
	sampledWindows int,
	windowDurationNs, totalQueueLenNs int64,
	maxQueueLen float64,
	totalWorkerBusyNs int64,
	maxWorkers float64,
	totalProcessed, totalLatencyNs int64,
	maxLatencyNs float64,
	sharedReport sharedIngestReport,
) {
	// ── CPU ──────────────────────────────────────────────────────────────────
	cpuPercents, err := cpu.Percent(0, true)
	if err != nil {
		log.Printf("metrics: cpu.Percent: %v", err)
	}
	totalCPU := 0.0
	for _, p := range cpuPercents {
		totalCPU += p
	}
	processCPU := 0.0
	if c.process != nil {
		processPercent, err := c.process.Percent(0)
		if err != nil {
			log.Printf("metrics: process.Percent: %v", err)
		} else {
			processCPU = processPercent
		}
	}

	// ── Memory ───────────────────────────────────────────────────────────────
	var appMem runtime.MemStats
	runtime.ReadMemStats(&appMem)
	heapAllocGB := float64(appMem.HeapAlloc) / (1024 * 1024)
	heapTotalGB := float64(appMem.HeapInuse) / (1024 * 1024)

	// Also capture available system memory for context (optional, no-op on error).
	vmStat, _ := mem.VirtualMemory()
	sysAvailGB := 0.0
	if vmStat != nil {
		sysAvailGB = float64(vmStat.Available) / (1024 * 1024 * 1024)
	}
	_ = sysAvailGB // available for future use

	// ── Goroutines ───────────────────────────────────────────────────────────
	goroutines := runtime.NumGoroutine()

	// ── Build TagData ─────────────────────────────────────────────────────────

	groups := make(map[string]map[string]any)

	groups["CPU"] = map[string]any{
		"SystemLoad": map[string]any{
			"value":    totalCPU,
			"units":    "%",
			"deadband": 2.0,
			"history":  true,
		},
		"ProcessLoad": map[string]any{
			"value":    processCPU,
			"units":    "%",
			"deadband": 2.0,
			"history":  true,
		},
		"Goroutines": map[string]any{
			"value":    float64(goroutines),
			"deadband": 5.0,
			"history":  true,
		},
	}

	groups["Memory"] = map[string]any{
		"HeapLive":  map[string]any{"value": heapAllocGB, "units": "MB", "deadband": 5.0, "history": true},
		"HeapTotal": map[string]any{"value": heapTotalGB, "units": "MB", "deadband": 5.0, "history": true},
	}

	if c.sampler != nil && sampledWindows > 0 {
		avgQLen := 0.0
		avgWorkers := 0.0
		if windowDurationNs > 0 {
			avgQLen = float64(totalQueueLenNs) / float64(windowDurationNs)
			avgWorkers = float64(totalWorkerBusyNs) / float64(windowDurationNs)
		}
		avgLatencyMs := 0.0
		if totalProcessed > 0 {
			avgLatencyMs = float64(totalLatencyNs) / float64(totalProcessed) / 1e6
		}
		maxLatencyMs := maxLatencyNs / 1e6

		groups["IngestMQTT"] = map[string]any{
			"AvgQueueLen":  map[string]any{"value": avgQLen, "deadband": 2.0, "history": true},
			"MaxQueueLen":  map[string]any{"value": maxQueueLen, "deadband": 2.0, "history": true},
			"AvgWorkers":   map[string]any{"value": avgWorkers, "deadband": 2.0, "history": true},
			"MaxWorkers":   map[string]any{"value": maxWorkers, "deadband": 1.0, "history": true},
			"AvgLatencyMs": map[string]any{"value": avgLatencyMs, "units": "ms", "deadband": 20.0, "history": true},
			"MaxLatencyMs": map[string]any{"value": maxLatencyMs, "units": "ms", "deadband": 20.0, "history": true},
		}
	}

	groups["Ingest"] = map[string]any{
		"PublishedRatePerSec": map[string]any{"value": sharedReport.PublishedRatePerSec, "units": "msg/s", "deadband": 0.1, "history": true, "description": "Rate of ingest events published into or observed on the shared ingest relay."},
		"CompletedRatePerSec": map[string]any{"value": sharedReport.CompletedRatePerSec, "units": "msg/s", "deadband": 0.1, "history": true, "description": "Rate of shared ingest events this server finished processing successfully."},
		"InFlight":            map[string]any{"value": float64(sharedReport.InFlight), "deadband": 1.0, "history": true, "description": "Number of shared ingest events currently being processed by this server."},
		"NatsPendingMsgs":     map[string]any{"value": float64(sharedReport.NatsPendingMsgs), "deadband": 1.0, "history": true, "description": "NATS messages queued on this server's shared ingest subscription."},
		"NatsPendingBytes":    map[string]any{"value": float64(sharedReport.NatsPendingBytes), "units": "bytes", "deadband": 1024.0, "history": true, "description": "Bytes queued on this server's shared ingest subscription."},
		"AvgLagMs":            map[string]any{"value": sharedReport.AvgLagMs, "units": "ms", "deadband": 10.0, "history": true, "description": "Average time from shared ingest publish to processor completion during the reporting window."},
		"MaxLagMs":            map[string]any{"value": sharedReport.MaxLagMs, "units": "ms", "deadband": 10.0, "history": true, "description": "Maximum time from shared ingest publish to processor completion during the reporting window."},
		"AvgProcessMs":        map[string]any{"value": sharedReport.AvgProcessMs, "units": "ms", "deadband": 10.0, "history": true, "description": "Average time spent inside processor.WriteDeviceData during the reporting window."},
		"MaxProcessMs":        map[string]any{"value": sharedReport.MaxProcessMs, "units": "ms", "deadband": 10.0, "history": true, "description": "Maximum time spent inside processor.WriteDeviceData during the reporting window."},
		"FailedRatePerSec":    map[string]any{"value": sharedReport.FailedRatePerSec, "units": "msg/s", "deadband": 0.1, "history": true, "description": "Rate of shared ingest events whose processor call returned an error."},
		"HealthCode":          map[string]any{"value": float64(sharedReport.HealthCode), "deadband": 1.0, "history": true, "description": "Shared ingest health code: 0 healthy, 1 warning, 2 critical."},
	}

	if c.relayQ != nil {
		snap := c.relayQ.SnapshotIngest()
		groups["IngestQ"] = map[string]any{
			"QueueLen":          map[string]any{"value": float64(snap.QueueLen), "deadband": 1.0, "history": true, "description": "Shared ingest worker-pool messages waiting behind the NATS subscription callback."},
			"QueueCap":          map[string]any{"value": float64(snap.QueueCap), "deadband": 1.0, "history": false, "description": "Total capacity of the shared ingest worker-pool queues."},
			"WorkerCount":       map[string]any{"value": float64(snap.WorkerCount), "deadband": 1.0, "history": false, "description": "Configured shared ingest processing workers."},
			"ActiveWorkers":     map[string]any{"value": float64(snap.ActiveWorkers), "deadband": 1.0, "history": true, "description": "Shared ingest workers currently processing messages."},
			"PeakQueueLen":      map[string]any{"value": float64(snap.PeakQueueLen), "deadband": 1.0, "history": true, "description": "Peak shared ingest worker queue length since the previous metrics report."},
			"PeakActiveWorkers": map[string]any{"value": float64(snap.PeakActiveWorkers), "deadband": 1.0, "history": true, "description": "Peak active shared ingest workers since the previous metrics report."},
			"MaxQueueLen":       map[string]any{"value": float64(snap.MaxQueueLen), "deadband": 1.0, "history": true, "description": "Highest shared ingest worker queue length observed since server start."},
			"MaxActiveWorkers":  map[string]any{"value": float64(snap.MaxActiveWorkers), "deadband": 1.0, "history": true, "description": "Highest active shared ingest worker count observed since server start."},
		}
	}

	persistQueueLen, persistQueueCap := xnats.PersistStoreQueueStats()
	groups["SideEffects"] = map[string]any{
		"PersistQueueLen":   map[string]any{"value": float64(persistQueueLen), "deadband": 1.0, "history": false, "description": "Queued RTDB_PERSIST writes waiting for coalesced NATS KV flush."},
		"PersistQueueCap":   map[string]any{"value": float64(persistQueueCap), "deadband": 1.0, "history": false, "description": "Capacity of the RTDB_PERSIST async write queue."},
		"MetricQueueLen":    map[string]any{"value": float64(c.metricQueueLen()), "deadband": 1.0, "history": false, "description": "Queued historyrecorder metric points waiting for batched SQL insert."},
		"MetricQueueCap":    map[string]any{"value": float64(c.metricQueueCap()), "deadband": 1.0, "history": false, "description": "Capacity of the historyrecorder metric writer queue."},
		"MetricDropped":     map[string]any{"value": float64(c.metricDropped()), "deadband": 1.0, "history": false, "description": "Total historyrecorder metric points dropped because the SQL writer queue was full."},
		"GoRoutines":        map[string]any{"value": float64(goroutines), "deadband": 5.0, "history": true, "description": "Current Go goroutine count."},
		"ProcessCPUPercent": map[string]any{"value": processCPU, "units": "%", "deadband": 2.0, "history": true, "description": "CPU used by this server process; can exceed 100% on multicore systems."},
	}

	tagData := ingest.TagData{
		DirectTags:  nil,
		Groups:      groups,
		TSUnixMilli: time.Now().UnixMilli(),
	}

	if err := ingest.PublishIngest(c.nc, "default", "", "system", c.serverName, tagData); err != nil {
		log.Printf("metrics: publish: %v", err)
	}
}

func (c *Collector) metricQueueLen() int {
	if c.metricQ == nil {
		return 0
	}
	return c.metricQ.QueueLen()
}

func (c *Collector) metricQueueCap() int {
	if c.metricQ == nil {
		return 0
	}
	return c.metricQ.QueueCap()
}

func (c *Collector) metricDropped() int64 {
	if c.metricQ == nil {
		return 0
	}
	return c.metricQ.Dropped()
}

type sharedIngestReportState struct {
	lastSnap            ingest.SharedMetricsSnapshot
	lastAt              time.Time
	lastPendingMsgs     int
	pendingGrowthStreak int
}

type sharedIngestReport struct {
	PublishedRatePerSec float64
	CompletedRatePerSec float64
	InFlight            int64
	NatsPendingMsgs     int
	NatsPendingBytes    int
	AvgLagMs            float64
	MaxLagMs            float64
	AvgProcessMs        float64
	MaxProcessMs        float64
	FailedRatePerSec    float64
	HealthCode          int
}

func (c *Collector) buildSharedIngestReport(state *sharedIngestReportState) sharedIngestReport {
	now := time.Now()
	snap := ingest.SharedIngestMetrics().Snapshot()
	pendingMsgs, pendingBytes := c.ingestPending()
	return buildSharedIngestReportFromSnapshot(snap, state, now, pendingMsgs, pendingBytes)
}

func buildSharedIngestReportFromSnapshot(
	snap ingest.SharedMetricsSnapshot,
	state *sharedIngestReportState,
	now time.Time,
	pendingMsgs, pendingBytes int,
) sharedIngestReport {
	elapsedSec := now.Sub(state.lastAt).Seconds()
	if elapsedSec <= 0 {
		elapsedSec = 1
	}

	if pendingMsgs > state.lastPendingMsgs {
		state.pendingGrowthStreak++
	} else {
		state.pendingGrowthStreak = 0
	}

	publishedDelta := positiveDelta(snap.TotalPublished, state.lastSnap.TotalPublished)
	completedDelta := positiveDelta(snap.TotalCompleted, state.lastSnap.TotalCompleted)
	failedDelta := positiveDelta(snap.TotalFailed, state.lastSnap.TotalFailed)
	finishedDelta := completedDelta + failedDelta
	lagDelta := positiveDelta(snap.TotalEndToEndLatencyNs, state.lastSnap.TotalEndToEndLatencyNs)
	processDelta := positiveDelta(snap.TotalProcessLatencyNs, state.lastSnap.TotalProcessLatencyNs)

	report := sharedIngestReport{
		PublishedRatePerSec: float64(publishedDelta) / elapsedSec,
		CompletedRatePerSec: float64(completedDelta) / elapsedSec,
		InFlight:            snap.InFlight,
		NatsPendingMsgs:     pendingMsgs,
		NatsPendingBytes:    pendingBytes,
		MaxLagMs:            float64(snap.PeakEndToEndLatencyNs) / 1e6,
		MaxProcessMs:        float64(snap.PeakProcessLatencyNs) / 1e6,
		FailedRatePerSec:    float64(failedDelta) / elapsedSec,
	}
	if finishedDelta > 0 {
		report.AvgLagMs = float64(lagDelta) / float64(finishedDelta) / 1e6
		report.AvgProcessMs = float64(processDelta) / float64(finishedDelta) / 1e6
	}
	report.HealthCode = classifySharedIngestHealth(report, state.pendingGrowthStreak)

	state.lastSnap = snap
	state.lastAt = now
	state.lastPendingMsgs = pendingMsgs
	return report
}

func (c *Collector) ingestPending() (int, int) {
	if c.ingestSub == nil {
		return 0, 0
	}
	msgs, bytes, err := c.ingestSub.Pending()
	if err != nil {
		log.Printf("metrics: ingest pending: %v", err)
		return 0, 0
	}
	return msgs, bytes
}

func classifySharedIngestHealth(report sharedIngestReport, pendingGrowthStreak int) int {
	if report.MaxLagMs >= float64(ingest.DefaultLagCriticalNs)/1e6 || pendingGrowthStreak >= 2 {
		return ingest.HealthCritical
	}
	if report.MaxLagMs >= float64(ingest.DefaultLagWarningNs)/1e6 || pendingGrowthStreak > 0 {
		return ingest.HealthWarning
	}
	if report.PublishedRatePerSec > report.CompletedRatePerSec && report.NatsPendingMsgs > 0 {
		return ingest.HealthWarning
	}
	return ingest.HealthHealthy
}

func positiveDelta(now, previous int64) int64 {
	delta := now - previous
	if delta < 0 {
		return 0
	}
	return delta
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func resolveServerName() string {
	if name, err := os.Hostname(); err == nil && name != "" {
		return name
	}
	if name := os.Getenv("SERVER_NAME"); name != "" {
		return name
	}
	return "server"
}
