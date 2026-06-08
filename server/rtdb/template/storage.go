package template

import (
	"github.com/xact-iot/xact/rtdb/tree"
)

// TemplatesRootName is the conventional name for the top-level node in the
// main tree that holds template nodes available for reference by other nodes.
const TemplatesRootName = "Templates"

// Storage manages template storage under /_templates/
type Storage struct {
	templates *tree.Node
}

// NewStorage creates a new template storage
func NewStorage() *Storage {
	return &Storage{
		templates: tree.NewNode("_templates"),
	}
}

// GetTemplatesRoot returns the templates root node
func (s *Storage) GetTemplatesRoot() *tree.Node {
	return s.templates
}

// StoreTemplate stores a template tree
func (s *Storage) StoreTemplate(path string, template *tree.Node) error {
	components := tree.ResolvePath(path)

	// Navigate/create to parent
	current := s.templates
	for i := 0; i < len(components)-1; i++ {
		child, ok := current.GetChild(components[i])
		if !ok {
			newNode := tree.NewNode(components[i])
			current.AddChild(newNode)
			current = newNode
		} else {
			node, ok := child.(*tree.Node)
			if !ok {
				return tree.ErrLeafAtPath
			}
			current = node
		}
	}

	// Store the template
	if len(components) > 0 {
		template.Name = components[len(components)-1]
		current.AddChild(template)
	} else {
		// Storing at root - merge
		s.mergeTemplates(s.templates, template)
	}

	return nil
}

// GetTemplate retrieves a template by path
func (s *Storage) GetTemplate(path string) (*tree.Node, error) {
	components := tree.ResolvePath(path)

	current := s.templates
	for _, comp := range components {
		child, ok := current.GetChild(comp)
		if !ok {
			return nil, tree.ErrNotFound
		}
		node, ok := child.(*tree.Node)
		if !ok {
			return nil, tree.ErrLeafAtPath
		}
		current = node
	}

	return current, nil
}

// DeleteTemplate removes a template
func (s *Storage) DeleteTemplate(path string) error {
	components := tree.ResolvePath(path)
	if len(components) == 0 {
		return tree.ErrCannotDeleteRoot
	}

	// Find parent
	var parent *tree.Node
	if len(components) == 1 {
		parent = s.templates
	} else {
		parentPathComponents := components[:len(components)-1]
		parent = s.templates
		for _, comp := range parentPathComponents {
			child, ok := parent.GetChild(comp)
			if !ok {
				return tree.ErrNotFound
			}
			node, ok := child.(*tree.Node)
			if !ok {
				return tree.ErrLeafAtPath
			}
			parent = node
		}
	}

	name := components[len(components)-1]
	if _, ok := parent.GetChild(name); !ok {
		return tree.ErrNotFound
	}

	parent.RemoveChild(name)
	return nil
}

// ListTemplates returns all template paths
func (s *Storage) ListTemplates() []string {
	var paths []string
	s.listTemplatesRecursive(s.templates, "", &paths)
	return paths
}

func (s *Storage) listTemplatesRecursive(node *tree.Node, prefix string, paths *[]string) {
	for name, child := range node.Children {
		path := prefix + "/" + name
		if childNode, ok := child.(*tree.Node); ok {
			*paths = append(*paths, path)
			s.listTemplatesRecursive(childNode, path, paths)
		}
	}
}

// ResolveTemplateRefs walks a tree and resolves TemplateName strings to template leaf pointers.
// Call this after restoring a tree from persistence to reconnect template references.
func (s *Storage) ResolveTemplateRefs(root *tree.Node) error {
	s.resolveRefsRecursive(root)
	return nil
}

func (s *Storage) resolveRefsRecursive(node *tree.Node) {
	children := node.GetChildren()
	for _, child := range children {
		if childNode, ok := child.(*tree.Node); ok {
			s.resolveRefsRecursive(childNode)
		} else if leaf, ok := child.(tree.Leaf); ok {
			tmplName := leaf.GetConfig().TemplateName
			if tmplName == "" {
				continue
			}
			// Find the template node and look for a matching leaf
			tmplNode, err := s.GetTemplate(tmplName)
			if err != nil {
				continue
			}
			tmplChild, ok := tmplNode.GetChild(leaf.GetName())
			if !ok {
				continue
			}
			tmplLeaf, ok := tmplChild.(tree.Leaf)
			if !ok {
				continue
			}
			leaf.SetTemplate(tmplLeaf)
		}
	}
}

// mergeTemplates merges src into dst (for root storage)
func (s *Storage) mergeTemplates(dst, src *tree.Node) {
	for name, child := range src.Children {
		if _, ok := dst.GetChild(name); !ok {
			dst.AddChild(child)
		}
	}
}
