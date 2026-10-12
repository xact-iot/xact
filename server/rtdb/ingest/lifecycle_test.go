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

func TestStableVehiclePathRejectsOldSessionsAndDelayedDeletes(t *testing.T) {
	ops := setupTree(t)
	store := &lifecycleMemoryStore{data: map[string]json.RawMessage{}}
	p := NewProcessor(ops)
	p.SetLifecycleStore(store)
	event := func(session string) IngestEvent {
		return IngestEvent{Tenant: "TestOrg", DeviceType: "PUBLIC_BUS.BUSES", DeviceName: "81207", Session: session, TagData: TagData{Groups: map[string]map[string]any{"meta": {"lat": 49.28, "lon": -123.12}}}}
	}
	first := event("one")
	if err := p.ProcessEvent(first); err != nil {
		t.Fatal(err)
	}
	next := event("two")
	if err := p.ProcessEvent(next); err != nil {
		t.Fatal(err)
	}
	if err := p.ProcessEvent(first); err == nil {
		t.Fatal("superseded session overwrote the new trip")
	}
	first.Operation = "delete"
	if err := p.ProcessEvent(first); err != nil {
		t.Fatal(err)
	}
	path := "TestOrg.PUBLIC_BUS.BUSES.81207"
	if got := p.deviceSession(path); got != "two" {
		t.Fatalf("delayed delete removed new trip: %q", got)
	}
	p = NewProcessor(ops)
	p.SetLifecycleStore(store)
	if err := p.ReconcileRetiredDevices(); err != nil {
		t.Fatal(err)
	}
	if got := p.deviceSession(path); got != "two" {
		t.Fatalf("active vehicle removed on restart: %q", got)
	}
	first.Operation = ""
	if err := p.ProcessEvent(first); err == nil {
		t.Fatal("old session accepted after receiver restart")
	}
	next.Operation = "delete"
	if err := p.ProcessEvent(next); err != nil {
		t.Fatal(err)
	}
	if _, err := ops.FindNode(path); err == nil {
		t.Fatal("current trip not retired")
	}
	third := event("three")
	if err := p.ProcessEvent(third); err != nil {
		t.Fatal(err)
	}
	next.Operation = ""
	if err := p.ProcessEvent(next); err == nil {
		t.Fatal("second retired session recreated device")
	}
	if err := p.ProcessEvent(first); err == nil {
		t.Fatal("first retired session lost its fence")
	}
	// Restore an outdated tree while the lifecycle record identifies session three.
	if err := ops.DeleteNode(path); err != nil {
		t.Fatal(err)
	}
	restored := NewProcessor(ops)
	if err := restored.ProcessEvent(first); err != nil {
		t.Fatal(err)
	}
	p = NewProcessor(ops)
	p.SetLifecycleStore(store)
	if err := p.ReconcileRetiredDevices(); err != nil {
		t.Fatal(err)
	}
	if _, err := ops.FindNode(path); err == nil {
		t.Fatal("old tree session survived restart reconciliation")
	}
	if err := p.ProcessEvent(third); err != nil {
		t.Fatalf("current trip could not republish: %v", err)
	}
}

func TestSessionTransitionPersistenceFailureLeavesCurrentTripUsable(t *testing.T) {
	ops := setupTree(t)
	store := &lifecycleMemoryStore{data: map[string]json.RawMessage{}}
	p := NewProcessor(ops)
	p.SetLifecycleStore(store)
	first := IngestEvent{Tenant: "TestOrg", DeviceType: "PUBLIC_BUS.BUSES", DeviceName: "81207", Session: "one", TagData: TagData{Groups: map[string]map[string]any{"meta": {"online": true}}}}
	if err := p.ProcessEvent(first); err != nil {
		t.Fatal(err)
	}
	next := first
	next.Session = "two"
	store.fail = true
	if err := p.ProcessEvent(next); err == nil {
		t.Fatal("transition acknowledged before durable state")
	}
	if got := p.deviceSession("TestOrg.PUBLIC_BUS.BUSES.81207"); got != "one" {
		t.Fatalf("failed transition changed session: %q", got)
	}
	if err := p.ProcessEvent(first); err != nil {
		t.Fatalf("failed transition retired current session: %v", err)
	}
	store.fail = false
	if err := p.ProcessEvent(next); err != nil {
		t.Fatal(err)
	}
	first.Operation = "delete"
	if err := p.ProcessEvent(first); err != nil {
		t.Fatal(err)
	}
	if got := p.deviceSession("TestOrg.PUBLIC_BUS.BUSES.81207"); got != "two" {
		t.Fatalf("old delete affected new session: %q", got)
	}
}
