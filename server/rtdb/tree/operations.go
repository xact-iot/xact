package tree

import (
	"encoding/json"
	"fmt"
	"sort"
)

// defaultPublishBlock is the default publish block that marks tags for publishing
type defaultPublishBlock struct{}

func (b *defaultPublishBlock) GetType() string { return "publish" }

func (b *defaultPublishBlock) Init(leaf Leaf)  {}
func (b *defaultPublishBlock) Close(leaf Leaf) {}

func (b *defaultPublishBlock) Process(leaf Leaf, value any) (any, error) {
	return value, nil
}

func (b *defaultPublishBlock) GetParameters() json.RawMessage {
	data, _ := json.Marshal(map[string]interface{}{
		"shouldPublish": true,
	})
	return data
}

func (b *defaultPublishBlock) SetParameters(data json.RawMessage) error {
	return nil
}

func (b *defaultPublishBlock) Schema() BlockSchema {
	return BlockSchema{Type: "publish", Label: "Publish", Description: "Publishes tag value changes via NATS", Params: map[string]ParamSchema{}}
}

// addPublishedLeaf creates a leaf with the default publish pipeline, adds it to parent,
// and initialises the pipeline blocks with tree context.
func addPublishedLeaf(parent *Node, t ScalarType, name string, config TagConfig) (*LeafNode, error) {
	var pb ProcessBlock
	if p, err := NewProcessBlockByType("publish"); err == nil {
		pb = p
	} else {
		pb = &defaultPublishBlock{}
	}
	leaf := NewLeaf(t, name, config, TagShared{Pipeline: []ProcessBlock{pb}})
	if err := parent.AddChild(leaf); err != nil {
		return nil, err
	}
	InitPipelineBlocks(leaf, leaf.GetPipeline())
	return leaf, nil
}

// TreeWithOperations extends Tree with create/delete operations
type TreeWithOperations struct {
	*Tree
	onChange          func(path string, node TreeNode) // Callback for all changes (value + structure)
	onStructureChange func(path string, node TreeNode) // Callback for structural changes only (no value updates)
}

// NewTreeWithOperations creates a new tree with operations
func NewTreeWithOperations(onChange func(path string, node TreeNode)) *TreeWithOperations {
	return &TreeWithOperations{
		Tree:     NewTree(),
		onChange: onChange,
	}
}

// SetOnChange sets the callback for tree changes
func (t *TreeWithOperations) SetOnChange(fn func(path string, node TreeNode)) {
	t.onChange = fn
}

// SetOnStructureChange sets a callback that fires only on structural changes
// (node/tag creation, deletion, config updates) but NOT on leaf value updates.
// Use this for persistence so that periodic script writes don't trigger saves.
func (t *TreeWithOperations) SetOnStructureChange(fn func(path string, node TreeNode)) {
	t.onStructureChange = fn
}

func (t *TreeWithOperations) notifyStructure(path string, node TreeNode) {
	if t.onStructureChange != nil {
		t.onStructureChange(path, node)
	}
}

// notifyBoth fires onChange and onStructureChange. Used for all structural
// mutations (create/delete/update). SetLeafValue uses onChange only.
func (t *TreeWithOperations) notifyBoth(path string, node TreeNode) {
	if t.onChange != nil {
		t.onChange(path, node)
	}
	t.notifyStructure(path, node)
}

// NotifyChange fires the onChange and onStructureChange callbacks for the given path and node.
// Use this when a node's fields are mutated directly (outside of Create/Delete
// operations) so that persistence and NATS subscribers are notified.
func (t *TreeWithOperations) NotifyChange(path string, node TreeNode) {
	t.notifyBoth(path, node)
}

