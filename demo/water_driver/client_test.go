package water_driver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientUsesConfiguredAPIKey(t *testing.T) {
	var ingestKey string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/ingest/default/zone/WaterWorks/DAM/UPPER_DAM":
			ingestKey = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "configured-key")
	if err := client.Post("DAM", "UPPER_DAM", map[string]any{"meta": map[string]any{"online": true}}); err != nil {
		t.Fatalf("Post returned error: %v", err)
	}

	if ingestKey != "ApiKey configured-key" {
		t.Fatalf("ingest key = %q", ingestKey)
	}
}

func TestClientRequiresConfiguredAPIKey(t *testing.T) {
	client := NewClient("http://example.invalid/xact", "")
	err := client.Post("DAM", "UPPER_DAM", map[string]any{"meta": map[string]any{"online": true}})
	if err == nil || !strings.Contains(err.Error(), "XACT_API_KEY") {
		t.Fatalf("Post error = %v, want XACT_API_KEY required", err)
	}
}
