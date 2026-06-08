package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xact-iot/xact/rtdb/tree"
)

type noopTagValuePublisher struct{}

func (noopTagValuePublisher) TagValuePublish(_ string, _ []byte) error { return nil }

func init() {
	tree.TagValuePublisher = noopTagValuePublisher{}
}

// ── pathToNatsSubject ────────────────────────────────────────────────────────

func TestPathToNatsSubject(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/LTA/VMS/EVMS_RS10", "LTA.VMS.EVMS_RS10"},
		{"LTA/VMS", "LTA.VMS"},
		{"/single", "single"},
		{"", ""},
	}
	for _, tt := range tests {
		got := pathToNatsSubject(tt.input)
		if got != tt.want {
			t.Errorf("pathToNatsSubject(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// ── selectOrg ────────────────────────────────────────────────────────────────
// selectOrg is tested indirectly via the login endpoint integration tests
// in server_test.go since it uses sqldb.UserOrg.

// ── handleUpdateNode ─────────────────────────────────────────────────────────

func TestUpdateNode(t *testing.T) {
	treeOps := tree.NewTreeWithOperations(nil)
	treeOps.CreateNode("tenant1/building/floor1", "")

	server := NewServer(ServerConfig{}, treeOps, nil, nil, "test-secret", nil, "")
	token := generateTestToken([]byte("test-secret"), "user1")

	t.Run("update description", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"description": "Updated Floor 1",
		})
		req := httptest.NewRequest("PUT", "/api/v1/nodes/tenant1/building/floor1", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		server.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var resp NodeResponse
		json.Unmarshal(rr.Body.Bytes(), &resp)
		if resp.Description != "Updated Floor 1" {
			t.Errorf("description = %q, want 'Updated Floor 1'", resp.Description)
		}
	})

	t.Run("update template name", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"templateName": "tmpl.VMS",
		})
		req := httptest.NewRequest("PUT", "/api/v1/nodes/tenant1/building/floor1", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		server.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rr.Code)
		}

		var resp NodeResponse
		json.Unmarshal(rr.Body.Bytes(), &resp)
		if resp.TemplateName != "tmpl.VMS" {
			t.Errorf("templateName = %q", resp.TemplateName)
		}
	})

	t.Run("lock node", func(t *testing.T) {
		locked := true
		body, _ := json.Marshal(map[string]any{
			"locked": locked,
		})
		req := httptest.NewRequest("PUT", "/api/v1/nodes/tenant1/building/floor1", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		server.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var resp NodeResponse
		json.Unmarshal(rr.Body.Bytes(), &resp)
		if !resp.Locked {
			t.Error("expected locked=true")
		}
	})

	t.Run("unlock node", func(t *testing.T) {
		locked := false
		body, _ := json.Marshal(map[string]any{
			"locked": locked,
		})
		req := httptest.NewRequest("PUT", "/api/v1/nodes/tenant1/building/floor1", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		server.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rr.Code)
		}

		var resp NodeResponse
		json.Unmarshal(rr.Body.Bytes(), &resp)
		if resp.Locked {
			t.Error("expected locked=false")
		}
	})

	t.Run("not found", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"description": "x"})
		req := httptest.NewRequest("PUT", "/api/v1/nodes/tenant1/nonexistent", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		server.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", rr.Code)
		}
	})

	t.Run("bad json", func(t *testing.T) {
		req := httptest.NewRequest("PUT", "/api/v1/nodes/tenant1/building/floor1", bytes.NewReader([]byte("{invalid")))
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		server.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rr.Code)
		}
	})
}

// ── handleUpdateTag ──────────────────────────────────────────────────────────