// CreateNode creates a new Standard Node at the given path (create if not exists)
// templateRef: optional reference to a template (empty string for no template)
func (t *TreeWithOperations) CreateNode(path string, templateRef string) error {
	components := ResolvePath(path)
	if len(components) == 0 {
		return fmt.Errorf("cannot create root node")
	}

	t.Lock()
	defer t.Unlock()

	// Idempotent early return
	if _, err := t.findNodeLocked(path); err == nil {
		return nil
	}

	// Navigate to parent node, creating intermediate nodes if needed
	current := t.Root
	for i := 0; i < len(components)-1; i++ {
		if current.IsDeleted() {
			return ErrNodeDeleted
		}
		child, ok := current.GetChild(components[i])
		if !ok {
			// Create intermediate node
			newNode := NewNode(components[i])
			if err := current.AddChild(newNode); err != nil {
				return err
			}
			t.notifyBoth(buildPath(components[:i+1]), newNode)
			current = newNode
		} else {
			node, ok := child.(*Node)
			if !ok {
				return fmt.Errorf("path component %s is a leaf", components[i])
			}
			if node.IsDeleted() {
				return ErrNodeDeleted
			}
			current = node
		}
	}

	// Check if final component already exists
	nodeName := components[len(components)-1]
	if _, ok := current.GetChild(nodeName); ok {
		// Node already exists, return success (idempotent)
		return nil
	}

	// Create the new node
	newNode := NewNode(nodeName)
	if templateRef != "" {
		newNode.templateName = templateRef
	}
	if err := current.AddChild(newNode); err != nil {
		return err
	}

	t.notifyBoth(path, newNode)

	return nil
}

// CreateDeviceNode creates a new Device node at the given path, automatically creating
// 'meta' (with mandatory tags) and 'kpi' subnodes.
func (t *TreeWithOperations) CreateDeviceNode(path string, templateRef string) error {
	if err := t.CreateNode(path, templateRef); err != nil {
		return err
	}

	t.Lock()
	defer t.Unlock()

	node, err := t.findNodeLocked(path)
	if err != nil {
		return err
	}

	node.nodeType = NodeTypeDevice

	// Get or create 'meta' subnode
	var metaNode *Node
	var addedNodes []*Node
	if existing, ok := node.GetChild("meta"); !ok {
		metaNode = NewNode("meta")
		if err := node.AddChild(metaNode); err != nil {
			return err
		}
		addedNodes = append(addedNodes, metaNode)
	} else {
		metaNode = existing.(*Node)
	}

	// Mandatory meta tags for a device node
	type tagDef struct {
		name    string
		tagType ScalarType
	}
	metaTags := []tagDef{
		{"name", TypeString},
		{"deviceFamily", TypeString},
		{"deviceSubtype", TypeString},
		{"description", TypeString},
		{"lat", TypeFloat},
		{"lon", TypeFloat},
		{"timeLastSeen", TypeInteger},
		{"online", TypeBoolean},
		{"commonAlarmPresent", TypeBoolean},
		{"commsParams", TypeString},
	}
	for _, td := range metaTags {
		if _, ok := metaNode.GetChild(td.name); ok {
			continue
		}
		leaf, err := addPublishedLeaf(metaNode, td.tagType, td.name, TagConfig{Type: td.tagType, Name: td.name})
		if err != nil {
			return err
		}
		t.notifyBoth(path+".meta."+td.name, leaf)
	}

	// Populate the name tag with the device's name
	if nameChild, ok := metaNode.GetChild("name"); ok {
		if nameLeaf, ok := nameChild.(Leaf); ok {
			_ = nameLeaf.SetString(node.Name)
		}
	}

	// Get or create 'kpi' subnode
	if _, ok := node.GetChild("kpi"); !ok {
		kpiNode := NewNode("kpi")
		if err := node.AddChild(kpiNode); err != nil {
			return err
		}
		addedNodes = append(addedNodes, kpiNode)
	}

	t.notifyBoth(path, node)
	for _, n := range addedNodes {
		t.notifyBoth(path+"."+n.Name, n)
	}

	return nil
}

// CreateOrganisationNode creates a new Organisation node at the given path,
// automatically creating a 'meta' subnode with 4 bounding-box coordinate tags: north, south, east, west.
func (t *TreeWithOperations) CreateOrganisationNode(path string, templateRef string) error {
	if err := t.CreateNode(path, templateRef); err != nil {
		return err
	}

	t.Lock()
	defer t.Unlock()

	node, err := t.findNodeLocked(path)
	if err != nil {
		return err
	}

	node.nodeType = NodeTypeOrganisation

	// Get or create 'meta' subnode
	var metaNode *Node
	var addedNodes []*Node
	if existing, ok := node.GetChild("meta"); !ok {
		metaNode = NewNode("meta")
		if err := node.AddChild(metaNode); err != nil {
			return err
		}
		addedNodes = append(addedNodes, metaNode)
	} else {
		metaNode = existing.(*Node)
	}

	// Bounding-box coordinate tags (all floats)
	coordTags := []string{"north", "south", "east", "west"}
	for _, name := range coordTags {
		if _, ok := metaNode.GetChild(name); ok {
			continue
		}
		leaf, err := addPublishedLeaf(metaNode, TypeFloat, name, TagConfig{Type: TypeFloat, Name: name})
		if err != nil {
			return err
		}
		t.notifyBoth(path+".meta."+name, leaf)
	}

	t.notifyBoth(path, node)
	for _, n := range addedNodes {
		t.notifyBoth(path+"."+n.Name, n)
	}

	return nil
}

