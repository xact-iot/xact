package tree

import "sync"

// TagConfig holds persisted, per-tag configuration
type TagConfig struct {
	Type         ScalarType `json:"type"`
	Name         string     `json:"name"`
	TemplateName string     `json:"templateName,omitempty"`
}

// TagShared holds data that may be inherited from a template
type TagShared struct {
	Description string         `json:"description,omitempty"`
	Units       string         `json:"units,omitempty"`
	Deadband    float64        `json:"deadband,omitempty"`
	EnumValues  map[int]string `json:"enumValues,omitempty"`
	Pipeline    []ProcessBlock `json:"-"` // Custom JSON via PipelineEnvelopes
}

// PipelineEnvelopes returns the pipeline as serializable envelopes
func (s TagShared) PipelineEnvelopes() ([]ProcessBlockEnvelope, error) {
	if len(s.Pipeline) == 0 {
		return nil, nil
	}
	return MarshalPipeline(s.Pipeline)
}

// NodeType defines the type of a container node
type NodeType string

const (
	NodeTypeStandard     NodeType = "Standard"
	NodeTypeDevice       NodeType = "Device"
	NodeTypeOrganisation NodeType = "Organisation"
)

// Node represents an internal node in the tree
type Node struct {
	Name         string `json:"name"`
	description  string
	templateName string
	nodeType     NodeType
	locked       bool
	isArray      bool
	Children     map[string]TreeNode `json:"-"`
	IsLeaf       bool                `json:"-"`
	deleted      bool
	parent       *Node
	mu           sync.RWMutex
}

// parentSetter is implemented by both Node and baseLeaf (tree package only).
type parentSetter interface {
	setParent(*Node)
}

func (n *Node) setParent(p *Node) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.parent = p
}

// GetParent returns the node's parent, or nil for the root.
func (n *Node) GetParent() *Node {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.parent
}

// TreeNode interface for both Node and Leaf
type TreeNode interface {
	GetName() string
	IsNode() bool
}

// GetName returns the node name
func (n *Node) GetName() string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.Name
}

// IsNode returns true for Node
func (n *Node) IsNode() bool {
	return true
}

// IsDeleted returns true if the node has been deleted
func (n *Node) IsDeleted() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.deleted
}

// GetDescription returns the node description.
func (n *Node) GetDescription() string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.description
}

// SetDescription sets the node description.
func (n *Node) SetDescription(d string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.description = d
}

// GetTemplateName returns the node template name.
func (n *Node) GetTemplateName() string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.templateName
}

// SetTemplateName sets the node template name.
func (n *Node) SetTemplateName(t string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.templateName = t
}

// GetNodeType returns the node type.
func (n *Node) GetNodeType() NodeType {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.nodeType
}

// SetNodeType sets the node type.
func (n *Node) SetNodeType(t NodeType) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.nodeType = t
}

// IsLocked returns whether the node is locked.
func (n *Node) IsLocked() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.locked
}

// SetLocked sets the node locked state.
func (n *Node) SetLocked(v bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.locked = v
}

// GetIsArray returns whether this node is an array container.
func (n *Node) GetIsArray() bool {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.isArray
}

// SetIsArray marks this node as an array container.
func (n *Node) SetIsArray(v bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.isArray = v
}

// NewNode creates a new internal node
func NewNode(name string) *Node {
	return &Node{
		Name:     name,
		nodeType: NodeTypeStandard,
		Children: make(map[string]TreeNode),
		IsLeaf:   false,
		deleted:  false,
	}
}

// AddChild adds a child node or leaf
func (n *Node) AddChild(child TreeNode) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.deleted {
		return ErrNodeDeleted
	}
	n.Children[child.GetName()] = child
	if ps, ok := child.(parentSetter); ok {
		ps.setParent(n)
	}
	return nil
}

// RemoveChild removes a child by name
func (n *Node) RemoveChild(name string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.deleted {
		return ErrNodeDeleted
	}
	delete(n.Children, name)
	return nil
}

// GetChild retrieves a child by name
func (n *Node) GetChild(name string) (TreeNode, bool) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	if n.deleted {
		return nil, false
	}
	child, ok := n.Children[name]
	return child, ok
}

// MarkDeleted marks the node as deleted
func (n *Node) MarkDeleted() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.deleted = true
}

// GetChildren returns a copy of the children map
func (n *Node) GetChildren() map[string]TreeNode {
	n.mu.RLock()
	defer n.mu.RUnlock()
	if n.deleted {
		return nil
	}
	children := make(map[string]TreeNode, len(n.Children))
	for k, v := range n.Children {
		children[k] = v
	}
	return children
}
