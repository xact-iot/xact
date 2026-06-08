package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/xact-iot/xact/rtdb/tree"
)

// CreateTagRequest represents a create tag request
type CreateTagRequest struct {
	Path   string          `json:"path"`
	Type   tree.ScalarType `json:"type"`
	Config tree.TagConfig  `json:"config,omitempty"`
	Shared tree.TagShared  `json:"shared,omitempty"`
	Value  interface{}     `json:"value,omitempty"`
}

// TagSharedJSON is the JSON-serialisable form of TagShared, including pipeline envelopes.
type TagSharedJSON struct {
	Description       string                      `json:"description,omitempty"`
	Units             string                      `json:"units,omitempty"`
	Deadband          float64                     `json:"deadband,omitempty"`
	EnumValues        map[int]string              `json:"enumValues,omitempty"`
	Pipeline          []tree.ProcessBlockEnvelope `json:"pipeline,omitempty"`
	PipelineInherited bool                        `json:"pipelineInherited,omitempty"`
}

// TagResponse represents a tag response
type TagResponse struct {
	Path      string         `json:"path"`
	Name      string         `json:"name"`
	Type      string         `json:"type"`
	Config    tree.TagConfig `json:"config"`
	Shared    TagSharedJSON  `json:"shared"`
	Value     interface{}    `json:"value,omitempty"`
	ValueType string         `json:"value_type"`
	Status    string         `json:"status"`
	Timestamp int64          `json:"timestamp"`
}

func buildTagResponse(path string, leaf tree.Leaf) TagResponse {
	return TagResponse{
		Path:      path,
		Name:      leaf.GetName(),
		Type:      "leaf",
		Config:    leaf.GetConfig(),
		Shared:    buildTagSharedJSON(leaf),
		Value:     leaf.GetAnyValue(),
		ValueType: leaf.ValueType().String(),
		Status:    leaf.GetState(),
		Timestamp: leaf.GetUpdatedTime().UnixMilli(),
	}
}

// handleCreateTag creates a new tag/leaf
func (s *Server) handleCreateTag(w http.ResponseWriter, r *http.Request) {
	var req CreateTagRequest
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

	// Create the tag
	err := s.tree.CreateTag(path, req.Type, req.Config, req.Shared)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// Set initial value if provided
	if req.Value != nil {
		leaf, err := s.tree.FindLeaf(path)
		if err == nil {
			leaf.SetAnyValue(req.Value)
		}
	}

	// Find the created tag
	leaf, err := s.tree.FindLeaf(path)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// CreateTag already fires onChange (→ NATS + persistence). No extra notify needed.
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(buildTagResponse(path, leaf))
}

// handleGetTag retrieves tag information
func (s *Server) handleGetTag(w http.ResponseWriter, r *http.Request) {
	path, ok := s.rtdbPathForRequest(r, chi.URLParam(r, "*"), false)
	if !ok {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "path is required"})
		return
	}

	leaf, err := s.tree.FindLeaf(path)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	json.NewEncoder(w).Encode(buildTagResponse(path, leaf))
}

