package nats

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	natsgo "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/xact-iot/xact/rtdb/tree"
)

type countingTagJetStream struct {
	jetstream.JetStream
	mu    sync.Mutex
	calls int
	fail  bool
}

func (js *countingTagJetStream) Publish(ctx context.Context, subject string, payload []byte, opts ...jetstream.PublishOpt) (*jetstream.PubAck, error) {
	js.mu.Lock()
	js.calls++
	fail := js.fail
	js.mu.Unlock()
	if fail {
		return nil, errors.New("test publish failure")
	}
	return js.JetStream.Publish(ctx, subject, payload, opts...)
}

func testTagBatches(t *testing.T) (*BroadcastStream, *testEmbeddedServer, *countingTagJetStream) {
	t.Helper()
	s := requireEmbeddedServer(t)
	t.Cleanup(s.Shutdown)
	js, err := jetstream.New(s.Conn())
	if err != nil {
		t.Fatal(err)
	}
	_, err = js.CreateStream(context.Background(), jetstream.StreamConfig{Name: string(TagValueStream), Subjects: []string{BroadcastStreamPrefix + "tagvalue.>", BroadcastStreamPrefix + "tagbatch.>"}, Storage: jetstream.MemoryStorage, MaxMsgsPerSubject: 1})
	if err != nil {
		t.Fatal(err)
	}
	counted := &countingTagJetStream{JetStream: js}
	return &BroadcastStream{js: counted}, s, counted
}

func retainedBatch(t *testing.T, s *testEmbeddedServer, group string) TagValueBatch {
	t.Helper()
	message, err := s.JetStream().GetLastMsg(string(TagValueStream), tagBatchSubject(group, ""))
	if err != nil {
		t.Fatal(err)
	}
	var batch TagValueBatch
	if err := json.Unmarshal(message.Data, &batch); err != nil {
		t.Fatal(err)
	}
	return batch
}

func TestTagBatchesPublishGroupsAndRetainPartialUpdates(t *testing.T) {
	pub, s, counted := testTagBatches(t)
	finish := pub.BeginTagValueBatch("acme.PUBLIC_BUS.BUSES.Bus1")
	values := map[string]any{"meta.lat": 49.283456, "meta.lon": -123.114567, "meta.online": true, "status.state": "moving", "status.speed": 42, "route.coordinates": []float64{49.283456, -123.114567, 49.29, -123.12}, "route.coordinates.0": 49.283456, "route.coordinates.1": -123.114567, "route.coordinates.2": 49.29, "route.coordinates.3": -123.12}
	for path, value := range values {
		if err := pub.PublishTagValue("tagvalue.acme.PUBLIC_BUS.BUSES.Bus1."+path, tree.TagValue{Type: "value", Value: value}); err != nil {
			t.Fatal(err)
		}
	}
	if counted.calls != 0 {
		t.Fatal("published before snapshot completion")
	}
	if err := finish(); err != nil {
		t.Fatal(err)
	}
	if counted.calls != 4 {
		t.Fatalf("got %d publications, want four groups for eleven values plus array markers", counted.calls)
	}
	finish = pub.BeginTagValueBatch("acme.PUBLIC_BUS.BUSES.Bus1")
	if err := pub.PublishTagValue("tagvalue.acme.PUBLIC_BUS.BUSES.Bus1.meta.lat", tree.TagValue{Type: "value", Value: 49.303456}); err != nil {
		t.Fatal(err)
	}
	if err := finish(); err != nil {
		t.Fatal(err)
	}
	if counted.calls != 5 {
		t.Fatalf("partial update issued %d publications", counted.calls-4)
	}
	group := "acme.PUBLIC_BUS.BUSES.Bus1.meta"
	batch := retainedBatch(t, s, group)
	if len(batch.Values) != 3 || batch.Values[group+".lon"].Value != -123.114567 || batch.Values[group+".lat"].Value != 49.303456 {
		t.Fatalf("incomplete replay snapshot: %#v", batch)
	}
	if len(batch.Changed) != 1 || batch.Changed[0] != group+".lat" {
		t.Fatalf("changed tags = %v", batch.Changed)
	}
	info, err := s.JetStream().StreamInfo(string(TagValueStream))
	if err != nil {
		t.Fatal(err)
	}
	if info.State.Msgs != 4 {
		t.Fatalf("retained %d messages, want four", info.State.Msgs)
	}
	array := retainedBatch(t, s, "acme.PUBLIC_BUS.BUSES.Bus1.route")
	if len(array.Values["acme.PUBLIC_BUS.BUSES.Bus1.route.coordinates"].Value.([]any)) != 4 {
		t.Fatal("missing complete array")
	}
}

