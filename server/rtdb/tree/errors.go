package tree

import "errors"

// Common errors for tree operations
var (
	ErrNotFound         = errors.New("not found")
	ErrLeafAtPath       = errors.New("leaf found at path")
	ErrCannotDeleteRoot = errors.New("cannot delete root")
	ErrIsLeaf           = errors.New("path is a leaf, not a node")
	ErrIsNode           = errors.New("path is a node, not a leaf")
	ErrWrongType        = errors.New("wrong value type for leaf")
	ErrNodeDeleted      = errors.New("node has been deleted")
)
