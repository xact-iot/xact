package nats

import (
	"bufio"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	natsgo "github.com/nats-io/nats.go"
)

func browserTestBroker(t *testing.T, auth *ClientAuthenticator) *server.Server {
	t.Helper()
	s, err := server.NewServer(&server.Options{
		DontListen: true, JetStream: true, StoreDir: t.TempDir(), NoLog: true,
		CustomClientAuthentication: auth,
	})
	if err != nil {
		t.Fatal(err)
	}
	go s.Start()
	t.Cleanup(s.Shutdown)
	if !s.ReadyForConnections(5 * time.Second) {
		t.Fatal("broker did not start")
	}
	return s
}

func browserTestConnect(t *testing.T, s *server.Server, user, password string, options ...natsgo.Option) *natsgo.Conn {
	t.Helper()
	opts := []natsgo.Option{natsgo.InProcessServer(s), natsgo.UserInfo(user, password), natsgo.NoReconnect(), natsgo.Timeout(time.Second)}
	nc, err := natsgo.Connect("nats://in-process", append(opts, options...)...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	return nc
}

func TestBrowserBrokerEnforcesTenantAndJetStreamPermissions(t *testing.T) {
	auth := NewClientAuthenticator("internal-secret")
	auth.SetBrowserAuthenticator(func(_ context.Context, token string) (BrowserSession, bool) {
		return BrowserSession{Org: "alpha", UserID: "7", ExpiresAt: time.Now().Add(time.Hour), ReadTree: true, ReadTags: true}, token == "alpha-session"
	})
	s := browserTestBroker(t, auth)
	internal := browserTestConnect(t, s, "internal", "internal-secret")
	errors := make(chan error, 32)
	browser := browserTestConnect(t, s, "browser", "alpha-session", natsgo.ErrorHandler(func(_ *natsgo.Conn, _ *natsgo.Subscription, err error) { errors <- err }))
	js, err := internal.JetStream()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := js.AddStream(&natsgo.StreamConfig{Name: "audit", Subjects: []string{"audit.>"}, Storage: natsgo.MemoryStorage}); err != nil {
		t.Fatal(err)
	}

	for _, subject := range []string{"rtdb.tree.alpha.device", "xact.internal.bcast.tagvalue.alpha.device.temp", "xact.internal.bcast.mobile.alpha.7", BrowserInboxPrefix("alpha-session") + ".reply"} {
		sub, err := browser.SubscribeSync(subject)
		if err != nil {
			t.Fatal(err)
		}
		if err := browser.Flush(); err != nil {
			t.Fatal(err)
		}
		if err := internal.Publish(subject, []byte("own data")); err != nil {
			t.Fatal(err)
		}
		if _, err := sub.NextMsg(time.Second); err != nil {
			t.Fatalf("allowed subject %s: %v", subject, err)
		}
	}

	assertDenied := func(action func() error) {
		t.Helper()
		if err := action(); err != nil {
			t.Fatal(err)
		}
		if err := browser.Flush(); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-errors:
			if !strings.Contains(strings.ToLower(err.Error()), "permissions violation") {
				t.Fatalf("unexpected error: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("expected broker permission rejection")
		}
	}
	for _, subject := range []string{"rtdb.tree.>", "rtdb.tree.beta.>", "xact.internal.bcast.tagvalue.beta.>", "xact.internal.bcast.mobile.alpha.8", "$KV.>", "$JS.>", "_INBOX.>", BrowserInboxPrefix("other-session") + ".>"} {
		t.Run("subscribe/"+subject, func(t *testing.T) {
			assertDenied(func() error { _, err := browser.SubscribeSync(subject); return err })
		})
	}
	for _, subject := range []string{"$JS.API.STREAM.DELETE.audit", "$JS.API.STREAM.PURGE.audit", "$JS.API.CONSUMER.CREATE.audit", "$JS.API.STREAM.MSG.GET.audit", "$KV.RTDB_PERSIST.alpha.device", "xact.command.alpha.device", "xact.command.beta.device", "rtdb.tree.alpha.device", BrowserInboxPrefix("other-session") + ".reply"} {
		t.Run("publish/"+subject, func(t *testing.T) { assertDenied(func() error { return browser.Publish(subject, []byte("{}")) }) })
	}
	if _, err := js.StreamInfo("audit"); err != nil {
		t.Fatalf("browser deleted internal stream: %v", err)
	}
}

func TestBrowserBrokerRejectsLegacyCredentialsAndInvalidSubjects(t *testing.T) {
	auth := NewClientAuthenticator("internal-secret")
	s := browserTestBroker(t, auth)
	connect := func(user, password string) {
		t.Helper()
		conn, err := s.InProcessConn()
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		if err := conn.SetDeadline(time.Now().Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		reader := bufio.NewReader(conn)
		if _, err := reader.ReadString('\n'); err != nil {
			t.Fatal(err)
		}
		payload, err := json.Marshal(map[string]string{"user": user, "pass": password})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Write(append(append([]byte("CONNECT "), payload...), '\r', '\n')); err != nil {
			t.Fatal(err)
		}
		response, err := reader.ReadString('\n')
		if err != nil || !strings.Contains(response, "Authorization Violation") {
			t.Fatalf("expected authentication rejection, got %q: %v", response, err)
		}
	}
	connect("browser", "legacy-shared-token") // API not ready: fail closed.
	connect("internal", "wrong")
	connect("", "")
	auth.SetBrowserAuthenticator(func(_ context.Context, org string) (BrowserSession, bool) {
		return BrowserSession{Org: org, UserID: "7", ExpiresAt: time.Now().Add(time.Hour)}, org != "legacy-shared-token"
	})
	connect("browser", "legacy-shared-token")
	for _, org := range []string{"*", ">", "alpha.beta", "alpha.*", "alpha beta", "alpha\n", "alpha/beta"} {
		connect("browser", org)
	}
}

func TestBrowserBrokerScopesCommandsAndExpiresConnections(t *testing.T) {
	auth := NewClientAuthenticator("internal-secret")
	auth.SetBrowserAuthenticator(func(_ context.Context, token string) (BrowserSession, bool) {
		return BrowserSession{Org: "alpha", UserID: "7", ExpiresAt: time.Now().Add(time.Second), SendCommands: true}, true
	})
	s := browserTestBroker(t, auth)
	internal := browserTestConnect(t, s, "internal", "internal-secret")
	if _, err := internal.Subscribe("xact.command.alpha.device", func(m *natsgo.Msg) { _ = m.Respond([]byte("ok")) }); err != nil {
		t.Fatal(err)
	}
	if err := internal.Flush(); err != nil {
		t.Fatal(err)
	}
	errors := make(chan error, 2)
	closed := make(chan struct{})
	browser := browserTestConnect(t, s, "browser", "session", natsgo.CustomInboxPrefix(BrowserInboxPrefix("session")), natsgo.ClosedHandler(func(*natsgo.Conn) { close(closed) }), natsgo.ErrorHandler(func(_ *natsgo.Conn, _ *natsgo.Subscription, err error) { errors <- err }))
	msg, err := browser.Request("xact.command.alpha.device", []byte("{}"), 500*time.Millisecond)
	if err != nil || string(msg.Data) != "ok" {
		t.Fatalf("own command failed: %v", err)
	}
	if err := browser.Publish("xact.command.beta.device", []byte("{}")); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errors:
		if !strings.Contains(strings.ToLower(err.Error()), "permissions violation") {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cross-tenant command allowed")
	}
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("session expiry did not disconnect browser")
	}
}