// EnsureDeviceNode ensures a device node exists at devicePath.
// Both the org node (orgPath) and the deviceType container (deviceTypePath)
// must already exist - if either is missing an error is returned and no nodes
// are created.  When the device node is absent it is auto-provisioned as a
// DeviceNode, inheriting the templateRef from the deviceType node if one is set.
// The call is idempotent: it returns nil if the device already exists.
func (t *TreeWithOperations) EnsureDeviceNode(orgPath, deviceTypePath, devicePath string) error {
	// Org must be pre-created manually.
	if _, err := t.FindNode(orgPath); err != nil {
		return fmt.Errorf("org %q not found: %w", orgPath, err)
	}

	// Auto-create device type container if it doesn't exist.
	if err := t.CreateNode(deviceTypePath, ""); err != nil {
		// CreateNode is idempotent - ignore "already exists" style errors.
	}
	deviceTypeNode, err := t.FindNode(deviceTypePath)
	if err != nil {
		return fmt.Errorf("device type %q not found: %w", deviceTypePath, err)
	}

	// Device already exists - nothing to do.
	if _, err := t.FindNode(devicePath); err == nil {
		return nil
	}

	templateRef := deviceTypeNode.GetTemplateName()
	// if templateRef != "" {
	// 	log.Printf("RTDB: auto-provisioning templated device %s (template: %s)", devicePath, templateRef)
	// } else {
	// 	log.Printf("RTDB: auto-provisioning device %s %s %s", orgPath, deviceTypePath, devicePath)
	// }
	return t.CreateDeviceNode(devicePath, templateRef)
}

// DeleteNode deletes a node and all its children (cascade delete)
func (t *TreeWithOperations) DeleteNode(path string) error {
	if path == "" || path == "." {
		return fmt.Errorf("cannot delete root node")
	}

	components := ResolvePath(path)
	if len(components) == 0 {
		return fmt.Errorf("cannot delete root node")
	}

	t.Lock()
	defer t.Unlock()

	// Find parent node
	var parent *Node
	if len(components) == 1 {
		parent = t.Root
	} else {
		parentPath := buildPath(components[:len(components)-1])
		var err error
		parent, err = t.findNodeLocked(parentPath)
		if err != nil {
			return fmt.Errorf("parent not found: %w", err)
		}
	}

	if parent.IsDeleted() {
		return ErrNodeDeleted
	}

	// Get the node to delete
	nodeName := components[len(components)-1]
	child, ok := parent.GetChild(nodeName)
	if !ok {
		return fmt.Errorf("node not found: %s", path)
	}

	node, ok := child.(*Node)
	if !ok {
		return fmt.Errorf("path %s is a leaf, use DeleteTag", path)
	}

	// Close pipeline blocks on all leaf descendants before removal
	closeLeafPipelinesRecursive(node)

	// Cascade delete - mark all children as deleted recursively FIRST
	// (before marking the node itself, because GetChildren returns nil for deleted nodes)
	t.markDeletedRecursive(node)

	// Mark the node as deleted
	node.MarkDeleted()

	// Notify about deletions if callback is set
	if t.onChange != nil {
		t.notifyDeleteRecursive(path, node)
	}

	// Remove from parent
	if err := parent.RemoveChild(nodeName); err != nil {
		return err
	}

	return nil
}

// closeLeafPipelinesRecursive walks the subtree rooted at node and calls
// ClosePipelineBlocks on every leaf found, before the node is deleted.
func closeLeafPipelinesRecursive(node *Node) {
	for _, child := range node.GetChildren() {
		switch c := child.(type) {
		case Leaf:
			ClosePipelineBlocks(c, c.GetPipeline())
		case *Node:
			closeLeafPipelinesRecursive(c)
		}
	}
}

