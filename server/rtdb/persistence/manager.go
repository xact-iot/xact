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

	mu    sync.Mutex
	dirty bool
	timer *time.Timer
	done  chan struct{}
}

// NewManager creates a new persistence manager
func NewManager(database sqldb.DB, treeOps *tree.TreeWithOperations, org string, debounce time.Duration) *Manager {
	return &Manager{
		db:       database,
		tree:     treeOps,
		org:      org,
		debounce: debounce,
		done:     make(chan struct{}),
	}
}

// MarkDirty signals that the tree has changed and needs saving.
// Resets the debounce timer on each call.
func (m *Manager) MarkDirty() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.dirty = true

	if m.timer != nil {
		m.timer.Stop()
	}
	m.timer = time.AfterFunc(m.debounce, func() {
		if err := m.Save(context.Background()); err != nil {
			log.Printf("persistence: auto-save failed: %v", err)
		}
	})
}

// Save immediately serializes the tree and writes to the database
func (m *Manager) Save(ctx context.Context) error {
	m.mu.Lock()
	if !m.dirty {
		m.mu.Unlock()
		return nil
	}
	m.dirty = false
	if m.timer != nil {
		m.timer.Stop()
		m.timer = nil
	}
	m.mu.Unlock()

	config, err := SerializeTree(m.tree.Root)
	if err != nil {
		return fmt.Errorf("serialize tree: %w", err)
	}

	data, err := json.MarshalIndent(config, "", "   ")
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
	if m.timer != nil {
		m.timer.Stop()
		m.timer = nil
	}
	// Force dirty so final save happens
	wasDirty := m.dirty
	m.mu.Unlock()

	if wasDirty {
		return m.Save(ctx)
	}
	return nil
}
