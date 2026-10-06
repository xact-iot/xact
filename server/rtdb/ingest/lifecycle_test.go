package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

type lifecycleMemoryStore struct {
	mu   sync.Mutex
	data map[string]json.RawMessage
	fail bool
}

func (s *lifecycleMemoryStore) SaveConfig(_ context.Context, org, key string, raw json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return errors.New("store unavailable")
	}
	s.data[org+"/"+key] = raw
	return nil
}
func (s *lifecycleMemoryStore) LoadConfig(_ context.Context, org, key string) (json.RawMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data[org+"/"+key], nil
}
func TestRetiredDeviceCannotBeRecreatedAfterReceiverRestart(t *testing.T) {
	ops := setupTree(t)
	store := &lifecycleMemoryStore{data: map[string]json.RawMessage{}}
	p := NewProcessor(ops)
	p.SetLifecycleStore(store)
	evt := IngestEvent{Tenant: "TestOrg", DeviceType: "PUBLIC_BUS.BUSES", DeviceName: "bus_session1", Session: "one", TagData: TagData{Groups: map[string]map[string]any{"meta": {"lat": 49.28, "lon": -123.12}}}}
	if err := p.ProcessEvent(evt); err != nil {
		t.Fatal(err)
	}
	evt.Operation = "delete"
	if err := p.ProcessEvent(evt); err != nil {
		t.Fatal(err)
	}
	if _, err := ops.FindNode("TestOrg.PUBLIC_BUS.BUSES.bus_session1"); err == nil {
		t.Fatal("device not deleted")
	}
	if err := p.ProcessEvent(evt); err != nil {
		t.Fatalf("repeat delete: %v", err)
	}
	// Simulate restoring a tree snapshot from before deletion.
	rawProcessor := NewProcessor(ops)
	evt.Operation = ""
	if err := rawProcessor.ProcessEvent(evt); err != nil {
		t.Fatal(err)
	}
	p = NewProcessor(ops)
	p.SetLifecycleStore(store)
	if err := p.ReconcileRetiredDevices(); err != nil {
		t.Fatal(err)
	}
	if _, err := ops.FindNode("TestOrg.PUBLIC_BUS.BUSES.bus_session1"); err == nil {
		t.Fatal("retired device survived old tree restore")
	}
	evt.Operation = ""
	evt.Session = ""
	if err := p.ProcessEvent(evt); err == nil {
		t.Fatal("late unversioned ingest recreated retired session")
	}
	evt.DeviceName = "bus_session2"
	evt.Session = "two"
	if err := p.ProcessEvent(evt); err != nil {
		t.Fatalf("next session: %v", err)
	}
}
func TestRetirementPersistenceFailureDoesNotDeleteDevice(t *testing.T) {
	ops := setupTree(t)
	p := NewProcessor(ops)
	evt := IngestEvent{Tenant: "TestOrg", DeviceType: "BUS", DeviceName: "one", TagData: TagData{DirectTags: map[string]any{"lat": 1.0}}}
	if err := p.ProcessEvent(evt); err != nil {
		t.Fatal(err)
	}
	p.SetLifecycleStore(&lifecycleMemoryStore{fail: true})
	evt.Operation = "delete"
	if err := p.ProcessEvent(evt); err == nil {
		t.Fatal("failed persistence acknowledged")
	}
	if _, err := ops.FindNode("TestOrg.BUS.one"); err != nil {
		t.Fatal("device removed before durable retirement")
	}
}
func TestDeviceRequestMustMatchAuthorizedSubject(t *testing.T) {
	evt := IngestEvent{Tenant: "alpha", Zone: "north", DeviceType: "PUBLIC_BUS.BUSES", DeviceName: "one", Operation: "delete"}
	good := IngestSubjectFor(DeleteRequestSubject, evt.Tenant, evt.Zone, evt.DeviceType, evt.DeviceName)
	if err := validateIngestRequest(good, evt); err != nil {
		t.Fatal(err)
	}
	for _, subject := range []string{IngestSubjectFor(IngestRequestSubject, evt.Tenant, evt.Zone, evt.DeviceType, evt.DeviceName), IngestSubjectFor(DeleteRequestSubject, "beta", evt.Zone, evt.DeviceType, evt.DeviceName), good + ".child"} {
		if err := validateIngestRequest(subject, evt); err == nil {
			t.Fatalf("subject accepted: %s", subject)
		}
	}
	evt.DeviceName = "*"
	if err := validateIngestRequest(good, evt); err == nil {
		t.Fatal("wildcard identity accepted")
	}
}
func TestDeleteAcknowledgementWaitsForProcessing(t *testing.T) {
	server := newTestNATSServer(t)
	defer server.shutdown()
	started := make(chan struct{})
	release := make(chan struct{})
	sub, err := SubscribeIngest(server.nc, func(evt IngestEvent) error { close(started); <-release; return errors.New("delete failed") })
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Unsubscribe()
	server.nc.Flush()
	evt := IngestEvent{Tenant: "alpha", DeviceType: "PUBLIC_BUS.BUSES", DeviceName: "one", Operation: "delete"}
	raw, _ := json.Marshal(evt)
	response := make(chan IngestResponse, 1)
	go func() {
		msg, err := server.nc.Request(IngestSubjectFor(DeleteRequestSubject, evt.Tenant, "", evt.DeviceType, evt.DeviceName), raw, 2*time.Second)
		if err != nil {
			response <- IngestResponse{Error: err.Error()}
			return
		}
		var r IngestResponse
		json.Unmarshal(msg.Data, &r)
		response <- r
	}()
	<-started
	select {
	case r := <-response:
		t.Fatalf("ack before processing: %#v", r)
	default:
	}
	close(release)
	r := <-response
	if r.Status != "error" || r.Error != "delete failed" {
		t.Fatalf("processing failure not propagated: %#v", r)
	}
}
