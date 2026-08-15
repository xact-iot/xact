package main

import (
	"testing"
	"time"

	"github.com/xact-iot/xact/visualscripts"
)

func TestDispatchVisualScriptTagDecodesBroadcastValue(t *testing.T) {
	router := visualscripts.NewTagChangeRouter(10, 10)
	received := make(chan visualscripts.TagChange, 1)
	_, err := router.Register("acme", "test", "SITE.*.Status.Running", func(change visualscripts.TagChange) { received <- change })
	if err != nil {
		t.Fatal(err)
	}
	dispatchVisualScriptTag(router, "xact.internal.bcast.tagvalue.acme.SITE.Pump01.Status.Running", []byte(`{"Status.Running":{"value":true,"status":"good","timestamp":1750000000123}}`))
	select {
	case change := <-received:
		if change.OrgName != "acme" || change.TagPath != "SITE.Pump01.Status.Running" || change.DevicePath != "SITE" || change.InstanceKey != "Pump01" || change.Value != true {
			t.Fatalf("decoded change = %#v", change)
		}
		if change.Fields["status"] != "good" || !change.Timestamp.Equal(time.UnixMilli(1750000000123).UTC()) {
			t.Fatalf("decoded metadata = %#v", change)
		}
	case <-time.After(time.Second):
		t.Fatal("tag change was not dispatched")
	}
}

func TestTenantTagPathAlwaysUsesMessageOrganisation(t *testing.T) {
	path, err := tenantTagPath("acme", "/SITE/Pump01/Speed")
	if err != nil || path != "acme.SITE.Pump01.Speed" {
		t.Fatalf("tenant path = %q, %v", path, err)
	}
	if _, err := tenantTagPath("acme", "SITE.*.Speed"); err == nil {
		t.Fatal("Set Tag accepted a wildcard path")
	}
}
