package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xact-iot/xact/rtdb/tree"
)

func TestNodeProjectionPreservesArraySnapshotAndElementAccess(t *testing.T) {
	ops := tree.NewTreeWithOperations(nil)
	for _, path := range []string{"tenant1/Routes/A/meta/name", "tenant1/Routes/A/shape/points/0", "tenant1/Routes/A/shape/points/1", "tenant1/Routes/A/trips/unused"} {
		if err := ops.CreateTag(path, tree.TypeFloat, tree.TagConfig{}); err != nil {
			t.Fatal(err)
		}
	}
	array, _ := ops.FindNode("tenant1/Routes/A/shape/points")
	array.SetIsArray(true)
	ops.SetLeafValue("tenant1/Routes/A/shape/points/0", 49.283456)
	ops.SetLeafValue("tenant1/Routes/A/shape/points/1", -123.114567)
	server := NewServer(ServerConfig{}, ops, nil, nil, "test-secret", nil, "")
	token := generateTestToken([]byte("test-secret"), "user1")
	get := func(url string) NodeResponse {
		t.Helper()
		request := httptest.NewRequest("GET", url, nil)
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		server.Router().ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s: %d %s", url, response.Code, response.Body)
		}
		var result NodeResponse
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	result := get("/api/v1/nodes/tenant1/Routes?select=*.shape.points&select=*.meta.name")
	if len(result.Children) != 1 || len(result.Children[0].Children) != 2 {
		t.Fatalf("unrequested groups in projection: %+v", result)
	}
	var points ChildInfo
	for _, group := range result.Children[0].Children {
		if group.Name == "shape" {
			points = group.Children[0]
		}
	}
	values, ok := points.Value.([]any)
	if !ok || len(values) != 2 || values[0] != 49.283456 || values[1] != -123.114567 || len(points.Children) != 0 {
		t.Fatalf("array snapshot: %+v", points)
	}
	full := get("/api/v1/nodes/tenant1/Routes/A/shape/points?depth=0")
	if len(full.Children) != 2 {
		t.Fatalf("scalar array elements no longer accessible: %+v", full)
	}
}

func TestNodeSearchBoundsResultsAndKeepsAncestors(t *testing.T) {
	ops := tree.NewTreeWithOperations(nil)
	for _, name := range []string{"temp1", "temp2", "other"} {
		ops.CreateTag("tenant1/Device/meta/"+name, tree.TypeFloat, tree.TagConfig{})
	}
	server := NewServer(ServerConfig{}, ops, nil, nil, "test-secret", nil, "")
	request := httptest.NewRequest("GET", "/api/v1/nodes/tenant1?search=temp&limit=1", nil)
	request.Header.Set("Authorization", "Bearer "+generateTestToken([]byte("test-secret"), "user1"))
	response := httptest.NewRecorder()
	server.Router().ServeHTTP(response, request)
	var result NodeResponse
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if response.Code != 200 || !result.Truncated || len(result.Children) != 1 || len(result.Children[0].Children[0].Children) != 1 {
		t.Fatalf("bounded search: %d %+v", response.Code, result)
	}
}
