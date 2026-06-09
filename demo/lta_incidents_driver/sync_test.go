package lta_incidents_driver

import (
	"context"
	"fmt"
	"reflect"
	"testing"
)

type fakeSource struct {
	incidents []TrafficIncident
	err       error
}

func (f fakeSource) FetchTrafficIncidents() ([]TrafficIncident, error) {
	return f.incidents, f.err
}

type fakeSink struct {
	existing    map[string]string
	existingErr error
	ingests     []TrafficIncident
	resolved    map[string]TrafficIncident
	deletes     []string
	events      []string
}

func (f *fakeSink) ExistingIncidents(string, string) (map[string]string, error) {
	if f.existingErr != nil {
		return nil, f.existingErr
	}
	return f.existing, nil
}

func (f *fakeSink) IngestIncident(_ string, _ string, incident TrafficIncident) error {
	f.ingests = append(f.ingests, incident)
	return nil
}

func (f *fakeSink) DeleteIncidentDevice(_, _, deviceName string) error {
	f.deletes = append(f.deletes, deviceName)
	return nil
}

func (f *fakeSink) GetIncident(_, _, deviceName string) (TrafficIncident, error) {
	for _, inc := range f.ingests {
		if inc.DeviceName() == deviceName {
			return inc, nil
		}
	}
	if inc, ok := f.resolved[deviceName]; ok {
		return inc, nil
	}
	return TrafficIncident{}, fmt.Errorf("incident %s not found", deviceName)
}

func (f *fakeSink) CreateIncidentEvent(_, _, _, message string, _ map[string]any) error {
	f.events = append(f.events, message)
	return nil
}

func TestPollOnceCreatesUpdatesAndRemovesByCoordinate(t *testing.T) {
	active := TrafficIncident{
		Type:      "Vehicle breakdown",
		Latitude:  1.30398068448214,
		Longitude: 103.919182834377,
		Message:   "Vehicle breakdown on ECP",
	}
	staleKey := coordinateKey(1.300000001, 103.900000001)
	staleIncident := TrafficIncident{
		Type:      "Vehicle breakdown",
		Latitude:  1.300000001,
		Longitude: 103.900000001,
		Message:   "Old vehicle breakdown near that spot",
	}
	staleDeviceName := "INC_STALE"
	sink := &fakeSink{
		existing: map[string]string{
			staleKey: staleDeviceName,
		},
		resolved: map[string]TrafficIncident{
			staleDeviceName: staleIncident,
		},
	}

	driver := NewDriver(DefaultTenant, DefaultZone, fakeSource{incidents: []TrafficIncident{active}}, sink)
	if err := driver.PollOnce(context.Background()); err != nil {
		t.Fatalf("PollOnce() error = %v", err)
	}

	if len(sink.ingests) != 1 || sink.ingests[0] != active {
		t.Fatalf("ingests = %#v, want active incident", sink.ingests)
	}
	if !reflect.DeepEqual(sink.deletes, []string{"INC_STALE"}) {
		t.Fatalf("deletes = %#v, want stale device deletion", sink.deletes)
	}
	wantEvents := []string{"LTA incident created: Vehicle breakdown", "LTA incident resolved: Vehicle breakdown"}
	if !reflect.DeepEqual(sink.events, wantEvents) {
		t.Fatalf("events = %#v, want %#v", sink.events, wantEvents)
	}
}

func TestPollOnceContinuesInIngestOnlyModeWhenLoginUnauthorized(t *testing.T) {
	active := TrafficIncident{
		Type:      "Accident",
		Latitude:  1.30398068448214,
		Longitude: 103.919182834377,
		Message:   "Accident on expressway",
	}
	sink := &fakeSink{
		existingErr: ErrXACTLoginUnauthorized,
		existing: map[string]string{
			coordinateKey(1.300000001, 103.900000001): "INC_STALE",
		},
	}

	driver := NewDriver(DefaultTenant, DefaultZone, fakeSource{incidents: []TrafficIncident{active}}, sink)
	if err := driver.PollOnce(context.Background()); err != nil {
		t.Fatalf("PollOnce() error = %v", err)
	}

	if len(sink.ingests) != 1 || sink.ingests[0] != active {
		t.Fatalf("ingests = %#v, want active incident", sink.ingests)
	}
	if len(sink.events) != 0 {
		t.Fatalf("events = %#v, want none in ingest-only mode", sink.events)
	}
	if len(sink.deletes) != 0 {
		t.Fatalf("deletes = %#v, want none in ingest-only mode", sink.deletes)
	}
}

func TestMapExistingIncidentsUsesMetaLatLon(t *testing.T) {
	node := nodeResponse{Children: []childInfo{
		{
			Name: "INC_EXISTING",
			Type: "node",
			Children: []childInfo{
				{
					Name: "meta",
					Type: "node",
					Children: []childInfo{
						{Name: "lat", Type: "leaf", Value: 1.30398068448214},
						{Name: "lon", Type: "leaf", Value: 103.919182834377},
					},
				},
			},
		},
	}}

	got := mapExistingIncidents(node)
	want := map[string]string{
		coordinateKey(1.30398068448214, 103.919182834377): "INC_EXISTING",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mapExistingIncidents() = %#v, want %#v", got, want)
	}
}

func TestDeviceNameForCoordinatesIsStableAndShort(t *testing.T) {
	a := deviceNameForCoordinates(1.30398068448214, 103.919182834377)
	b := deviceNameForCoordinates(1.30398068448214, 103.919182834377)
	if a != b {
		t.Fatalf("device name not stable: %s != %s", a, b)
	}
	if len(a) > 12 {
		t.Fatalf("device name %q is too long", a)
	}
}
