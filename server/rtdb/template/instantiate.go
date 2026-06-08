package template

import (
	"github.com/xact-iot/xact/rtdb/tree"
)

// Instantiate creates an instance from a template using reference-based inheritance.
// Instantiated leaves store the template name and a pointer to the template leaf.
// Description and pipeline fall through to the template when local values are empty.
func Instantiate(tmpl *tree.Node, targetName string) *tree.Node {
	return instantiateNode(tmpl, targetName, tmpl.GetTemplateName())
}

// InstantiateWithSubTemplate creates instance from template + sub-template
func InstantiateWithSubTemplate(mainTemplate, subTemplate *tree.Node, targetName string) *tree.Node {
	instance := instantiateNode(mainTemplate, targetName, mainTemplate.GetTemplateName())

	// Merge sub-template children
	if subTemplate != nil {
		for _, child := range subTemplate.Children {
			if _, exists := instance.GetChild(child.GetName()); !exists {
				copied := instantiateTreeNode(child, subTemplate.GetTemplateName())
				instance.AddChild(copied)
			}
		}
	}

	return instance
}

// instantiateNode creates a ref-based instance of a node
func instantiateNode(node *tree.Node, newName string, templateName string) *tree.Node {
	cp := tree.NewNode(newName)
	cp.SetDescription(node.GetDescription())
	if templateName != "" {
		cp.SetTemplateName(templateName)
	} else {
		cp.SetTemplateName(node.GetTemplateName())
	}

	for _, child := range node.Children {
		copiedChild := instantiateTreeNode(child, templateName)
		cp.AddChild(copiedChild)
	}

	return cp
}

// instantiateTreeNode creates a ref-based copy of any TreeNode.
// For leaves, it creates a new leaf with a template reference pointing to the source.
func instantiateTreeNode(node tree.TreeNode, templateName string) tree.TreeNode {
	switch n := node.(type) {
	case *tree.Node:
		return instantiateNode(n, n.GetName(), templateName)
	default:
		if srcLeaf, ok := node.(tree.Leaf); ok {
			return instantiateLeaf(srcLeaf, templateName)
		}
		return nil
	}
}

// instantiateLeaf creates a new leaf that references the template leaf
func instantiateLeaf(src tree.Leaf, templateName string) tree.Leaf {
	// Create config with template reference
	cfg := src.GetConfig()
	cfg.TemplateName = templateName

	// Create the leaf with empty shared data - it will fall through to template
	newLeaf := tree.NewLeaf(src.ValueType(), src.GetName(), cfg)
	if newLeaf == nil {
		return nil
	}

	// Set the template reference so accessors fall through
	newLeaf.SetTemplate(src)

	return newLeaf
}

// CopyTemplateSubtree copies an entire template subtree to a target node (deep copy)
func CopyTemplateSubtree(tmpl *tree.Node, target *tree.Node) {
	for _, child := range tmpl.Children {
		copied := instantiateTreeNode(child, tmpl.GetTemplateName())
		target.AddChild(copied)
	}
}