// markDeletedRecursive recursively marks all children as deleted
// This is a depth-first traversal that marks from the bottom up
func (t *TreeWithOperations) markDeletedRecursive(node *Node) {
	// Get children BEFORE marking this node as deleted
	// (because GetChildren returns nil for deleted nodes)
	children := node.GetChildren()

	// First, recursively process all child nodes
	for _, child := range children {
		if childNode, ok := child.(*Node); ok {
			// Recursively mark this child's descendants FIRST
			t.markDeletedRecursive(childNode)
			// Then mark the child itself
			childNode.MarkDeleted()
		}
	}
}

// notifyDeleteRecursive recursively notifies about deletions
func (t *TreeWithOperations) notifyDeleteRecursive(path string, node *Node) {
	children := node.GetChildren()
	for name, child := range children {
		childPath := path + "." + name
		t.notifyBoth(childPath, nil) // nil indicates deletion
		if childNode, ok := child.(*Node); ok {
			t.notifyDeleteRecursive(childPath, childNode)
		}
	}
}

// findNodeLocked finds a node without acquiring the lock (must be called with lock held)
func (t *TreeWithOperations) findNodeLocked(path string) (*Node, error) {
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

// CreateTag creates a new tag/leaf at the given path with the specified type
func (t *TreeWithOperations) CreateTag(path string, scalarType ScalarType, config TagConfig, shared ...TagShared) error {
	components := ResolvePath(path)
	if len(components) == 0 {
		return fmt.Errorf("cannot create tag at root")
	}

	// Validate that config.Type matches scalarType if config.Type is set
	if config.Type != 0 && config.Type != scalarType {
		return fmt.Errorf("config.Type %v does not match scalarType %v", config.Type, scalarType)
	}
	config.Type = scalarType

	t.Lock()
	defer t.Unlock()

	// Navigate to parent node
	current := t.Root
	for i := 0; i < len(components)-1; i++ {
		if current.IsDeleted() {
			return ErrNodeDeleted
		}
		child, ok := current.GetChild(components[i])
		if !ok {
			// Create intermediate node
			newNode := NewNode(components[i])
			if err := current.AddChild(newNode); err != nil {
				return err
			}
			t.notifyBoth(buildPath(components[:i+1]), newNode)
			current = newNode
		} else {
			node, ok := child.(*Node)
			if !ok {
				return fmt.Errorf("path component %s is a leaf", components[i])
			}
			if node.IsDeleted() {
				return ErrNodeDeleted
			}
			current = node
		}
	}

	// Check if tag already exists
	tagName := components[len(components)-1]
	if _, ok := current.GetChild(tagName); ok {
		// Tag already exists, return success (idempotent)
		return nil
	}

	// Prepare shared with default pipeline if not provided
	sharedToUse := TagShared{}
	if len(shared) > 0 {
		sharedToUse = shared[0]
	}
	// Add default publish block to pipeline if none provided.
	// Use the registered PublishBlock so values are broadcast to browsers.
	// Fall back to the no-op placeholder only if the registry is not yet populated.
	if len(sharedToUse.Pipeline) == 0 {
		if pb, err := NewProcessBlockByType("publish"); err == nil {
			sharedToUse.Pipeline = []ProcessBlock{pb}
		} else {
			sharedToUse.Pipeline = []ProcessBlock{&defaultPublishBlock{}}
		}
	}

	// Create the appropriate leaf type based on scalarType
	newTag := NewLeaf(scalarType, tagName, config, sharedToUse)
	if newTag == nil {
		return fmt.Errorf("unknown scalar type: %v", scalarType)
	}

	if err := current.AddChild(newTag); err != nil {
		return err
	}

	// Init pipeline blocks now that the leaf is attached to the tree
	// (tree-context accessors like GetTreeOrg/GetTreeDevice are valid after AddChild).
	InitPipelineBlocks(newTag, newTag.GetPipeline())

	t.notifyBoth(path, newTag)

	return nil
}

// CreateTagWithTemplateLeaf creates a tag whose pipeline falls through to tmplLeaf.
// The new leaf is created with an empty local pipeline so that GetPipeline() returns
// the template's pipeline. If the tag already exists, its template reference is updated
// and any local pipeline is cleared so the fallthrough takes effect.
// InitPipelineBlocks is intentionally NOT called - the template's blocks are already
// initialised and must use Process(leaf, value) for per-leaf context.
func (t *TreeWithOperations) CreateTagWithTemplateLeaf(path string, scalarType ScalarType, config TagConfig, tmplLeaf Leaf) error {
	components := ResolvePath(path)
	if len(components) == 0 {
		return fmt.Errorf("cannot create tag at root")
	}
	config.Type = scalarType

	t.Lock()
	defer t.Unlock()

	current := t.Root
	for i := 0; i < len(components)-1; i++ {
		if current.IsDeleted() {
			return ErrNodeDeleted
		}
		child, ok := current.GetChild(components[i])
		if !ok {
			newNode := NewNode(components[i])
			if err := current.AddChild(newNode); err != nil {
				return err
			}
			t.notifyBoth(buildPath(components[:i+1]), newNode)
			current = newNode
		} else {
			node, ok := child.(*Node)
			if !ok {
				return fmt.Errorf("path component %s is a leaf", components[i])
			}
			if node.IsDeleted() {
				return ErrNodeDeleted
			}
			current = node
		}
	}

	tagName := components[len(components)-1]
	if existing, ok := current.GetChild(tagName); ok {
		// Tag already exists - clear any local pipeline so fallthrough takes effect.
		if existingLeaf, ok := existing.(Leaf); ok {
			shared := existingLeaf.GetShared()
			shared.Pipeline = nil
			existingLeaf.SetShared(shared)
			existingLeaf.SetTemplate(tmplLeaf)
		}
		return nil
	}

	newTag := NewLeaf(scalarType, tagName, config, TagShared{})
	if newTag == nil {
		return fmt.Errorf("unknown scalar type: %v", scalarType)
	}
	newTag.SetTemplate(tmplLeaf)

	if err := current.AddChild(newTag); err != nil {
		return err
	}

	t.notifyBoth(path, newTag)

	return nil
}

// DeleteTag deletes a tag/leaf
func (t *TreeWithOperations) DeleteTag(path string) error {
	components := ResolvePath(path)
	if len(components) == 0 {
		return fmt.Errorf("cannot delete root")
	}

	t.Lock()
	defer t.Unlock()

	// Find parent node
	var parent *Node
	if len(components) == 1 {
		parent = t.Root
	} else {
		parentPath := buildPath(components[:len(components)-1])
		var err error
		parent, err = t.findNodeLocked(parentPath)
		if err != nil {
			return fmt.Errorf("parent not found: %w", err)
		}
	}

	if parent.IsDeleted() {
		return ErrNodeDeleted
	}

	// Get the tag to delete
	tagName := components[len(components)-1]
	child, ok := parent.GetChild(tagName)
	if !ok {
		return fmt.Errorf("tag not found: %s", path)
	}

	// Check if child implements Leaf interface
	leaf, ok := child.(Leaf)
	if !ok {
		return fmt.Errorf("path %s is a node, use DeleteNode", path)
	}

	// Close pipeline blocks before removal
	ClosePipelineBlocks(leaf, leaf.GetPipeline())

	// Notify deletion
	t.notifyBoth(path, nil)

	// Remove from parent
	if err := parent.RemoveChild(tagName); err != nil {
		return err
	}

	return nil
}

// SetLeafValue sets a leaf's value and fires the onChange callback so the
// change is published to NATS subscribers and marked dirty for persistence.
// Use this in preference to calling leaf.SetAnyValue() directly whenever the
// change should be observable outside the tree package.
func (t *TreeWithOperations) SetLeafValue(path string, value any) error {
	leaf, err := t.FindLeaf(path)
	if err != nil {
		return err
	}
	if err := leaf.SetAnyValue(value); err != nil {
		return err
	}
	if t.onChange != nil {
		t.onChange(path, leaf)
	}
	return nil
}

// PrintTree prints the full tree structure to stdout, starting from the root,
// showing only node/leaf names with indentation to reflect hierarchy.
func (t *TreeWithOperations) PrintTree() {
	t.RLock()
	defer t.RUnlock()
	fmt.Println(t.Root.Name)
	printNodeChildren(t.Root, "")
}

// printNodeChildren recursively prints children with tree-style connectors.
func printNodeChildren(node *Node, prefix string) {
	children := node.GetChildren()
	names := make([]string, 0, len(children))
	for name := range children {
		names = append(names, name)
	}
	sort.Strings(names)

	for i, name := range names {
		child := children[name]
		last := i == len(names)-1
		connector := "├── "
		childPrefix := prefix + "│   "
		if last {
			connector = "└── "
			childPrefix = prefix + "    "
		}
		fmt.Println(prefix + connector + name)
		if childNode, ok := child.(*Node); ok {
			printNodeChildren(childNode, childPrefix)
		}
	}
}

// WalkLeaves calls fn for every leaf in the tree, passing the full slash-path
// and the leaf. The tree read lock is held for the duration of the walk, so fn
// must not attempt to acquire it.
func (t *TreeWithOperations) WalkLeaves(fn func(path string, leaf Leaf)) {
	t.RLock()
	defer t.RUnlock()
	walkNodeLeaves(t.Root, "", fn)
}

func walkNodeLeaves(node *Node, prefix string, fn func(string, Leaf)) {
	for name, child := range node.GetChildren() {
		path := prefix + "." + name
		switch c := child.(type) {
		case *Node:
			walkNodeLeaves(c, path, fn)
		case Leaf:
			fn(path, c)
		}
	}
}

// buildPath builds a path string from components
func buildPath(components []string) string {
	if len(components) == 0 {
		return ""
	}
	result := ""
	for _, comp := range components {
		result += "." + comp
	}
	return result
}

// RenameChild renames a direct child (node or leaf) of the given parent path.
// It removes the old key from the parent's Children map, updates the child's
// name, and re-inserts under the new key. All subscribers are notified of the
// removal at the old path and creation at the new path.
func (t *TreeWithOperations) RenameChild(parentPath, oldName, newName string) error {
	if oldName == "" || newName == "" {
		return fmt.Errorf("old and new names must be non-empty")
	}
	if oldName == newName {
		return nil
	}

	t.Lock()
	defer t.Unlock()

	// Find parent
	parent, err := t.findNodeLocked(parentPath)
	if err != nil {
		return fmt.Errorf("parent not found: %w", err)
	}
	if parent.IsDeleted() {
		return ErrNodeDeleted
	}

	// Verify old child exists and new name doesn't collide
	child, ok := parent.GetChild(oldName)
	if !ok {
		return fmt.Errorf("child %q not found under %s", oldName, parentPath)
	}
	if _, exists := parent.GetChild(newName); exists {
		return fmt.Errorf("a child named %q already exists under %s", newName, parentPath)
	}

	oldPath := parentPath + "." + oldName
	newPath := parentPath + "." + newName

	// Notify deletion of old paths
	if node, ok := child.(*Node); ok {
		t.notifyBoth(oldPath, nil)
		t.notifyDeleteRecursive(oldPath, node)
	} else {
		t.notifyBoth(oldPath, nil)
	}

	// Remove from old key, update name, re-add under new key
	parent.mu.Lock()
	delete(parent.Children, oldName)
	switch c := child.(type) {
	case *Node:
		c.mu.Lock()
		c.Name = newName
		c.mu.Unlock()
	case *LeafNode:
		c.mu.Lock()
		c.name = newName
		c.mu.Unlock()
	}
	parent.Children[newName] = child
	parent.mu.Unlock()

	// Notify creation at new paths
	t.notifyBoth(newPath, child)
	if node, ok := child.(*Node); ok {
		t.notifyRenameRecursive(newPath, node)
	}

	return nil
}

// notifyRenameRecursive notifies about all children under a newly renamed node.
func (t *TreeWithOperations) notifyRenameRecursive(basePath string, node *Node) {
	for name, child := range node.GetChildren() {
		childPath := basePath + "." + name
		t.notifyBoth(childPath, child)
		if childNode, ok := child.(*Node); ok {
			t.notifyRenameRecursive(childPath, childNode)
		}
	}
}

// LockNode locks a node to prevent structural modifications to its subtree
func (t *TreeWithOperations) LockNode(path string) error {
	t.Lock()
	defer t.Unlock()
	node, err := t.findNodeLocked(path)
	if err != nil {
		return err
	}
	node.mu.Lock()
	node.locked = true
	node.mu.Unlock()
	return nil
}

// UnlockNode unlocks a node
func (t *TreeWithOperations) UnlockNode(path string) error {
	t.Lock()
	defer t.Unlock()
	node, err := t.findNodeLocked(path)
	if err != nil {
		return err
	}
	node.mu.Lock()
	node.locked = false
	node.mu.Unlock()
	return nil
}