func TestUpdateTag(t *testing.T) {
	treeOps := tree.NewTreeWithOperations(nil)
	treeOps.CreateNode("tenant1/building", "")
	treeOps.UnlockNode("tenant1/building")
	treeOps.CreateTag("tenant1/building/temperature", tree.TypeFloat, tree.TagConfig{Name: "Temp"})

	leaf, _ := treeOps.FindLeaf("tenant1/building/temperature")
	leaf.SetFloat(25.5)

	server := NewServer(ServerConfig{}, treeOps, nil, nil, "test-secret", nil, "")
	token := generateTestToken([]byte("test-secret"), "user1")

	t.Run("update value", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"value": 30.0,
		})
		req := httptest.NewRequest("PUT", "/api/v1/tags/tenant1/building/temperature", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		server.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var resp TagResponse
		json.Unmarshal(rr.Body.Bytes(), &resp)
		if resp.Value != 30.0 {
			t.Errorf("value = %v, want 30.0", resp.Value)
		}
	})

	t.Run("update description and units", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"description": "Room temperature",
			"units":       "°C",
			"deadband":    0.25,
		})
		req := httptest.NewRequest("PUT", "/api/v1/tags/tenant1/building/temperature", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		server.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var resp TagResponse
		json.Unmarshal(rr.Body.Bytes(), &resp)
		if resp.Shared.Units != "°C" {
			t.Errorf("units = %q", resp.Shared.Units)
		}
		if resp.Shared.Deadband != 0.25 {
			t.Errorf("deadband = %v", resp.Shared.Deadband)
		}
	})

	t.Run("clear pipeline", func(t *testing.T) {
		emptyPipeline := []tree.ProcessBlockEnvelope{}
		body, _ := json.Marshal(map[string]any{
			"pipeline": emptyPipeline,
		})
		req := httptest.NewRequest("PUT", "/api/v1/tags/tenant1/building/temperature", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		server.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("not found", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"value": 1.0})
		req := httptest.NewRequest("PUT", "/api/v1/tags/tenant1/nonexistent", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		server.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", rr.Code)
		}
	})

	t.Run("bad json", func(t *testing.T) {
		req := httptest.NewRequest("PUT", "/api/v1/tags/tenant1/building/temperature", bytes.NewReader([]byte("{invalid")))
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		server.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rr.Code)
		}
	})
}

// ── handleDeleteTag ──────────────────────────────────────────────────────────

func TestDeleteTag(t *testing.T) {
	treeOps := tree.NewTreeWithOperations(nil)
	treeOps.CreateNode("tenant1/building", "")
	treeOps.UnlockNode("tenant1/building")
	treeOps.CreateTag("tenant1/building/temperature", tree.TypeFloat, tree.TagConfig{Name: "Temp"})

	server := NewServer(ServerConfig{}, treeOps, nil, nil, "test-secret", nil, "")
	token := generateTestToken([]byte("test-secret"), "user1")

	req := httptest.NewRequest("DELETE", "/api/v1/tags/tenant1/building/temperature", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	server.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d: %s", rr.Code, rr.Body.String())
	}

	// Verify tag is deleted
	_, err := treeOps.FindLeaf("tenant1/building/temperature")
	if err == nil {
		t.Error("expected tag to be deleted")
	}
}

func TestDeleteTagNotFound(t *testing.T) {
	treeOps := tree.NewTreeWithOperations(nil)
	server := NewServer(ServerConfig{}, treeOps, nil, nil, "test-secret", nil, "")
	token := generateTestToken([]byte("test-secret"), "user1")

	req := httptest.NewRequest("DELETE", "/api/v1/tags/tenant1/nonexistent", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	server.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rr.Code)
	}
}

// ── handleGetBlockSchemas ────────────────────────────────────────────────────

