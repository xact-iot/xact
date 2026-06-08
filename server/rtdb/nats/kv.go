package nats

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
)

// HistoryValue represents the stored value in NATS KV
type HistoryValue struct {
	Value     any   `json:"value"`
	Timestamp int64 `json:"timestamp"`
}

// HistoryStore manages history using NATS KV's native history feature
// NATS KV automatically maintains version history when configured with History > 1
type HistoryStore struct {
	js nats.JetStreamContext
	kv nats.KeyValue
}

// NewHistoryStore creates a new history store with NATS KV native history
// depth: number of historical versions to keep per key (configures KV History)
func NewHistoryStore(js nats.JetStreamContext, depth int) (*HistoryStore, error) {
	// Create KV bucket with native history support
	// History > 1 enables automatic versioning on each Put
	kv, err := js.CreateKeyValue(&nats.KeyValueConfig{
		Bucket:       "RTDB_HISTORY",
		Description:  "RTDB Tag History - Native KV versioning",
		MaxValueSize: 1024 * 1024,  // 1MB
		History:      uint8(depth), // Native history depth (max 255)
		TTL:          15 * time.Minute,
	})
	if err != nil {
		// If already exists, try to get it
		kv, err = js.KeyValue("RTDB_HISTORY")
		if err != nil {
			return nil, fmt.Errorf("failed to create history KV: %w", err)
		}
	}

	return &HistoryStore{
		js: js,
		kv: kv,
	}, nil
}

// Record stores the current value - NATS KV handles versioning automatically
func (hs *HistoryStore) Record(path string, value any) error {
	entry := HistoryValue{
		Value:     value,
		Timestamp: time.Now().UnixMilli(),
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal value: %w", err)
	}

	_, err = hs.kv.Put(path, data)
	if err != nil {
		return fmt.Errorf("failed to store value: %w", err)
	}

	return nil
}

// Get retrieves the current value for a path
func (hs *HistoryStore) Get(path string) (*HistoryValue, error) {
	entry, err := hs.kv.Get(path)
	if err != nil {
		if err == nats.ErrKeyNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get value: %w", err)
	}

	var value HistoryValue
	if err := json.Unmarshal(entry.Value(), &value); err != nil {
		return nil, fmt.Errorf("failed to unmarshal value: %w", err)
	}

	return &value, nil
}

// GetHistory retrieves all historical versions for a path
// Returns slice from oldest to newest
func (hs *HistoryStore) GetHistory(path string) ([]HistoryValue, error) {
	// Use NATS KV's History() to get all versions
	history, err := hs.kv.History(path)
	if err != nil {
		if err == nats.ErrKeyNotFound {
			return []HistoryValue{}, nil
		}
		return nil, fmt.Errorf("failed to get history: %w", err)
	}

	var values []HistoryValue
	for _, entry := range history {
		var value HistoryValue
		if err := json.Unmarshal(entry.Value(), &value); err != nil {
			continue // Skip corrupted entries
		}
		values = append(values, value)
	}

	return values, nil
}

// Delete removes a key and all its history
func (hs *HistoryStore) Delete(path string) error {
	err := hs.kv.Delete(path)
	if err != nil && err != nats.ErrKeyNotFound {
		return fmt.Errorf("failed to delete history: %w", err)
	}
	return nil
}
