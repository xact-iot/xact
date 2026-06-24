package lta_incidents_driver

import (
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

	client := NewXACTClient(server.URL, " configured-key ", server.Client())
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

func TestXACTClientRequiresConfiguredAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("missing API key should not call XACT, got %s", r.URL.Path)
	}))
	defer server.Close()

	client := NewXACTClient(server.URL, "", server.Client())
	err := client.IngestIncident(DefaultTenant, DefaultZone, TrafficIncident{
		Type:      "Accident",
		Latitude:  1.27250896,
		Longitude: 103.84792602,
		Message:   "Accident",
	})
	if err == nil || !strings.Contains(err.Error(), "XACT_API_KEY") {
		t.Fatalf("IngestIncident() error = %v, want XACT_API_KEY required", err)
	}
}

func TestXACTClientDeletesKnownStaleIncidentWithAPIKey(t *testing.T) {
	var deletes []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			if !strings.HasPrefix(r.URL.Path, "/api/v1/ingest/default/zone/Singapore/Incidents/") {
				t.Fatalf("unexpected POST path %s", r.URL.Path)
			}
			if got := r.Header.Get("Authorization"); got != "ApiKey configured-key" {
				t.Fatalf("POST Authorization = %q, want configured API key", got)
			}
			w.WriteHeader(http.StatusNoContent)
		case http.MethodDelete:
			deletes = append(deletes, r.URL.Path)
			if got := r.Header.Get("Authorization"); got != "ApiKey configured-key" {
				t.Fatalf("DELETE Authorization = %q, want configured API key", got)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	}))
	defer server.Close()

	client := NewXACTClient(server.URL, "configured-key", server.Client())
	incident := TrafficIncident{
		Type:      "Accident",
		Latitude:  1.27250896,
		Longitude: 103.84792602,
		Message:   "Accident",
	}
	if err := client.IngestIncident(DefaultTenant, DefaultZone, incident); err != nil {
		t.Fatalf("IngestIncident() error = %v", err)
	}
	existing, err := client.ExistingIncidents(DefaultTenant, DefaultZone)
	if err != nil {
		t.Fatalf("ExistingIncidents() error = %v", err)
	}
	if existing[incident.CoordinateKey()] != incident.DeviceName() {
		t.Fatalf("existing = %#v, want incident cached", existing)
	}
	if err := client.DeleteIncidentDevice(DefaultTenant, DefaultZone, incident.DeviceName()); err != nil {
		t.Fatalf("DeleteIncidentDevice() error = %v", err)
	}
	if len(deletes) != 1 || deletes[0] != "/api/v1/ingest/default/zone/Singapore/Incidents/"+incident.DeviceName() {
		t.Fatalf("deletes = %#v", deletes)
	}
	existing, err = client.ExistingIncidents(DefaultTenant, DefaultZone)
	if err != nil {
		t.Fatalf("ExistingIncidents() after delete error = %v", err)
	}
	if len(existing) != 0 {
		t.Fatalf("existing after delete = %#v, want empty", existing)
	}
}
