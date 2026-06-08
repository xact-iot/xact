package ingest

// IngestSnapshot is a point-in-time sample from the ingest worker pool.
// Peak fields are reset by the sampler after each read.
type IngestSnapshot struct {
	QueueLen          int
	QueueCap          int
	PeakQueueLen      int
	MaxQueueLen       int
	WorkerCount       int
	ActiveWorkers     int64
	PeakActiveWorkers int64
	MaxActiveWorkers  int64
	TotalProcessed    int64
	TotalLatencyNs    int64
	TotalWorkerBusyNs int64
	TotalQueueLenNs   int64
	PeakLatencyNs     int64
}

// IngestSampler is implemented by anything that can provide ingest queue stats.
// The MQTT client worker pool satisfies this interface.
type IngestSampler interface {
	SnapshotIngest() IngestSnapshot
}
