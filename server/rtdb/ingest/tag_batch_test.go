package ingest

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
	natsgo "github.com/nats-io/nats.go"
	xactnats "github.com/xact-iot/xact/rtdb/nats"
	"github.com/xact-iot/xact/rtdb/tree"
)

func TestDeviceIngestBatchesProcessedValuesAndRetiresReplay(t *testing.T) {
	s, err := natsserver.NewServer(&natsserver.Options{Port: -1, JetStream: true, StoreDir: t.TempDir(), NoLog: true})
	if err != nil {
		t.Fatal(err)
	}
	go s.Start()
	t.Cleanup(s.Shutdown)
	if !s.ReadyForConnections(5 * time.Second) {
		t.Fatal("NATS did not start")
	}
	nc, err := natsgo.Connect(s.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	if err := xactnats.PreparePubDedup(s, nc); err != nil {
		t.Fatal(err)
	}
	pub, err := xactnats.GetBroadcastStream(xactnats.TagValueStream)
	if err != nil {
		t.Fatal(err)
	}
	previous := tree.TagValuePublisher
	tree.TagValuePublisher = pub
	t.Cleanup(func() { tree.TagValuePublisher = previous })
	ops := tree.NewTreeWithOperations(nil)
	if err := ops.CreateOrganisationNode("TestOrg", ""); err != nil {
		t.Fatal(err)
	}
	var deleteErr error
	ops.SetOnChange(func(path string, node tree.TreeNode) {
		if node == nil {
			deleteErr = errors.Join(deleteErr, pub.ForgetTagValues(path))
		}
	})
	processor := NewProcessor(ops)
	for _, raw := range []string{`{"meta":{"lat":{"value":10,"scaling":{"scale":2,"offset":1}},"lon":-61.123456,"online":true},"status":{"speed":42,"moving":true}}`, `{"meta":{"lat":12}}`} {
		data, err := ParsePayload([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		if err := processor.WriteDeviceData("TestOrg", "", "VMS", "Dev1", data); err != nil {
			t.Fatal(err)
		}
	}
	js, err := nc.JetStream()
	if err != nil {
		t.Fatal(err)
	}
	info, err := js.StreamInfo("tagvalue")
	if err != nil {
		t.Fatal(err)
	}
	if info.State.LastSeq != 3 || info.State.Msgs != 2 {
		t.Fatalf("publications=%d retained=%d, want 3/2", info.State.LastSeq, info.State.Msgs)
	}
	msg, err := js.GetLastMsg("tagvalue", "xact.internal.bcast.tagbatch.TestOrg.VMS.Dev1.meta.all")
	if err != nil {
		t.Fatal(err)
	}
	var batch xactnats.TagValueBatch
	if err := json.Unmarshal(msg.Data, &batch); err != nil {
		t.Fatal(err)
	}
	if batch.Values["TestOrg.VMS.Dev1.meta.lat"].Value != 25.0 || len(batch.Values) != 4 || len(batch.Changed) != 1 {
		t.Fatalf("unprocessed or incomplete batch: %#v", batch)
	}
	if err := DeleteDevice(ops, "TestOrg", "", "VMS", "Dev1"); err != nil {
		t.Fatal(err)
	}
	if deleteErr != nil {
		t.Fatal(deleteErr)
	}
	info, err = js.StreamInfo("tagvalue")
	if err != nil {
		t.Fatal(err)
	}
	if info.State.Msgs != 0 {
		t.Fatalf("deleted device retained %d batch messages", info.State.Msgs)
	}
}