func TestGetBlockSchemas(t *testing.T) {
	treeOps := tree.NewTreeWithOperations(nil)
	server := NewServer(ServerConfig{}, treeOps, nil, nil, "test-secret", nil, "")
	token := generateTestToken([]byte("test-secret"), "user1")

	req := httptest.NewRequest("GET", "/api/v1/blocks/schemas", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	server.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	// Response should be valid JSON array
	var schemas []any
	if err := json.Unmarshal(rr.Body.Bytes(), &schemas); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
}

// ── handleDebugTagPipeline ───────────────────────────────────────────────────

func TestDebugTagPipeline(t *testing.T) {
	treeOps := tree.NewTreeWithOperations(nil)
	treeOps.CreateNode("tenant1/building", "")
	treeOps.UnlockNode("tenant1/building")
	treeOps.CreateTag("tenant1/building/temperature", tree.TypeFloat, tree.TagConfig{Name: "Temp"})

	server := NewServer(ServerConfig{}, treeOps, nil, nil, "test-secret", nil, "")
	token := generateTestToken([]byte("test-secret"), "user1")

	t.Run("basic debug", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"input": 25.5})
		req := httptest.NewRequest("POST", "/api/v1/debug/tags/tenant1/building/temperature", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		server.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var resp map[string]any
		json.Unmarshal(rr.Body.Bytes(), &resp)
		if resp["finalOutput"] == nil {
			t.Error("expected finalOutput in response")
		}
	})

	t.Run("tag not found", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"input": 1.0})
		req := httptest.NewRequest("POST", "/api/v1/debug/tags/tenant1/nonexistent", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		server.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", rr.Code)
		}
	})

	t.Run("bad json", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/debug/tags/tenant1/building/temperature", bytes.NewReader([]byte("{invalid")))
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		server.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rr.Code)
		}
	})
}

// ── handleGetNode with children ──────────────────────────────────────────────

func TestGetNodeWithChildren(t *testing.T) {
	treeOps := tree.NewTreeWithOperations(nil)
	treeOps.CreateNode("tenant1/building", "")
	treeOps.UnlockNode("tenant1/building")
	treeOps.CreateNode("tenant1/building/floor1", "")
	treeOps.CreateTag("tenant1/building/temp", tree.TypeFloat, tree.TagConfig{Name: "Temp"})

	server := NewServer(ServerConfig{}, treeOps, nil, nil, "test-secret", nil, "")
	token := generateTestToken([]byte("test-secret"), "user1")

	req := httptest.NewRequest("GET", "/api/v1/nodes/tenant1/building", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	server.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp NodeResponse
	json.Unmarshal(rr.Body.Bytes(), &resp)

	if len(resp.Children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(resp.Children))
	}

	// Check child types
	childTypes := map[string]string{}
	for _, c := range resp.Children {
		childTypes[c.Name] = c.Type
	}
	if childTypes["floor1"] != "node" {
		t.Errorf("floor1 type = %q, want node", childTypes["floor1"])
	}
	if childTypes["temp"] != "leaf" {
		t.Errorf("temp type = %q, want leaf", childTypes["temp"])
	}
}

