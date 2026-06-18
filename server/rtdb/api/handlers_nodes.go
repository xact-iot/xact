package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/xact-iot/xact/rtdb/tree"
)

// pathToNatsSubject converts a slash path to dot notation for NATS subjects
// e.g., "/LTA/VMS/EVMS_RS10" -> "LTA.VMS.EVMS_RS10"
func pathToNatsSubject(path string) string {
	// Remove leading slash and replace slashes with dots
	path = strings.TrimPrefix(path, "/")
	path = strings.ReplaceAll(path, "/", ".")
	return path
}

// CreateNodeRequest represents a create node request
type CreateNodeRequest struct {
	Path         string `json:"path"`
	Description  string `json:"description,omitempty"`
	TemplateName string `json:"templateName,omitempty"`
	NodeType     string `json:"nodeType,omitempty"` // "Standard" | "Device" | "Organisation"
	IsDevice     bool   `json:"isDevice,omitempty"` // deprecated: use NodeType instead
	IsArray      bool   `json:"isArray,omitempty"`
}

// CreateNodeResponse represents a create node response
type CreateNodeResponse struct {
	Path         string `json:"path"`
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	TemplateName string `json:"templateName,omitempty"`
}

// NodeResponse represents a node response
type NodeResponse struct {
	Path         string      `json:"path"`
	Name         string      `json:"name"`
	Description  string      `json:"description,omitempty"`
	TemplateName string      `json:"templateName,omitempty"`
	Type         string      `json:"type,omitempty"`
	Locked       bool        `json:"locked"`
	IsArray      bool        `json:"isArray,omitempty"`
	Children     []ChildInfo `json:"children,omitempty"`
}

type UpdateNodeRequest struct {
	Name         string `json:"name,omitempty"`
	Description  string `json:"description,omitempty"`
	TemplateName string `json:"templateName,omitempty"`
	IsDevice     bool   `json:"isDevice,omitempty"`
	Locked       *bool  `json:"locked,omitempty"`
}

// ChildInfo represents child node info
type ChildInfo struct {
	Name        string          `json:"name"`
	Type        string          `json:"type"` // "node" or "leaf"
	Config      *tree.TagConfig `json:"config,omitempty"`
	Description string          `json:"description,omitempty"`
	IsArray     bool            `json:"isArray,omitempty"`
	Children    []ChildInfo     `json:"children,omitempty"`
	// Full tag metadata (populated when depth > 0 or for leaf children at any depth)
	Shared    *TagSharedJSON `json:"shared,omitempty"`
	Value     any            `json:"value,omitempty"`
	ValueType string         `json:"value_type,omitempty"`
	Status    string         `json:"status,omitempty"`
	Timestamp int64          `json:"timestamp,omitempty"`
}

// buildTagSharedJSON serialises a leaf's effective shared data including the pipeline.
func buildTagSharedJSON(leaf tree.Leaf) TagSharedJSON {
	shared := leaf.GetShared()
	s := TagSharedJSON{Description: leaf.GetDescription(), Units: shared.Units, Deadband: shared.Deadband}
	if len(shared.EnumValues) > 0 {
		enumValues := make(map[int]string, len(shared.EnumValues))
		for k, v := range shared.EnumValues {
			enumValues[k] = v
		}
		s.EnumValues = enumValues
	}
	if pipeline := leaf.GetPipeline(); len(pipeline) > 0 {
		if envelopes, err := tree.MarshalPipeline(pipeline); err == nil {
			s.Pipeline = envelopes
		}
		if len(shared.Pipeline) == 0 {
			s.PipelineInherited = true
		}
	}
	return s
}

func (s *Server) handleCreateNodeWithSchema() openAPIHandler {
	return openAPIHandler{
		Handler:     s.handleCreateNode,
		RequestBody: jsonRequestBody(CreateNodeRequest{}),
		Responses:   responseSchema(http.StatusCreated, CreateNodeResponse{}),
		Tags:        []string{"nodes"},
	}
}

