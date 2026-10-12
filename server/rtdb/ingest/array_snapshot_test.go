package ingest

import (
	"encoding/json"
	"github.com/xact-iot/xact/rtdb/tree"
	"testing"
)

type arrayCapturePublisher struct {
	subjects []string
	data     [][]byte
}

func (p *arrayCapturePublisher) TagValuePublish(subject string, data []byte) error {
	p.subjects = append(p.subjects, subject)
	p.data = append(p.data, append([]byte(nil), data...))
	return nil
}

func TestArraySnapshotPublishesOnlyAfterAllElementsAreWritten(t *testing.T) {
	ops := setupTree(t)
	proc := NewProcessor(ops)
	previous := tree.TagValuePublisher
	capture := &arrayCapturePublisher{}
	tree.TagValuePublisher = capture
	defer func() { tree.TagValuePublisher = previous }()
	for _, raw := range []string{`{"route":{"coordinates":[49.283456,-123.114567,49.294567,-123.125678,49.30,-123.14]}}`, `{"route":{"coordinates":[49.303456,-123.134567,49.314567,-123.145678]}}`} {
		start := len(capture.subjects)
		data, err := ParsePayload([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		if err := proc.WriteDeviceData("TestOrg", "", "VMS", "Dev1", data); err != nil {
			t.Fatal(err)
		}
		var beginning map[string]tree.TagValue
		if err := json.Unmarshal(capture.data[start], &beginning); err != nil {
			t.Fatal(err)
		}
		if capture.subjects[start] != "tagvalue.TestOrg.VMS.Dev1.route.coordinates" || beginning["coordinates"].Type != "array-start" {
			t.Fatalf("array batch did not start before element updates: %s", capture.data[start])
		}
		end := len(capture.subjects) - 1
		if capture.subjects[end] != "tagvalue.TestOrg.VMS.Dev1.route.coordinates" {
			t.Fatalf("final event = %s", capture.subjects[end])
		}
		var msg map[string]tree.TagValue
		if err := json.Unmarshal(capture.data[end], &msg); err != nil {
			t.Fatal(err)
		}
		var request struct {
			Route struct {
				Coordinates []any `json:"coordinates"`
			} `json:"route"`
		}
		if err := json.Unmarshal([]byte(raw), &request); err != nil {
			t.Fatal(err)
		}
		value := msg["route.coordinates"].Value.([]any)
		if len(value) != len(request.Route.Coordinates) {
			t.Fatalf("snapshot length = %d", len(value))
		}
		for i, want := range request.Route.Coordinates {
			if value[i] != want {
				t.Fatalf("snapshot[%d]=%v want %v", i, value[i], want)
			}
		}
	}
}
