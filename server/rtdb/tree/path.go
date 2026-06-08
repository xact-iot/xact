package tree

import (
	"fmt"
	"strings"
	"sync"
)

// Tree represents the root of the hierarchical data structure
type Tree struct {
	Root *Node
	mu   sync.RWMutex // protects structural changes and traversals
}

// NewTree creates a new empty tree
func NewTree() *Tree {
	tree := &Tree{
		Root: NewNode("root"),
	}
	return tree
}

// ResolvePath splits a path into components
func ResolvePath(path string) []string {
	path = strings.ReplaceAll(path, "/", ".")
	// Remove leading dot if present
	path = strings.TrimPrefix(path, ".")
	if path == "" {
		return []string{}
	}
	return strings.Split(path, ".")
}

// FindNode traverses the tree to find a node at the given path
// Returns nil if not found or if path leads to a leaf
func (t *Tree) FindNode(path string) (*Node, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	components := ResolvePath(path)
	if len(components) == 0 {
		if t.Root.IsDeleted() {
			return nil, ErrNodeDeleted
		}
		return t.Root, nil
	}

	current := t.Root
	for _, component := range components {
		if current.IsDeleted() {
			return nil, ErrNodeDeleted
		}
		child, ok := current.GetChild(component)
		if !ok {
			return nil, fmt.Errorf("path not found: %s", path)
		}
		node, ok := child.(*Node)
		if !ok {
			return nil, fmt.Errorf("path %s is a leaf, not a node", path)
		}
		current = node
	}

	if current.IsDeleted() {
		return nil, ErrNodeDeleted
	}

	return current, nil
}

// FindLeaf traverses the tree to find a leaf at the given path
// Returns nil if not found or if path leads to a node
func (t *Tree) FindLeaf(path string) (Leaf, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	components := ResolvePath(path)
	if len(components) == 0 {
		return nil, fmt.Errorf("root is a node, not a leaf")
	}

	// All but last component should be nodes
	current := t.Root
	for i := 0; i < len(components)-1; i++ {
		if current.IsDeleted() {
			return nil, ErrNodeDeleted
		}
		child, ok := current.GetChild(components[i])
		if !ok {
			return nil, fmt.Errorf("path not found: %s", path)
		}
		node, ok := child.(*Node)
		if !ok {
			return nil, fmt.Errorf("path component %s is a leaf", components[i])
		}
		current = node
	}

	if current.IsDeleted() {
		return nil, ErrNodeDeleted
	}

	// Last component should be a leaf
	leafName := components[len(components)-1]
	child, ok := current.GetChild(leafName)
	if !ok {
		return nil, fmt.Errorf("leaf not found: %s", path)
	}

	// Use type assertion to check if it implements Leaf interface
	leaf, ok := child.(Leaf)
	if !ok {
		return nil, fmt.Errorf("path %s is a node, not a leaf", path)
	}

	return leaf, nil
}

// FindNodeOrLeaf finds any TreeNode at the given path
func (t *Tree) FindNodeOrLeaf(path string) (TreeNode, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	components := ResolvePath(path)
	if len(components) == 0 {
		if t.Root.IsDeleted() {
			return nil, ErrNodeDeleted
		}
		return t.Root, nil
	}

	current := t.Root
	for i := 0; i < len(components)-1; i++ {
		if current.IsDeleted() {
			return nil, ErrNodeDeleted
		}
		child, ok := current.GetChild(components[i])
		if !ok {
			return nil, fmt.Errorf("path not found: %s", path)
		}
		node, ok := child.(*Node)
		if !ok {
			return nil, fmt.Errorf("path component %s is a leaf", components[i])
		}
		current = node
	}

	if current.IsDeleted() {
		return nil, ErrNodeDeleted
	}

	lastName := components[len(components)-1]
	child, ok := current.GetChild(lastName)
	if !ok {
		return nil, fmt.Errorf("path not found: %s", path)
	}

	return child, nil
}

// Lock acquires the tree write lock for structural changes
func (t *Tree) Lock() {
	t.mu.Lock()
}

// Unlock releases the tree write lock
func (t *Tree) Unlock() {
	t.mu.Unlock()
}

// RLock acquires the tree read lock for traversals
func (t *Tree) RLock() {
	t.mu.RLock()
}

// RUnlock releases the tree read lock
func (t *Tree) RUnlock() {
	t.mu.RUnlock()
}
