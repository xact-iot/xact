package nats

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// PersistEntry is the value stored in the RTDB_PERSIST KV bucket.
type PersistEntry struct {
	Value     any   `json:"value"`
	Timestamp int64 `json:"timestamp"`
}

const (
	persistQueueSize     = 8192
	persistFlushInterval = 100 * time.Millisecond
	persistFlushBatch    = 1000
)

// PersistStore wraps a NATS JetStream KV bucket for durable tag-value storage.
type PersistStore struct {
	kv     jetstream.KeyValue
	writes chan persistWrite
	stopCh chan struct{}
	done   chan struct{}

	stopOnce sync.Once
}

type persistWrite struct {
	key   string
	value any
}

var (
	persistStore *PersistStore
)

func PreparePersistStore(natsConn *nats.Conn) error {
	js, err := jetstream.New(natsConn)
	if err != nil {
		return fmt.Errorf("persist store: %w", err)
	}
	kv, err := js.CreateOrUpdateKeyValue(context.Background(), jetstream.KeyValueConfig{
		Bucket:      "RTDB_PERSIST",
		Description: "Last-known tag values - restored on startup",
		History:     1,
		Storage:     jetstream.FileStorage,
	})
	if err != nil {
		return fmt.Errorf("persist store: create KV: %w", err)
	}
	persistStore = &PersistStore{
		kv:     kv,
		writes: make(chan persistWrite, persistQueueSize),
		stopCh: make(chan struct{}),
		done:   make(chan struct{}),
	}
	go persistStore.run()
	return nil
}

// GetPersistStore returns the singleton PersistStore, creating the RTDB_PERSIST
// bucket on first call. Requires PrepareLockStore to have been called first
// (which sets the package-level natsConn).
func GetPersistStore() *PersistStore {
	return persistStore
}

// Put serialises value and stores it under key.
func (s *PersistStore) Put(key string, value any) error {
	if s == nil {
		return nil
	}
	select {
	case s.writes <- persistWrite{key: key, value: value}:
		return nil
	case <-s.stopCh:
		return nil
	default:
		// Apply backpressure only when the async queue is full. This keeps normal
		// ingest fast while preventing unbounded memory growth under sustained load.
		select {
		case s.writes <- persistWrite{key: key, value: value}:
			return nil
		case <-s.stopCh:
			return nil
		}
	}
}

func (s *PersistStore) QueueLen() int {
	if s == nil {
		return 0
	}
	return len(s.writes)
}

func (s *PersistStore) QueueCap() int {
	if s == nil {
		return 0
	}
	return cap(s.writes)
}

func PersistStoreQueueStats() (int, int) {
	if persistStore == nil {
		return 0, 0
	}
	return persistStore.QueueLen(), persistStore.QueueCap()
}

func (s *PersistStore) Stop(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.stopOnce.Do(func() {
		close(s.stopCh)
	})
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func StopPersistStore(ctx context.Context) error {
	if persistStore == nil {
		return nil
	}
	return persistStore.Stop(ctx)
}

func (s *PersistStore) run() {
	defer close(s.done)

	ticker := time.NewTicker(persistFlushInterval)
	defer ticker.Stop()

	pending := make(map[string]PersistEntry)
	add := func(w persistWrite) {
		if w.key == "" {
			return
		}
		pending[w.key] = PersistEntry{Value: w.value, Timestamp: time.Now().UnixMilli()}
	}
	flush := func() {
		if len(pending) == 0 {
			return
		}
		for key, entry := range pending {
			if err := s.putNow(key, entry); err != nil {
				log.Printf("[persist] put %s: %v", key, err)
			}
			delete(pending, key)
		}
	}

	for {
		select {
		case w := <-s.writes:
			add(w)
			if len(pending) >= persistFlushBatch {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-s.stopCh:
			draining := true
			for draining {
				select {
				case w := <-s.writes:
					add(w)
				default:
					draining = false
				}
			}
			flush()
			return
		}
	}
}

func (s *PersistStore) putNow(key string, entry PersistEntry) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	_, err = s.kv.Put(context.Background(), key, data)
	return err
}

// Get retrieves and deserialises the entry for key. Returns nil, nil when the
// key does not exist.
func (s *PersistStore) Get(key string) (*PersistEntry, error) {
	e, err := s.kv.Get(context.Background(), key)
	if err == jetstream.ErrKeyNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var entry PersistEntry
	if err := json.Unmarshal(e.Value(), &entry); err != nil {
		return nil, err
	}
	return &entry, nil
}
