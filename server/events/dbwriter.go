package events

import (
	"context"
	"log"
	"sync"
	"time"
)

const (
	dbBufferSize  = 100
	flushInterval = time.Second
)

// EventInserter is the interface for inserting event entries into the database.
type EventInserter interface {
	InsertEventEntries(ctx context.Context, entries []EventEntry) error
}

// DBPurger is the interface for deleting old event entries.
type DBPurger interface {
	PurgeEventsBefore(ctx context.Context, before time.Time) error
}

// EventWriter buffers events and batch-inserts them into the database.
// It is called by the NotificationHandler (or directly) rather than
// subscribing to NATS itself.
type EventWriter struct {
	inserter EventInserter
	mu       sync.Mutex
	buf      []EventEntry
	done     chan struct{}
	stopCh   chan struct{}
	purger   *purger
}

// NewEventWriter creates a new EventWriter. Call Start() to begin the flush loop.
func NewEventWriter(inserter EventInserter) *EventWriter {
	return &EventWriter{
		inserter: inserter,
		buf:      make([]EventEntry, 0, dbBufferSize),
		done:     make(chan struct{}),
		stopCh:   make(chan struct{}),
	}
}

// Start begins the background flush loop.
func (w *EventWriter) Start() {
	go w.flushLoop()
}

// EnablePurger starts the background purge loop.
func (w *EventWriter) EnablePurger(p DBPurger, retentionDays int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if p != nil && retentionDays > 0 {
		w.purger = newPurger(p, retentionDays)
		w.purger.start()
	}
}

// WriteEvent buffers a single event for batch insertion.
// Debug severity events are silently ignored (not persisted).
func (w *EventWriter) WriteEvent(entry EventEntry) {
	if Severity(entry.Severity) == Debug {
		return
	}
	w.mu.Lock()
	w.buf = append(w.buf, entry)
	shouldFlush := len(w.buf) >= dbBufferSize
	w.mu.Unlock()
	if shouldFlush {
		w.flush()
	}
}

// Stop flushes the buffer, stops the purge loop, and terminates the flush loop.
func (w *EventWriter) Stop() {
	close(w.stopCh)
	<-w.done
	w.flush()
	w.mu.Lock()
	p := w.purger
	w.mu.Unlock()
	if p != nil {
		p.stop()
	}
}

func (w *EventWriter) flushLoop() {
	defer close(w.done)
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			w.flush()
		case <-w.stopCh:
			return
		}
	}
}

func (w *EventWriter) flush() {
	w.mu.Lock()
	if len(w.buf) == 0 {
		w.mu.Unlock()
		return
	}
	entries := w.buf
	w.buf = make([]EventEntry, 0, dbBufferSize)
	w.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := w.inserter.InsertEventEntries(ctx, entries); err != nil {
		log.Printf("events: dbwriter: flush: %v", err)
	}
}

// purger runs a background goroutine that periodically deletes old events.
type purger struct {
	p             DBPurger
	retentionDays int
	stopCh        chan struct{}
	done          chan struct{}
}

func newPurger(p DBPurger, retentionDays int) *purger {
	return &purger{
		p:             p,
		retentionDays: retentionDays,
		stopCh:        make(chan struct{}),
		done:          make(chan struct{}),
	}
}

func (pg *purger) start() {
	go pg.loop()
}

func (pg *purger) stop() {
	close(pg.stopCh)
	<-pg.done
}

func (pg *purger) loop() {
	defer close(pg.done)
	pg.purge()
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			pg.purge()
		case <-pg.stopCh:
			return
		}
	}
}

func (pg *purger) purge() {
	cutoff := time.Now().Add(-time.Duration(pg.retentionDays) * 24 * time.Hour)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = pg.p.PurgeEventsBefore(ctx, cutoff)
}
