package persistence

import (
	"fmt"
	"log"
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
	walkNode(root, "", config)
	return config, nil
}

func walkNode(node *tree.Node, parentPath string, config *TreeConfig) {
	children := node.GetChildren()

	path := parentPath
	if node.GetName() != "root" {
		if parentPath == "" {
			path = sep + node.GetName()
		} else {
			path = parentPath + sep + node.GetName()
		}
	}

	nc := NodeConfig{
		Path:         path,
		Description:  node.GetDescription(),
		TemplateName: node.GetTemplateName(),
		Type:         string(node.GetNodeType()),
		Locked:       node.IsLocked(),
		IsArray:      node.GetIsArray(),
	}

	// Gather leaves first
	for _, child := range children {
		if leaf, ok := child.(tree.Leaf); ok {
			lc := LeafConfig{
				Name:         leaf.GetName(),
				Type:         leaf.ValueType().String(),
				Description:  leaf.GetDescription(),
				Units:        leaf.GetShared().Units,
				Deadband:     leaf.GetShared().Deadband,
				TemplateName: leaf.GetConfig().TemplateName,
			}
			if leaf.ValueType() == tree.TypeEnum {
				lc.EnumValues = leaf.GetShared().EnumValues
			}
			// Serialize the local pipeline only (not the effective/inherited pipeline).
			// Template-linked leaves have an empty local pipeline; the template leaf's
			// pipeline is serialized on the template node itself and re-linked on restore.
			if pipeline := leaf.GetShared().Pipeline; len(pipeline) > 0 {
				envelopes, err := tree.MarshalPipeline(pipeline)
				if err == nil {
					lc.Pipeline = envelopes
				}
			}
			nc.Children = append(nc.Children, lc)
		}
	}

	// Add parent node before recursing (pre-order) ensures parents are unlocked before children process
	if path != "" {
		config.Nodes = append(config.Nodes, nc)
	}

	// Recurse for child nodes
	for _, child := range children {
		if child.IsNode() {
			childNode := child.(*tree.Node)
			walkNode(childNode, path, config)
		}
	}
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
				Name: lc.Name,
				Type: scalarType,
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

			// Restore pipeline if present
			if len(lc.Pipeline) > 0 {
				leaf, err := treeOps.FindLeaf(leafPath)
				if err == nil {
					pipeline, err := tree.UnmarshalPipeline(lc.Pipeline)
					if err == nil {
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

	// Second pass: re-establish template pointers for template-linked leaves.
	// All nodes and leaves are in the tree by now, so template lookup is safe.
	// Device structure is fixed: org/deviceType/device/tagGroup.../tag
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
			// Compute template leaf path.
			// nc.Path = "/org/deviceType/device/tagGroupPath" (pre-order, starts with sep)
			// Strip leading sep and split to extract org and the device-relative suffix.
			ncPath := strings.TrimPrefix(nc.Path, sep)
			parts := strings.Split(ncPath, sep)
			if len(parts) < 3 {
				continue // not deep enough to be a device-level leaf
			}
			org := parts[0]
			deviceRelParts := append(parts[3:], lc.Name)
			deviceRelSuffix := strings.Join(deviceRelParts, sep)
			templateBase := strings.ReplaceAll(lc.TemplateName, ".", sep)
			tmplLeafPath := org + sep + templateBase + sep + deviceRelSuffix

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
