package water_driver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientRefreshesStaleAPIKeyOnUnauthorized(t *testing.T) {
	var createdKeys int
	var ingestKeys []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			if r.Method != http.MethodPost {
				t.Fatalf("login method = %s", r.Method)
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"token": "jwt"})
		case "/api/v1/api-keys":
			if r.Method != http.MethodPost {
				t.Fatalf("api key method = %s", r.Method)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer jwt" {
				t.Fatalf("api key Authorization = %q", got)
			}
			createdKeys++
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]string{"key": "fresh-key"})
		case "/api/v1/ingest/default/zone/WaterWorks/DAM/UPPER_DAM":
			ingestKeys = append(ingestKeys, r.Header.Get("Authorization"))
			if len(ingestKeys) == 1 {
				http.Error(w, "invalid or missing API key", http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "stale-key")
	if err := client.Post("DAM", "UPPER_DAM", map[string]any{"meta": map[string]any{"online": true}}); err != nil {
		t.Fatalf("Post returned error: %v", err)
	}

	if createdKeys != 1 {
		t.Fatalf("createdKeys = %d, want 1", createdKeys)
	}
	if len(ingestKeys) != 2 {
		t.Fatalf("ingest attempts = %d, want 2", len(ingestKeys))
	}
	if ingestKeys[0] != "ApiKey stale-key" {
		t.Fatalf("first ingest key = %q", ingestKeys[0])
	}
	if ingestKeys[1] != "ApiKey fresh-key" {
		t.Fatalf("second ingest key = %q", ingestKeys[1])
	}
}