// handleCreateNode creates a new node
func (s *Server) handleCreateNode(w http.ResponseWriter, r *http.Request) {
	var req CreateNodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	if req.Path == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "path is required"})
		return
	}
	path, ok := s.rtdbPathForRequest(r, req.Path, false)
	if !ok {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "path is outside your organisation scope"})
		return
	}

	// Resolve node type: NodeType field takes precedence, IsDevice is the legacy fallback
	nodeType := req.NodeType
	if nodeType == "" && req.IsDevice {
		nodeType = string(tree.NodeTypeDevice)
	}

	// Create the node
	var err error
	switch nodeType {
	case string(tree.NodeTypeDevice):
		err = s.tree.CreateDeviceNode(path, req.TemplateName)
	case string(tree.NodeTypeOrganisation):
		err = s.tree.CreateOrganisationNode(path, req.TemplateName)
	default:
		err = s.tree.CreateNode(path, req.TemplateName)
	}
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// Find the created node to update description
	node, err := s.tree.FindNode(path)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// Update description if provided
	if req.Description != "" {
		node.SetDescription(req.Description)
	}

	// Mark as array container if requested
	if req.IsArray {
		node.SetIsArray(true)
	}

	// Publish change to NATS (convert path to dot notation)
	if s.treeSync != nil {
		natsPath := pathToNatsSubject(path)
		if err := s.treeSync.PublishChange(natsPath, node); err != nil {
			// Log but don't fail the request
		}
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(CreateNodeResponse{
		Path:         path,
		Name:         node.Name,
		Description:  node.GetDescription(),
		TemplateName: node.GetTemplateName(),
	})
}

func (s *Server) handleGetNodeWithSchema() openAPIHandler {
	return handlerWithSchema(s.handleGetNode, nil, NodeResponse{}, "nodes")
}

// handleGetNode retrieves node information with optional recursive depth.
// Query param "depth" controls how many levels of children to include:
//   - depth=0 (default): immediate children only (original behavior)
//   - depth=1,2,3...: include that many levels of descendants
//   - depth=-1: entire subtree
//
// When depth > 0, leaf children include full tag metadata (shared, value, status).
func (s *Server) handleGetNode(w http.ResponseWriter, r *http.Request) {
	path, ok := s.rtdbPathForRequest(r, chi.URLParam(r, "*"), true)
	if !ok {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "path is required"})
		return
	}

	// Parse depth query param (default 0)
	depth := 0
	if depthStr := r.URL.Query().Get("depth"); depthStr != "" {
		if d, err := strconv.Atoi(depthStr); err == nil {
			depth = d
		}
	}

	node, err := s.tree.FindNode(path)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// Build response
	response := NodeResponse{
		Path:         path,
		Name:         node.Name,
		Description:  node.GetDescription(),
		TemplateName: node.GetTemplateName(),
		Type:         string(node.GetNodeType()),
		Locked:       node.IsLocked(),
		IsArray:      node.GetIsArray(),
	}

	// Only include children if depth >= 0 (depth=0 means immediate children only)
	// depth=-1 means entire subtree, which also should include children
	if depth >= 0 || depth == -1 {
		response.Children = s.buildChildren(node, depth)
	}

	json.NewEncoder(w).Encode(response)
}

