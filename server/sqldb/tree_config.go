package sqldb

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// TreeConfigStore persists node configuration independently of live tag values.
// The boolean returned by LoadTreeNodes distinguishes an initialized empty tree
// from a database that still needs migration from its legacy configuration blob.
type TreeConfigStore interface {
	LoadTreeNodes(context.Context, string) ([]TreeConfigNode, bool, error)
	SaveTreeNodes(context.Context, string, TreeConfigBatch) error
}

// TreeConfigNode contains only this node's metadata and immediate leaf configs;
// descendants have their own rows. Paths use canonical dot notation without a
// leading dot. Org identifies the snapshot owner, as with SaveConfig.
type TreeConfigNode struct {
	Path         string          `json:"path"`
	ParentPath   string          `json:"parent_path"`
	Type         string          `json:"node_type"`
	Description  string          `json:"description"`
	TemplateName string          `json:"template_name"`
	Locked       bool            `json:"locked"`
	IsArray      bool            `json:"is_array"`
	Tags         json.RawMessage `json:"tags"`
}

// TreeConfigBatch is atomic. Deletes run before upserts so deleting and then
// recreating a path removes obsolete descendants while retaining the new nodes.
// Replace is reserved for initial migration and explicit full saves.
type TreeConfigBatch struct {
	Replace     bool
	DeletePaths []string
	Upserts     []TreeConfigNode
}

func ValidateTreeConfigBatch(batch TreeConfigBatch) error {
	validPath := func(path string) bool {
		return path != "" && !strings.Contains(path, "/") &&
			!strings.HasPrefix(path, ".") && !strings.HasSuffix(path, ".") &&
			!strings.Contains(path, "..")
	}
	for _, path := range batch.DeletePaths {
		if !validPath(path) {
			return fmt.Errorf("invalid tree deletion path %q", path)
		}
	}
	for _, node := range batch.Upserts {
		parent := ""
		if dot := strings.LastIndexByte(node.Path, '.'); dot >= 0 {
			parent = node.Path[:dot]
		}
		if !validPath(node.Path) || node.ParentPath != parent {
			return fmt.Errorf("invalid tree node path/parent %q", node.Path)
		}
		if len(node.Tags) == 0 || !json.Valid(node.Tags) || node.Tags[0] != '[' {
			return fmt.Errorf("invalid tag configuration array for %q", node.Path)
		}
	}
	return nil
}
