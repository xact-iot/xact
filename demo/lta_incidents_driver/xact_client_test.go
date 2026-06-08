package lta_incidents_driver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestXACTClientUsesConfiguredAPIKey(t *testing.T) {
	var sawIngest bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			t.Fatal("configured API key should avoid login")
		}
		if !strings.HasPrefix(r.URL.Path, "/api/v1/ingest/default/zone/Singapore/Incidents/") {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		sawIngest = true
		if got := r.Header.Get("Authorization"); got != "ApiKey configured-key" {
			t.Fatalf("Authorization = %q, want configured API key", got)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewXACTClient(server.URL, " configured-key ", "admin", "admin", server.Client())
	err := client.IngestIncident(DefaultTenant, DefaultZone, TrafficIncident{
		Type:      "Vehicle breakdown",
		Latitude:  1.27250896,
		Longitude: 103.84792602,
		Message:   "Vehicle breakdown",
	})
	if err != nil {
		t.Fatalf("IngestIncident() error = %v", err)
	}
	if !sawIngest {
		t.Fatal("ingest request was not sent")
	}
}

func TestXACTClientCreatesNewAPIKeyInsteadOfReusingMaskedListValue(t *testing.T) {
	var createdKey bool
	var sawIngest bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"token": "jwt",
				"user":  map[string]string{"tenant_id": DefaultTenant},
			})
		case "/api/v1/api-keys":
			if r.Method == http.MethodGet {
				t.Fatal("client must not reuse masked API keys from list response")
			}
			if r.Method != http.MethodPost {
				t.Fatalf("api key method = %s, want POST", r.Method)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer jwt" {
				t.Fatalf("create Authorization = %q, want bearer token", got)
			}
			createdKey = true
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(apiKeyResponse{
				ID:      1,
				OrgName: DefaultTenant,
				Name:    incidentAPIKeyName,
				Key:     "raw-created-key",
			})
		default:
			if !strings.HasPrefix(r.URL.Path, "/api/v1/ingest/default/zone/Singapore/Incidents/") {
				t.Fatalf("unexpected path %s", r.URL.Path)
			}
			sawIngest = true
			if got := r.Header.Get("Authorization"); got != "ApiKey raw-created-key" {
				t.Fatalf("ingest Authorization = %q, want created raw key", got)
			}
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()

	client := NewXACTClient(server.URL, "", "admin", "admin", server.Client())
	err := client.IngestIncident(DefaultTenant, DefaultZone, TrafficIncident{
		Type:      "Accident",
		Latitude:  1.27250896,
		Longitude: 103.84792602,
		Message:   "Accident",
	})
	if err != nil {
		t.Fatalf("IngestIncident() error = %v", err)
	}
	if !createdKey {
		t.Fatal("API key was not created")
	}
	if !sawIngest {
		t.Fatal("ingest request was not sent")
	}
}