func TestFailedBatchIsRetriedWithoutAnotherChangedValue(t *testing.T) {
	pub, s, counted := testTagBatches(t)
	finish := pub.BeginTagValueBatch("acme.BUS.One")
	for path, value := range map[string]any{"lat": 49.2, "online": true} {
		if err := pub.PublishTagValue("tagvalue.acme.BUS.One.meta."+path, tree.TagValue{Type: "value", Value: value}); err != nil {
			t.Fatal(err)
		}
	}
	counted.fail = true
	if err := finish(); err == nil {
		t.Fatal("failed publication was acknowledged")
	}
	counted.fail = false
	finish = pub.BeginTagValueBatch("acme.BUS.One")
	if err := finish(); err != nil {
		t.Fatal(err)
	}
	batch := retainedBatch(t, s, "acme.BUS.One.meta")
	if len(batch.Values) != 2 || len(batch.Changed) != 2 {
		t.Fatalf("retry lost values: %#v", batch)
	}
	if counted.calls != 2 {
		t.Fatalf("publish attempts %d", counted.calls)
	}
}

func TestTagBatchesForgetDeletedDeviceBeforeReuse(t *testing.T) {
	pub, s, _ := testTagBatches(t)
	finish := pub.BeginTagValueBatch("acme.BUS.One")
	for _, name := range []string{"session", "lat", "old_only"} {
		if err := pub.PublishTagValue("tagvalue.acme.BUS.One.meta."+name, tree.TagValue{Type: "value", Value: "old"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := finish(); err != nil {
		t.Fatal(err)
	}
	if err := pub.ForgetTagValues(".acme.BUS.One"); err != nil {
		t.Fatal(err)
	}
	info, err := s.JetStream().StreamInfo(string(TagValueStream))
	if err != nil {
		t.Fatal(err)
	}
	if info.State.Msgs != 0 || len(pub.tags.scopes) != 0 || len(pub.tags.groups) != 0 {
		t.Fatalf("deleted device retained state: %#v", info.State)
	}
	finish = pub.BeginTagValueBatch("acme.BUS.One")
	if err := pub.PublishTagValue("tagvalue.acme.BUS.One.meta.session", tree.TagValue{Type: "value", Value: "new"}); err != nil {
		t.Fatal(err)
	}
	if err := finish(); err != nil {
		t.Fatal(err)
	}
	batch := retainedBatch(t, s, "acme.BUS.One.meta")
	if len(batch.Values) != 1 {
		t.Fatalf("reused device got old values: %#v", batch)
	}
}

func TestLargeTagGroupsSplitAndRetainAllValues(t *testing.T) {
	pub, s, _ := testTagBatches(t)
	finish := pub.BeginTagValueBatch("acme.Device")
	for i := 0; i < 3000; i++ {
		path := fmt.Sprintf("tagvalue.acme.Device.coordinates.%d", i)
		if err := pub.PublishTagValue(path, tree.TagValue{Type: "value", Value: strings.Repeat("x", 100), Timestamp: 123}); err != nil {
			t.Fatal(err)
		}
		if i == 99 {
			if err := finish(); err != nil {
				t.Fatal(err)
			}
			finish = pub.BeginTagValueBatch("acme.Device")
		}
	}
	if err := finish(); err != nil {
		t.Fatal(err)
	}
	info, err := s.JetStream().StreamInfo(string(TagValueStream))
	if err != nil {
		t.Fatal(err)
	}
	if info.State.Msgs <= 1 || info.State.Msgs >= 3000 {
		t.Fatalf("messages = %d", info.State.Msgs)
	}
	seen := map[string]bool{}
	for seq := info.State.FirstSeq; seq <= info.State.LastSeq; seq++ {
		message, err := s.JetStream().GetMsg(string(TagValueStream), seq)
		if errors.Is(err, natsgo.ErrMsgNotFound) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if len(message.Data) > tagBatchTargetBytes {
			t.Fatalf("oversized partition: %d", len(message.Data))
		}
		var batch TagValueBatch
		if err := json.Unmarshal(message.Data, &batch); err != nil {
			t.Fatal(err)
		}
		for path := range batch.Values {
			if seen[path] {
				t.Fatalf("duplicate retained value %s", path)
			}
			seen[path] = true
		}
	}
	if len(seen) != 3000 {
		t.Fatalf("retained values = %d", len(seen))
	}
	if _, err := s.JetStream().GetLastMsg(string(TagValueStream), tagBatchSubject("acme.Device.coordinates", "")); !errors.Is(err, natsgo.ErrMsgNotFound) {
		t.Fatalf("obsolete parent snapshot remained: %v", err)
	}
	if err := pub.PublishTagValue("tagvalue.acme.Device.coordinates.new", tree.TagValue{Type: "value", Value: 42}); err != nil {
		t.Fatal(err)
	}
}

func TestTagBatchConcurrentDevicesAndImmediateWrites(t *testing.T) {
	pub, s, _ := testTagBatches(t)
	var workers sync.WaitGroup
	for device := 0; device < 8; device++ {
		workers.Add(1)
		go func(device int) {
			defer workers.Done()
			path := fmt.Sprintf("acme.Device%d", device)
			for run := 0; run < 5; run++ {
				finish := pub.BeginTagValueBatch(path)
				for tag := 0; tag < 20; tag++ {
					if err := pub.PublishTagValue(fmt.Sprintf("tagvalue.%s.meta.field%d", path, tag), tree.TagValue{Type: "value", Value: run}); err != nil {
						t.Error(err)
					}
				}
				if err := finish(); err != nil {
					t.Error(err)
				}
			}
		}(device)
	}
	workers.Wait()
	if err := pub.PublishTagValue("tagvalue.acme.Device0.meta.script", tree.TagValue{Type: "value", Value: 99}); err != nil {
		t.Fatal(err)
	}
	batch := retainedBatch(t, s, "acme.Device0.meta")
	if len(batch.Values) != 21 || len(batch.Changed) != 1 {
		t.Fatalf("immediate write snapshot = %#v", batch)
	}
}

func TestDecodeTagBatchDispatchesOnlyChangesInItsGroup(t *testing.T) {
	subject := tagBatchSubject("acme.Bus.meta", "")
	data := []byte(`{"values":{"acme.Bus.meta.lat":{"type":"value","value":49.2},"acme.Bus.meta.online":{"type":"value","value":true}},"changed":["acme.Bus.meta.lat"]}`)
	changes, err := DecodeTagValueChanges(subject, data)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes["acme.Bus.meta.lat"].Value != 49.2 {
		t.Fatalf("changes = %#v", changes)
	}
	for _, path := range []string{"other.Bus.meta.lat", "acme.Bus.status.lat", "acme.Bus.meta.nested.lat", "acme.Bus.meta.absent"} {
		data, _ := json.Marshal(TagValueBatch{Values: map[string]tree.TagValue{path: {Type: "value", Value: 1}}, Changed: []string{path}})
		if path == "acme.Bus.meta.absent" {
			data = []byte(`{"values":{},"changed":["acme.Bus.meta.absent"]}`)
		}
		if _, err := DecodeTagValueChanges(subject, data); err == nil {
			t.Fatalf("accepted invalid path %s", path)
		}
	}
}

func TestTagValueNamedOrganisationDoesNotCollideWithTheSubjectPrefix(t *testing.T) {
	pub, s, counted := testTagBatches(t)
	finish := pub.BeginTagValueBatch("tagvalue.Device")
	if err := pub.PublishTagValue("tagvalue.tagvalue.Device.meta.lat", tree.TagValue{Type: "value", Value: 49.2}); err != nil {
		t.Fatal(err)
	}
	if counted.calls != 0 {
		t.Fatal("organisation name bypassed batching")
	}
	if err := finish(); err != nil {
		t.Fatal(err)
	}
	if len(retainedBatch(t, s, "tagvalue.Device.meta").Values) != 1 {
		t.Fatal("missing batch")
	}
	if err := pub.ForgetTagValues("tagvalue.Device"); err != nil {
		t.Fatal(err)
	}
	info, err := s.JetStream().StreamInfo("tagvalue")
	if err != nil {
		t.Fatal(err)
	}
	if info.State.Msgs != 0 {
		t.Fatal("organisation name prevented deletion")
	}
}

func TestRawTagBatchPreservesLargeIntegerArrayValues(t *testing.T) {
	pub, s, _ := testTagBatches(t)
	if err := pub.TagValuePublish("tagvalue.acme.Device.counters", []byte(`{"counters":{"type":"value","value":[9007199254740993],"timestamp":123}}`)); err != nil {
		t.Fatal(err)
	}
	message, err := s.JetStream().GetLastMsg("tagvalue", tagBatchSubject("acme.Device", ""))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(message.Data, []byte("9007199254740993")) {
		t.Fatalf("integer lost precision: %s", message.Data)
	}
}
