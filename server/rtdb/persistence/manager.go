package persistence

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/xact-iot/xact/rtdb/tree"
	"github.com/xact-iot/xact/sqldb"
)

const configName = "rtdb_tree"

// Manager handles debounced persistence of tree config to the database
type Manager struct {
	db       sqldb.DB
	tree     *tree.TreeWithOperations
	org      string
	debounce time.Duration

	mu              sync.Mutex
	saveMu          sync.Mutex // Only one full-tree snapshot may be allocated at a time.
	dirty           bool
	timer           *time.Timer
	dirtySince      time.Time
	lastSaveAttempt time.Time
	stopped         bool
}

// NewManager creates a new persistence manager
func NewManager(database sqldb.DB, treeOps *tree.TreeWithOperations, org string, debounce time.Duration) *Manager {
	return &Manager{
		db:       database,
		tree:     treeOps,
		org:      org,
		debounce: debounce,
	}
}

// MarkDirty coalesces bursts, while bounding both save frequency and the time
// an ongoing stream of structural changes may remain without a checkpoint.
func (m *Manager) MarkDirty() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped {
		return
	}
	if !m.dirty {
		m.dirtySince = time.Now()
	}
	m.dirty = true
	m.scheduleLocked()
}

func (m *Manager) scheduleLocked() {
	if m.stopped {
		return
	}
	now := time.Now()
	due := now.Add(m.debounce)
	if minimum := m.lastSaveAttempt.Add(6 * m.debounce); minimum.After(due) {
		due = minimum
	}
	if maximum := m.dirtySince.Add(12 * m.debounce); maximum.Before(due) {
		due = maximum
	}
	if m.timer != nil {
		m.timer.Reset(max(time.Duration(0), due.Sub(now)))
		return
	}
	m.timer = time.AfterFunc(max(time.Duration(0), due.Sub(now)), func() {
		if err := m.Save(context.Background()); err != nil {
			log.Printf("persistence: auto-save failed: %v", err)
		}
	})
}

// Save immediately serializes the tree and writes to the database
func (m *Manager) Save(ctx context.Context) (saveErr error) {
	m.saveMu.Lock()
	defer m.saveMu.Unlock()
	m.mu.Lock()
	if !m.dirty {
		m.mu.Unlock()
		return nil
	}
	m.dirty = false
	m.dirtySince = time.Time{}
	m.lastSaveAttempt = time.Now()
	if m.timer != nil {
		m.timer.Stop()
		m.timer = nil
	}
	m.mu.Unlock()
	defer func() {
		if saveErr == nil {
			return
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		if !m.dirty {
			m.dirtySince = time.Now()
		}
		m.dirty = true
		m.scheduleLocked()
	}()

	config, err := SerializeTree(m.tree.Root)
	if err != nil {
		return fmt.Errorf("serialize tree: %w", err)
	}

	// Database snapshots do not need formatting. Indenting this large document
	// allocates several additional copies of the entire tree.
	data, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := m.db.SaveConfig(ctx, m.org, configName, data); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	return nil
}

// Restore loads tree config from the database and rebuilds the tree.
// Returns false if no saved config was found.
func (m *Manager) Restore(ctx context.Context) (bool, error) {
	data, err := m.db.LoadConfig(ctx, m.org, configName)
	if err != nil {
		return false, fmt.Errorf("load config: %w", err)
	}
	if data == nil {
		return false, nil
	}

	var config TreeConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return false, fmt.Errorf("unmarshal config: %w", err)
	}

	if err := DeserializeTree(&config, m.tree); err != nil {
		return false, fmt.Errorf("deserialize tree: %w", err)
	}

	log.Printf("persistence: tree config restored (%d nodes)", len(config.Nodes))
	return true, nil
}

// Stop cancels any pending timer and performs a final save
func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	m.stopped = true
	if m.timer != nil {
		m.timer.Stop()
		m.timer = nil
	}
	m.mu.Unlock()
	// Save also waits for an already-running checkpoint and retries its dirty
	// state if it failed; shutdown must not race the current database write.
	return m.Save(ctx)
}