// handleUpdateTag updates tag metadata and value
func (s *Server) handleUpdateTag(w http.ResponseWriter, r *http.Request) {
	path, ok := s.rtdbPathForRequest(r, chi.URLParam(r, "*"), false)
	if !ok {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "path is required"})
		return
	}

	var req struct {
		Name        string                       `json:"name,omitempty"`
		Shared      tree.TagShared               `json:"shared,omitempty"`
		Value       interface{}                  `json:"value,omitempty"`
		Pipeline    *[]tree.ProcessBlockEnvelope `json:"pipeline"`
		Description string                       `json:"description,omitempty"`
		Units       string                       `json:"units,omitempty"`
		Deadband    *float64                     `json:"deadband,omitempty"`
		EnumValues  *map[int]string              `json:"enumValues,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// Rename the tag if a new name is provided
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
			components[len(components)-1] = req.Name
			path = strings.Join(components, "/")
		}
	}

	leaf, err := s.tree.FindLeaf(path)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// Track whether a structural change occurred (needs persistence).
	structuralChange := false

	// Update shared fields if provided at top level
	if req.Description != "" || req.Units != "" || req.Deadband != nil {
		shared := leaf.GetShared()
		if req.Description != "" {
			shared.Description = req.Description
		}
		if req.Units != "" {
			shared.Units = req.Units
		}
		if req.Deadband != nil {
			shared.Deadband = *req.Deadband
		}
		leaf.SetShared(shared)
		structuralChange = true
	}

	if req.EnumValues != nil {
		shared := leaf.GetShared()
		shared.EnumValues = *req.EnumValues
		leaf.SetShared(shared)
		if enumLeaf, ok := leaf.(interface{ SetEnumValues(map[int]string) }); ok {
			enumLeaf.SetEnumValues(*req.EnumValues)
		}
		structuralChange = true
	}

	// Update shared if provided
	if req.Shared.Description != "" || req.Shared.Units != "" || req.Shared.Deadband != 0 || len(req.Shared.EnumValues) > 0 || len(req.Shared.Pipeline) > 0 {
		shared := leaf.GetShared()
		if req.Shared.Description != "" {
			shared.Description = req.Shared.Description
		}
		if req.Shared.Units != "" {
			shared.Units = req.Shared.Units
		}
		if req.Shared.Deadband != 0 {
			shared.Deadband = req.Shared.Deadband
		}
		if len(req.Shared.EnumValues) > 0 {
			shared.EnumValues = req.Shared.EnumValues
			if enumLeaf, ok := leaf.(interface{ SetEnumValues(map[int]string) }); ok {
				enumLeaf.SetEnumValues(req.Shared.EnumValues)
			}
		}
		if len(req.Shared.Pipeline) > 0 {
			shared.Pipeline = req.Shared.Pipeline
		}
		leaf.SetShared(shared)
		structuralChange = true
	}

	// Update pipeline if explicitly provided (nil = not provided, empty slice = clear pipeline)
	if req.Pipeline != nil {
		envelopes := *req.Pipeline
		if len(envelopes) == 0 {
			// Clear pipeline
			shared := leaf.GetShared()
			shared.Pipeline = nil
			leaf.SetShared(shared)
		} else {
			pipeline, err := tree.UnmarshalPipeline(envelopes)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": "invalid pipeline: " + err.Error()})
				return
			}
			shared := leaf.GetShared()
			shared.Pipeline = pipeline
			leaf.SetShared(shared)
			tree.InitPipelineBlocks(leaf, pipeline)
		}
		structuralChange = true
	}

	// Update value if provided
	if req.Value != nil {
		leaf.SetAnyValue(req.Value)
	}

	// Fire onChange only for structural changes (description, units, pipeline)
	// which need persistence. Value-only updates are broadcast via the
	// publish block in the pipeline and do not need to persist the tree config.
	if structuralChange {
		s.tree.NotifyChange(path, leaf)
	}

	json.NewEncoder(w).Encode(buildTagResponse(path, leaf))
}

// DebugStepResult represents the result of a single pipeline step
type DebugStepResult struct {
	Type        string      `json:"type"`
	Input       interface{} `json:"input"`
	Output      interface{} `json:"output"`
	Error       string      `json:"error,omitempty"`
	StateChange string      `json:"stateChange,omitempty"`
}

// handleDebugTagPipeline runs the tag pipeline on a test input and returns step-by-step results
func (s *Server) handleDebugTagPipeline(w http.ResponseWriter, r *http.Request) {
	path, ok := s.rtdbPathForRequest(r, chi.URLParam(r, "*"), false)
	if !ok {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "path is required"})
		return
	}

	var req struct {
		Input interface{} `json:"input"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	leaf, err := s.tree.FindLeaf(path)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	pipeline := leaf.GetPipeline()
	steps := make([]DebugStepResult, 0, len(pipeline))
	value := req.Input

	for _, block := range pipeline {
		input := value
		stateBefore := leaf.GetState()

		output, blockErr := block.Process(leaf, value)

		step := DebugStepResult{
			Type:  block.GetType(),
			Input: input,
		}

		stateAfter := leaf.GetState()
		if stateAfter != stateBefore {
			step.StateChange = stateAfter
		}

		if blockErr != nil {
			step.Error = blockErr.Error()
			step.Output = input // pass through on error
		} else {
			step.Output = output
			value = output
		}

		steps = append(steps, step)
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"steps":       steps,
		"finalOutput": value,
		"blockCount":  len(pipeline),
	})
}

// handleGetBlockSchemas returns the self-reported schemas for all registered ProcessBlock types.
func (s *Server) handleGetBlockSchemas(w http.ResponseWriter, r *http.Request) {
	schemas := tree.GetRegisteredSchemas()
	json.NewEncoder(w).Encode(schemas)
}

// handleDeleteTag deletes a tag
func (s *Server) handleDeleteTag(w http.ResponseWriter, r *http.Request) {
	path, ok := s.rtdbPathForRequest(r, chi.URLParam(r, "*"), false)
	if !ok {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "path is required"})
		return
	}

	// Publish deletion to NATS before deleting
	if s.treeSync != nil {
		if err := s.treeSync.PublishChange(path, nil); err != nil {
			// Log but don't fail the request
		}
	}

	err := s.tree.DeleteTag(path)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