func TestGetNodeNotFound(t *testing.T) {
	treeOps := tree.NewTreeWithOperations(nil)
	server := NewServer(ServerConfig{}, treeOps, nil, nil, "test-secret", nil, "")
	token := generateTestToken([]byte("test-secret"), "user1")

	req := httptest.NewRequest("GET", "/api/v1/nodes/tenant1/nonexistent", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	server.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

func TestGetNodeEmptyPath(t *testing.T) {
	treeOps := tree.NewTreeWithOperations(nil)
	// Create the tenant org so root lookup works
	treeOps.CreateNode("tenant1", "")

	server := NewServer(ServerConfig{}, treeOps, nil, nil, "test-secret", nil, "")
	token := generateTestToken([]byte("test-secret"), "user1")

	req := httptest.NewRequest("GET", "/api/v1/nodes/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	server.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ── handleCreateNode variations ──────────────────────────────────────────────

func TestCreateNodeEmptyPath(t *testing.T) {
	treeOps := tree.NewTreeWithOperations(nil)
	server := NewServer(ServerConfig{}, treeOps, nil, nil, "test-secret", nil, "")
	token := generateTestToken([]byte("test-secret"), "user1")

	body, _ := json.Marshal(CreateNodeRequest{Path: ""})
	req := httptest.NewRequest("POST", "/api/v1/nodes/", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	server.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestCreateNodeBadJSON(t *testing.T) {
	treeOps := tree.NewTreeWithOperations(nil)
	server := NewServer(ServerConfig{}, treeOps, nil, nil, "test-secret", nil, "")
	token := generateTestToken([]byte("test-secret"), "user1")

	req := httptest.NewRequest("POST", "/api/v1/nodes/", bytes.NewReader([]byte("{invalid")))
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	server.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestCreateNodeWithDescription(t *testing.T) {
	treeOps := tree.NewTreeWithOperations(nil)
	server := NewServer(ServerConfig{}, treeOps, nil, nil, "test-secret", nil, "")
	token := generateTestToken([]byte("test-secret"), "user1")

	body, _ := json.Marshal(CreateNodeRequest{
		Path:        "tenant1/building/floor2",
		Description: "Second floor",
	})
	req := httptest.NewRequest("POST", "/api/v1/nodes/", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	server.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp CreateNodeResponse
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Description != "Second floor" {
		t.Errorf("description = %q", resp.Description)
	}
}

func TestCreateNodeDeviceType(t *testing.T) {
	treeOps := tree.NewTreeWithOperations(nil)
	server := NewServer(ServerConfig{}, treeOps, nil, nil, "test-secret", nil, "")
	token := generateTestToken([]byte("test-secret"), "user1")

	// Create parent nodes first
	treeOps.CreateNode("tenant1/building", "")
	treeOps.UnlockNode("tenant1/building")

	body, _ := json.Marshal(CreateNodeRequest{
		Path:     "tenant1/building/device1",
		NodeType: "Device",
	})
	req := httptest.NewRequest("POST", "/api/v1/nodes/", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	server.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestCreateNodeLegacyIsDevice(t *testing.T) {
	treeOps := tree.NewTreeWithOperations(nil)
	server := NewServer(ServerConfig{}, treeOps, nil, nil, "test-secret", nil, "")
	token := generateTestToken([]byte("test-secret"), "user1")

	treeOps.CreateNode("tenant1/building", "")
	treeOps.UnlockNode("tenant1/building")

	body, _ := json.Marshal(CreateNodeRequest{
		Path:     "tenant1/building/device2",
		IsDevice: true,
	})
	req := httptest.NewRequest("POST", "/api/v1/nodes/", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	server.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
}

// ── handleCreateTag variations ───────────────────────────────────────────────

func TestCreateTagEmptyPath(t *testing.T) {
	treeOps := tree.NewTreeWithOperations(nil)
	server := NewServer(ServerConfig{}, treeOps, nil, nil, "test-secret", nil, "")
	token := generateTestToken([]byte("test-secret"), "user1")

	body, _ := json.Marshal(CreateTagRequest{Path: ""})
	req := httptest.NewRequest("POST", "/api/v1/tags/", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	server.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestCreateTagBadJSON(t *testing.T) {
	treeOps := tree.NewTreeWithOperations(nil)
	server := NewServer(ServerConfig{}, treeOps, nil, nil, "test-secret", nil, "")
	token := generateTestToken([]byte("test-secret"), "user1")

	req := httptest.NewRequest("POST", "/api/v1/tags/", bytes.NewReader([]byte("{invalid")))
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	server.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

// ── handleGetTag not found ───────────────────────────────────────────────────

func TestGetTagNotFound(t *testing.T) {
	treeOps := tree.NewTreeWithOperations(nil)
	server := NewServer(ServerConfig{}, treeOps, nil, nil, "test-secret", nil, "")
	token := generateTestToken([]byte("test-secret"), "user1")

	req := httptest.NewRequest("GET", "/api/v1/tags/tenant1/nonexistent", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	server.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

// ── OrgSandbox middleware ────────────────────────────────────────────────────

func TestOrgSandbox(t *testing.T) {
	treeOps := tree.NewTreeWithOperations(nil)
	treeOps.CreateNode("tenant1/building", "")
	treeOps.CreateNode("tenant1/other_org/building", "")
	treeOps.CreateNode("other_org/building", "")

	server := NewServer(ServerConfig{}, treeOps, nil, nil, "test-secret", nil, "")
	token := generateTestToken([]byte("test-secret"), "user1") // tenant1

	t.Run("allowed path", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/nodes/tenant1/building", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		server.Router().ServeHTTP(rr, req)

		if rr.Code == http.StatusForbidden {
			t.Error("should be allowed to access own org path")
		}
	})

	t.Run("relative path scoped to token org", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/nodes/other_org/building", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		server.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rr.Code)
		}
	})
}

// ── handleDeleteNode empty path ──────────────────────────────────────────────

func TestDeleteNodeNotFound(t *testing.T) {
	treeOps := tree.NewTreeWithOperations(nil)
	server := NewServer(ServerConfig{}, treeOps, nil, nil, "test-secret", nil, "")
	token := generateTestToken([]byte("test-secret"), "user1")

	req := httptest.NewRequest("DELETE", "/api/v1/nodes/tenant1/nonexistent", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	server.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rr.Code)
	}
}

// ── handleAPIDocs ────────────────────────────────────────────────────────────

func TestAPIDocs(t *testing.T) {
	treeOps := tree.NewTreeWithOperations(nil)
	server := NewServer(ServerConfig{}, treeOps, nil, nil, "test-secret", nil, "")

	req := httptest.NewRequest("GET", "/api-docs", nil)
	rr := httptest.NewRecorder()
	server.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	var docs map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &docs); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if docs["title"] != "XACT REST API" {
		t.Errorf("title = %v", docs["title"])
	}
}

// ── GetClaimsFromContext ─────────────────────────────────────────────────────

func TestGetClaimsFromContextNoClaims(t *testing.T) {
	_, ok := GetClaimsFromContext(context.Background())
	if ok {
		t.Error("expected false for empty context")
	}
}

// ── JSONContentType middleware ────────────────────────────────────────────────

func TestJSONContentType(t *testing.T) {
	handler := JSONContentType(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ct := w.Header().Get("Content-Type")
		if ct != "application/json" {
			t.Errorf("Content-Type = %q", ct)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

// ── buildTagSharedJSON ───────────────────────────────────────────────────────

func TestBuildTagSharedJSON(t *testing.T) {
	leaf := tree.NewFloatLeaf("temp", tree.TagConfig{Name: "temp", Type: tree.TypeFloat})
	shared := leaf.GetShared()
	shared.Units = "°C"
	shared.Deadband = 0.5
	shared.Description = "Temperature sensor"
	leaf.SetShared(shared)

	result := buildTagSharedJSON(leaf)
	if result.Units != "°C" {
		t.Errorf("units = %q", result.Units)
	}
	if result.Description != "Temperature sensor" {
		t.Errorf("description = %q", result.Description)
	}
	if result.Deadband != 0.5 {
		t.Errorf("deadband = %v", result.Deadband)
	}
}

// ── GetNode with isArray ─────────────────────────────────────────────────────

func TestGetNodeIsArray(t *testing.T) {
	treeOps := tree.NewTreeWithOperations(nil)
	treeOps.CreateNode("tenant1/batteries", "")
	node, _ := treeOps.FindNode("tenant1/batteries")
	node.SetIsArray(true)

	server := NewServer(ServerConfig{}, treeOps, nil, nil, "test-secret", nil, "")
	token := generateTestToken([]byte("test-secret"), "user1")

	req := httptest.NewRequest("GET", "/api/v1/nodes/tenant1/batteries", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	server.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp NodeResponse
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if !resp.IsArray {
		t.Error("expected isArray=true")
	}
}

// ── GetTag full response ─────────────────────────────────────────────────────

func TestGetTagFullResponse(t *testing.T) {
	treeOps := tree.NewTreeWithOperations(nil)
	treeOps.CreateNode("tenant1/building", "")
	treeOps.UnlockNode("tenant1/building")
	treeOps.CreateTag("tenant1/building/sensor", tree.TypeString, tree.TagConfig{Name: "sensor"})

	leaf, _ := treeOps.FindLeaf("tenant1/building/sensor")
	leaf.SetString("active")

	server := NewServer(ServerConfig{}, treeOps, nil, nil, "test-secret", nil, "")
	token := generateTestToken([]byte("test-secret"), "user1")

	req := httptest.NewRequest("GET", "/api/v1/tags/tenant1/building/sensor", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	server.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp TagResponse
	json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Value != "active" {
		t.Errorf("value = %v", resp.Value)
	}
	if resp.ValueType != "string" {
		t.Errorf("value_type = %q", resp.ValueType)
	}
	if resp.Name != "sensor" {
		t.Errorf("name = %q", resp.Name)
	}
}