// buildChildren recursively builds ChildInfo slices up to maxDepth.
// maxDepth: -1 means infinite (entire subtree), 0 means immediate children only.
func (s *Server) buildChildren(node *tree.Node, maxDepth int) []ChildInfo {
	children := node.GetChildren()
	result := make([]ChildInfo, 0, len(children))

	for name, child := range children {
		childInfo := ChildInfo{Name: name}

		if child.IsNode() {
			childInfo.Type = "node"
			if n, ok := child.(*tree.Node); ok {
				childInfo.Description = n.GetDescription()
				childInfo.IsArray = n.GetIsArray()
			}
			// Recurse if we haven't hit maxDepth
			if maxDepth != 0 {
				childInfo.Children = s.buildChildren(child.(*tree.Node), maxDepth-1)
			}
		} else {
			childInfo.Type = "leaf"
			if leaf, ok := child.(tree.Leaf); ok {
				config := leaf.GetConfig()
				childInfo.Config = &config
				childInfo.Description = leaf.GetDescription()

				// Include full tag metadata for leaves
				shared := buildTagSharedJSON(leaf)
				childInfo.Shared = &shared
				childInfo.Value = leaf.GetAnyValue()
				childInfo.ValueType = leaf.ValueType().String()
				childInfo.Status = leaf.GetState()
				childInfo.Timestamp = leaf.GetUpdatedTime().UnixMilli()
			}
		}

		result = append(result, childInfo)
	}

	return result
}

func (s *Server) handleUpdateNodeWithSchema() openAPIHandler {
	return handlerWithSchema(s.handleUpdateNode, UpdateNodeRequest{}, NodeResponse{}, "nodes")
}

// handleUpdateNode updates node metadata
func (s *Server) handleUpdateNode(w http.ResponseWriter, r *http.Request) {
	path, ok := s.rtdbPathForRequest(r, chi.URLParam(r, "*"), false)
	if !ok {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "path is required"})
		return
	}

	var req UpdateNodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// Rename the node if a new name is provided
	if req.Name != "" {
		components := tree.ResolvePath(path)
		oldName := components[len(components)-1]
		if req.Name != oldName {
			parentPath := "/"
			if len(components) > 1 {
				parentPath = "/" + strings.Join(components[:len(components)-1], "/")
			}
			if err := s.tree.RenameChild(parentPath, oldName, req.Name); err != nil {
				w.WriteHeader(http.StatusConflict)
				json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			// Update path to the new location
			components[len(components)-1] = req.Name
			path = strings.Join(components, "/")
		}
	}

	node, err := s.tree.FindNode(path)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// Update node fields
	if req.Description != "" {
		node.SetDescription(req.Description)
	}
	if req.TemplateName != "" {
		node.SetTemplateName(req.TemplateName)
	}

	// Upgrade to Device Node if requested
	if req.IsDevice && node.GetNodeType() != tree.NodeTypeDevice {
		err := s.tree.CreateDeviceNode(path, node.GetTemplateName())
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
	}

	// Handle lock/unlock
	if req.Locked != nil {
		if *req.Locked {
			if err := s.tree.LockNode(path); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
		} else {
			if err := s.tree.UnlockNode(path); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
		}
	}

	// Fire onChange so persistence is marked dirty and NATS subscribers are notified.
	s.tree.NotifyChange(path, node)

	json.NewEncoder(w).Encode(NodeResponse{
		Path:         path,
		Name:         node.Name,
		Description:  node.GetDescription(),
		TemplateName: node.GetTemplateName(),
		Type:         string(node.GetNodeType()),
		Locked:       node.IsLocked(),
		IsArray:      node.GetIsArray(),
	})
}

func (s *Server) handleDeleteNodeWithSchema() openAPIHandler {
	return handlerWithResponses(s.handleDeleteNode, map[int]any{http.StatusNoContent: nil}, "nodes")
}

// handleDeleteNode deletes a node (cascade)
func (s *Server) handleDeleteNode(w http.ResponseWriter, r *http.Request) {
	path, ok := s.rtdbPathForRequest(r, chi.URLParam(r, "*"), false)
	if !ok {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "path is required"})
		return
	}

	s.tree.PropagateTemplateNodeDelete(path)

	// Publish deletion to NATS before deleting (convert path to dot notation)
	if s.treeSync != nil {
		natsPath := pathToNatsSubject(path)
		if err := s.treeSync.PublishChange(natsPath, nil); err != nil {
			// Log but don't fail the request
		}
	}

	err := s.tree.DeleteNode(path)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
