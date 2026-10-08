package persistence

import (
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/xact-iot/xact/rtdb/tree"
)

var sep = "."

// TreeConfig is the top-level JSON representation of a tree
type TreeConfig struct {
	Nodes []NodeConfig `json:"nodes"`
}

// NodeConfig represents a node in the serialized tree
type NodeConfig struct {
	Path         string       `json:"path"`
	Description  string       `json:"description,omitempty"`
	TemplateName string       `json:"templateName,omitempty"`
	Type         string       `json:"type,omitempty"`
	Locked       bool         `json:"locked,omitempty"`
	IsArray      bool         `json:"isArray,omitempty"`
	Children     []LeafConfig `json:"children,omitempty"`
	SubNodes     []NodeConfig `json:"subNodes,omitempty"`
}

// LeafConfig represents a leaf in the serialized tree
type LeafConfig struct {
	Name         string                      `json:"name"`
	Type         string                      `json:"type"`
	Description  string                      `json:"description,omitempty"`
	Units        string                      `json:"units,omitempty"`
	Deadband     float64                     `json:"deadband,omitempty"`
	TemplateName string                      `json:"templateName,omitempty"`
	EnumValues   map[int]string              `json:"enumValues,omitempty"`
	Pipeline     []tree.ProcessBlockEnvelope `json:"pipeline,omitempty"`
}

// SerializeTree walks the tree and produces a TreeConfig
func SerializeTree(root *tree.Node) (*TreeConfig, error) {
	config := &TreeConfig{}
	if err := walkNode(root, "", config); err != nil {
		return nil, err
	}
	return config, nil
}

func walkNode(node *tree.Node, parentPath string, config *TreeConfig) error {
	children := node.GetChildren()

	path := parentPath
	if node.GetName() != "root" {
		if parentPath == "" {
			path = sep + node.GetName()
		} else {
			path = parentPath + sep + node.GetName()
		}
	}

	nc, err := serializeNode(node, path, children)
	if err != nil {
		return err
	}

	// Add parent node before recursing (pre-order) ensures parents are unlocked before children process
	if path != "" {
		config.Nodes = append(config.Nodes, nc)
	}

	// Recurse for child nodes
	for _, child := range children {
		if child.IsNode() {
			childNode := child.(*tree.Node)
			if err := walkNode(childNode, path, config); err != nil {
				return err
			}
		}
	}
	return nil
}

// SerializeNode captures metadata and immediate leaves without walking descendants.
func SerializeNode(node *tree.Node, path string) (NodeConfig, error) {
	return serializeNode(node, path, node.GetChildren())
}

func serializeNode(node *tree.Node, path string, children map[string]tree.TreeNode) (NodeConfig, error) {
	nc := NodeConfig{
		Path:         path,
		Description:  node.GetDescription(),
		TemplateName: node.GetTemplateName(),
		Type:         string(node.GetNodeType()),
		Locked:       node.IsLocked(),
		IsArray:      node.GetIsArray(),
	}

	// Gather leaves first
	names := make([]string, 0, len(children))
	for name, child := range children {
		if _, ok := child.(tree.Leaf); ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		child := children[name]
		if leaf, ok := child.(tree.Leaf); ok {
			shared := leaf.GetShared()
			lc := LeafConfig{
				Name:         leaf.GetName(),
				Type:         leaf.ValueType().String(),
				Description:  leaf.GetDescription(),
				Units:        shared.Units,
				Deadband:     shared.Deadband,
				TemplateName: leaf.GetConfig().TemplateName,
			}
			if leaf.ValueType() == tree.TypeEnum {
				lc.EnumValues = shared.EnumValues
			}
			// Serialize the local pipeline only (not the effective/inherited pipeline).
			// Template-linked leaves have an empty local pipeline; the template leaf's
			// pipeline is serialized on the template node itself and re-linked on restore.
			if pipeline := shared.Pipeline; len(pipeline) > 0 {
				envelopes, err := tree.MarshalPipeline(pipeline)
				if err != nil {
					return NodeConfig{}, fmt.Errorf("serialize pipeline %s.%s: %w", path, name, err)
				}
				lc.Pipeline = envelopes
			}
			nc.Children = append(nc.Children, lc)
		}
	}

	return nc, nil
}

