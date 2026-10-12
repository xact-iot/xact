package nats

import (
	natsgo "github.com/nats-io/nats.go"
	"github.com/xact-iot/xact/applications"
	"strings"
	"testing"
	"time"
)

func TestApplicationCanReplyOnceWithoutBroadInboxPublishPermission(t *testing.T) {
	t.Setenv("REPLY_TEST_APP_PASSWORD", "app-secret")
	t.Setenv("REPLY_TEST_ADAPTER_PASSWORD", "adapter-secret")
	subject := "xact.app.v1.default.public_bus.default.ingest.translink"
	auth := NewClientAuthenticator("internal-secret")
	auth.services = map[string]applications.Service{
		"app:public_bus": {Username: "app:public_bus", PasswordEnv: "REPLY_TEST_APP_PASSWORD", Publish: []string{"xact.internal.ingest_request.default.PUBLIC_BUS.BUSES.*"}, Subscribe: []string{subject}},
		"app:translink":  {Username: "app:translink", PasswordEnv: "REPLY_TEST_ADAPTER_PASSWORD", Publish: []string{subject}, Subscribe: []string{"_INBOX.app_translink.>"}},
	}
	broker := browserTestBroker(t, auth)
	failures := make(chan error, 8)
	app := browserTestConnect(t, broker, "app:public_bus", "app-secret", natsgo.ErrorHandler(func(_ *natsgo.Conn, _ *natsgo.Subscription, err error) { failures <- err }))
	adapter := browserTestConnect(t, broker, "app:translink", "adapter-secret", natsgo.CustomInboxPrefix("_INBOX.app_translink"))
	requests := make(chan *natsgo.Msg, 1)
	if _, err := app.Subscribe(subject, func(m *natsgo.Msg) { requests <- m; m.Respond([]byte("accepted")) }); err != nil {
		t.Fatal(err)
	}
	if err := app.Flush(); err != nil {
		t.Fatal(err)
	}
	response, err := adapter.Request(subject, []byte("batch"), time.Second)
	if err != nil || string(response.Data) != "accepted" {
		t.Fatalf("authorized reply failed: %v", err)
	}
	request := <-requests
	for _, reply := range []string{request.Reply, "_INBOX.app_translink.unrequested"} {
		if err := app.Publish(reply, []byte("unauthorized")); err != nil {
			t.Fatal(err)
		}
		if err := app.Flush(); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-failures:
			if !strings.Contains(strings.ToLower(err.Error()), "permissions violation") {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("extra/unrequested reply was allowed")
		}
	}
}
