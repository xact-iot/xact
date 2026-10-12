package persistence

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/xact-iot/xact/rtdb/tree"
	"github.com/xact-iot/xact/sqldb"
)

// MarkStructureDirty must not traverse the tree: structural callbacks can run
// while its write lock is held. Leaves belong to their containing node's row.
func (m *Manager) MarkStructureDirty(path string, node tree.TreeNode) {
	path = normalizeConfigPath(path)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped {
		return
	}
	if node == nil {
		// Keep deletion even if the path is recreated before the save. Deletes
		// precede upserts so old descendants cannot survive that recreation.
		if path != "" && !hasAncestor(m.deletePaths, path) {
			m.deletePaths[path] = struct{}{}
		}
		path = parentPath(path)
	} else if !node.IsNode() {
		path = parentPath(path)
	}
	if path != "" {
		m.dirtyPaths[path] = struct{}{}
	}
	m.markDirtyLocked()
}

func parentPath(path string) string {
	if dot := strings.LastIndexByte(path, '.'); dot >= 0 {
		return path[:dot]
	}
	return ""
}

func hasAncestor(paths map[string]struct{}, path string) bool {
	for path = parentPath(path); path != ""; path = parentPath(path) {
		if _, found := paths[path]; found {
			return true
		}
	}
	return false
}

func sortedPaths(paths map[string]struct{}) []string {
	out := make([]string, 0, len(paths))
	for path := range paths {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

// nodeBatch takes one consistent structural view, without retaining the tree
// lock during database I/O. A later mutation stays in the next pending batch.
func (m *Manager) nodeBatch(paths, deletes map[string]struct{}, replace bool) (sqldb.TreeConfigBatch, error) {
	batch := sqldb.TreeConfigBatch{Replace: replace}
	if !replace {
		for _, path := range sortedPaths(deletes) {
			if !hasAncestor(deletes, path) {
				batch.DeletePaths = append(batch.DeletePaths, path)
			}
		}
	}
	m.tree.RLock()
	defer m.tree.RUnlock()
	appendNode := func(node *tree.Node, path string) error {
		config, err := SerializeNode(node, path)
		if err != nil {
			return err
		}
		tags := config.Children
		if tags == nil {
			tags = []LeafConfig{}
		}
		data, err := json.Marshal(tags)
		if err != nil {
			return fmt.Errorf("marshal node %q: %w", path, err)
		}
		batch.Upserts = append(batch.Upserts, sqldb.TreeConfigNode{
			Path: path, ParentPath: parentPath(path), Type: config.Type,
			Description: config.Description, TemplateName: config.TemplateName,
			Locked: config.Locked, IsArray: config.IsArray, Tags: data,
		})
		return nil
	}
	if replace {
		var walk func(*tree.Node, string) error
		walk = func(node *tree.Node, path string) error {
			if path != "" {
				if err := appendNode(node, path); err != nil {
					return err
				}
			}
			for name, child := range node.GetChildren() {
				if n, ok := child.(*tree.Node); ok {
					childPath := name
					if path != "" {
						childPath = path + "." + name
					}
					if err := walk(n, childPath); err != nil {
						return err
					}
				}
			}
			return nil
		}
		err := walk(m.tree.Root, "")
		return batch, err
	}
	for _, path := range sortedPaths(paths) {
		// Traverse under the already-held tree lock instead of recursively
		// taking RLock with FindNode (which can deadlock behind a writer).
		node := m.tree.Root
		for _, name := range strings.Split(path, ".") {
			child, _ := node.GetChild(name)
			node, _ = child.(*tree.Node)
			if node == nil {
				break
			}
		}
		if node != nil && !node.IsDeleted() {
			if err := appendNode(node, path); err != nil {
				return batch, err
			}
		}
	}
	return batch, nil
}

func configFromRows(nodes []sqldb.TreeConfigNode) (*TreeConfig, error) {
	if err := sqldb.ValidateTreeConfigBatch(sqldb.TreeConfigBatch{Upserts: nodes}); err != nil {
		return nil, err
	}
	config := &TreeConfig{Nodes: make([]NodeConfig, 0, len(nodes))}
	for _, n := range nodes {
		nc := NodeConfig{Path: n.Path, Type: n.Type, Description: n.Description,
			TemplateName: n.TemplateName, Locked: n.Locked, IsArray: n.IsArray}
		if err := json.Unmarshal(n.Tags, &nc.Children); err != nil {
			return nil, fmt.Errorf("decode node %q: %w", n.Path, err)
		}
		config.Nodes = append(config.Nodes, nc)
	}
	// Restore parents first, independent of database or migration row order.
	sort.Slice(config.Nodes, func(i, j int) bool {
		a, b := config.Nodes[i].Path, config.Nodes[j].Path
		if strings.Count(a, ".") != strings.Count(b, ".") {
			return strings.Count(a, ".") < strings.Count(b, ".")
		}
		return a < b
	})
	return config, nil
}