// DeserializeTree rebuilds a tree from a TreeConfig
func DeserializeTree(config *TreeConfig, treeOps *tree.TreeWithOperations) error {
	for _, nc := range config.Nodes {
		// Create the node based on Type
		switch nc.Type {
		case string(tree.NodeTypeDevice):
			if err := treeOps.CreateDeviceNode(nc.Path, nc.TemplateName); err != nil {
				return fmt.Errorf("creating device node %s: %w", nc.Path, err)
			}
		case string(tree.NodeTypeOrganisation):
			if err := treeOps.CreateOrganisationNode(nc.Path, nc.TemplateName); err != nil {
				return fmt.Errorf("creating organisation node %s: %w", nc.Path, err)
			}
		default:
			if err := treeOps.CreateNode(nc.Path, nc.TemplateName); err != nil {
				return fmt.Errorf("creating node %s: %w", nc.Path, err)
			}
		}

		// Always unlock the node so we can create leaf children beneath it without lock errors
		if err := treeOps.UnlockNode(nc.Path); err != nil {
			return fmt.Errorf("unlocking node %s: %w", nc.Path, err)
		}

		// Set description and isArray flag on the created node
		if nc.Description != "" || nc.IsArray {
			node, err := treeOps.FindNode(nc.Path)
			if err == nil {
				if nc.Description != "" {
					node.SetDescription(nc.Description)
				}
				if nc.IsArray {
					node.SetIsArray(true)
				}
			}
		}

		// Create leaves
		for _, lc := range nc.Children {
			leafPath := nc.Path + sep + lc.Name
			scalarType := parseScalarType(lc.Type)

			config := tree.TagConfig{
				Name:         lc.Name,
				Type:         scalarType,
				TemplateName: lc.TemplateName,
			}
			shared := tree.TagShared{
				Description: lc.Description,
				Units:       lc.Units,
				Deadband:    lc.Deadband,
				EnumValues:  lc.EnumValues,
			}

			if err := treeOps.CreateTag(leafPath, scalarType, config, shared); err != nil {
				return fmt.Errorf("creating tag %s: %w", leafPath, err)
			}
			// Device/organisation creation has already provisioned mandatory
			// meta leaves. CreateTag is idempotent, so explicitly restore their
			// saved metadata too instead of keeping constructor defaults.
			leaf, err := treeOps.FindLeaf(leafPath)
			if err != nil {
				return err
			}
			shared.Pipeline = leaf.GetShared().Pipeline
			leaf.SetShared(shared)

			// Restore pipeline if present
			if len(lc.Pipeline) > 0 {
				leaf, err := treeOps.FindLeaf(leafPath)
				if err == nil {
					pipeline, err := tree.UnmarshalPipeline(lc.Pipeline)
					if err == nil {
						tree.ClosePipelineBlocks(leaf, leaf.GetShared().Pipeline)
						shared := leaf.GetShared()
						shared.Pipeline = pipeline
						leaf.SetShared(shared)
						tree.InitPipelineBlocks(leaf, pipeline)
					}
				}
			}
		}
	}

	// Reapply locks sequentially across the tree now that children and tags exist
	for _, nc := range config.Nodes {
		if nc.Locked {
			treeOps.LockNode(nc.Path)
		}
	}

	deviceTemplates := map[string]string{}
	for _, nc := range config.Nodes {
		if nc.Type == string(tree.NodeTypeDevice) && nc.TemplateName != "" {
			deviceTemplates[normalizeConfigPath(nc.Path)] = normalizeTemplateName(nc.TemplateName)
		}
	}
	// Repair legacy instances after every node and template has been restored.
	// Older snapshots retained templateName only on the device node, leaving the
	// descendant leaves with a local default publish block and no template link.
	for devicePath, templateName := range deviceTemplates {
		treeOps.LinkDeviceTemplate(devicePath, templateName)
	}

	// Second pass: re-establish template pointers for template-linked leaves.
	// All nodes and leaves are in the tree by now, so template lookup is safe.
	// TemplateName uses dot notation relative to org (e.g. "Templates.VMS").
	for _, nc := range config.Nodes {
		for _, lc := range nc.Children {
			if lc.TemplateName == "" {
				continue
			}
			leafPath := nc.Path + sep + lc.Name
			leaf, err := treeOps.FindLeaf(leafPath)
			if err != nil {
				continue
			}
			tmplLeafPath, ok := templateLeafPathForLinkedLeaf(leafPath, lc.TemplateName, deviceTemplates)
			if !ok {
				log.Printf("persistence: template device ancestor not found for %s using template %s", leafPath, lc.TemplateName)
				continue
			}

			tmplLeaf, err := treeOps.FindLeaf(tmplLeafPath)
			if err != nil {
				log.Printf("persistence: template leaf not found for %s → %s: %v", leafPath, tmplLeafPath, err)
				continue
			}
			// Close the default publish block added by CreateTag, then link to template.
			tree.ClosePipelineBlocks(leaf, leaf.GetShared().Pipeline)
			shared := leaf.GetShared()
			shared.Pipeline = nil
			leaf.SetShared(shared)
			leaf.SetTemplate(tmplLeaf)
		}
	}

	return nil
}

func normalizeConfigPath(path string) string {
	return strings.Trim(strings.ReplaceAll(path, "/", sep), sep)
}

func normalizeTemplateName(name string) string {
	return strings.Trim(strings.ReplaceAll(name, "/", sep), sep)
}

func templateLeafPathForLinkedLeaf(leafPath, templateName string, deviceTemplates map[string]string) (string, bool) {
	leafPath = normalizeConfigPath(leafPath)
	templateName = normalizeTemplateName(templateName)
	if leafPath == "" || templateName == "" {
		return "", false
	}

	var devicePath string
	for path, tmpl := range deviceTemplates {
		if tmpl != templateName {
			continue
		}
		if leafPath == path || strings.HasPrefix(leafPath, path+sep) {
			if len(path) > len(devicePath) {
				devicePath = path
			}
		}
	}
	if devicePath == "" {
		return "", false
	}

	parts := strings.Split(devicePath, sep)
	if len(parts) == 0 {
		return "", false
	}
	suffix := strings.TrimPrefix(leafPath, devicePath+sep)
	if suffix == "" {
		return "", false
	}
	return parts[0] + sep + templateName + sep + suffix, true
}

func parseScalarType(s string) tree.ScalarType {
	switch s {
	case "integer":
		return tree.TypeInteger
	case "float":
		return tree.TypeFloat
	case "string":
		return tree.TypeString
	case "boolean":
		return tree.TypeBoolean
	case "enum":
		return tree.TypeEnum
	default:
		return tree.TypeString
	}
}
