package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/url"
	"sync"
	"testing"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
	"github.com/wind-c/comqtt/v2/mqtt"
	"github.com/wind-c/comqtt/v2/mqtt/packets"
	ingestmqtt "github.com/xact-iot/xact/rtdb/ingest/mqtt"
)

type mqttTestKeys struct {
	sync.Mutex
	revoked bool
	fail    bool
}

func (k *mqttTestKeys) GetAPIKeyOrg(_ context.Context, key string) (string, error) {
	k.Lock()
	defer k.Unlock()
	if k.fail {
		return "", errors.New("database unavailable")
	}
	if k.revoked {
		return "", nil
	}
	return map[string]string{"alpha-key": "alpha", "beta-key": "beta"}[key], nil
}

func TestMQTTTenantAuthenticationAndACL(t *testing.T) {
	keys := &mqttTestKeys{}
	h := &MqttPasswordHook{keys: keys, internalPassword: "process-local-test-secret"}
	for _, tc := range []struct {
		name, user, pass, id, will string
		allowed                    bool
	}{
		{"valid", "alpha", "alpha-key", "alpha:sensor", "", true},
		{"wrong tenant", "beta", "alpha-key", "beta:sensor", "", false},
		{"global password", "alpha", "xact", "alpha:sensor", "", false},
		{"missing username", "", "alpha-key", "alpha:sensor", "", false},
		{"other session", "alpha", "alpha-key", "beta:sensor", "", false},
		{"internal session", "alpha", "alpha-key", mqttInternalUsername, "", false},
		{"empty device id", "alpha", "alpha-key", "alpha:", "", false},
		{"valid will", "alpha", "alpha-key", "alpha:sensor", "xact/data/alpha/sensor/one", true},
		{"foreign will", "alpha", "alpha-key", "alpha:sensor", "xact/data/beta/sensor/one", false},
		{"internal", mqttInternalUsername, "process-local-test-secret", mqttInternalUsername, "", true},
		{"internal impersonation", mqttInternalUsername, "alpha-key", mqttInternalUsername, "", false},
		{"internal will", mqttInternalUsername, "process-local-test-secret", mqttInternalUsername, "xact/data/beta/sensor/one", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cl := &mqtt.Client{ID: tc.id}
			pk := packets.Packet{Connect: packets.ConnectParams{Username: []byte(tc.user), Password: []byte(tc.pass), WillFlag: tc.will != "", WillTopic: tc.will}}
			if got := h.OnConnectAuthenticate(cl, pk); got != tc.allowed {
				t.Fatalf("authentication = %v", got)
			}
			h.OnDisconnect(cl, nil, true)
			if h.OnACLCheck(cl, "xact/data/alpha/sensor/one", true) {
				t.Fatal("disconnected client authorized")
			}
		})
	}
	cl := &mqtt.Client{ID: "alpha:sensor"}
	if !h.OnConnectAuthenticate(cl, packets.Packet{Connect: packets.ConnectParams{Username: []byte("alpha"), Password: []byte("alpha-key")}}) {
		t.Fatal("valid auth rejected")
	}
	for _, tc := range []struct {
		topic          string
		write, allowed bool
	}{
		{"xact/data/alpha/sensor/one", true, true},
		{"xact/data/alpha/zone/north/sensor/one", true, true},
		{"xact/data/beta/sensor/one", true, false},
		{"xact/data/alpha/../beta/sensor/one", true, false},
		{"xact/control/alpha/sensor/one", true, false},
		{"xact/data/alpha/sensor/+", true, false},
		{"xact/data/alpha/#", false, true},
		{"xact/control/alpha/+/+", false, true},
		{"xact/data/beta/#", false, false},
		{"xact/data/+/#", false, false},
		{"xact/+/alpha/#", false, false},
		{"$share/group/xact/data/alpha/#", false, false},
		{"#", false, false},
		{"$SYS/#", false, false},
		{"xact/data/alpha/#/sensor", false, false},
	} {
		if got := h.OnACLCheck(cl, tc.topic, tc.write); got != tc.allowed {
			t.Errorf("topic %q write=%v: %v", tc.topic, tc.write, got)
		}
	}
	keys.Lock()
	keys.revoked = true
	keys.Unlock()
	if h.OnACLCheck(cl, "xact/data/alpha/sensor/one", true) || h.OnACLCheck(cl, "xact/data/alpha/sensor/one", false) {
		t.Fatal("revoked key still authorized")
	}
	keys.Lock()
	keys.revoked = false
	keys.fail = true
	keys.Unlock()
	if h.OnACLCheck(cl, "xact/data/alpha/sensor/one", true) {
		t.Fatal("database failure did not fail closed")
	}
}

// Exercise the actual broker over in-process pipes, including delivery ACLs and
// the privileged ingest subscription, without opening network listeners.
func TestMQTTBrokerTenantIsolation(t *testing.T) {
	broker := mqtt.New(&mqtt.Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	h := &MqttPasswordHook{keys: &mqttTestKeys{}, internalPassword: "process-local-test-secret"}
	if err := broker.AddHook(h, nil); err != nil {
		t.Fatal(err)
	}
	if err := broker.Serve(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { broker.Close() })
	connect := func(user, pass, id string, allowed bool) paho.Client {
		t.Helper()
		opts := paho.NewClientOptions().AddBroker("tcp://in-process:1883").SetUsername(user).SetPassword(pass).SetClientID(id).SetAutoReconnect(false).SetConnectRetry(false).SetConnectTimeout(time.Second)
		opts.SetCustomOpenConnectionFn(func(_ *url.URL, _ paho.ClientOptions) (net.Conn, error) {
			client, server := net.Pipe()
			go func() { _ = broker.EstablishConnection("test", server); server.Close() }()
			return client, nil
		})
		cl := paho.NewClient(opts)
		token := cl.Connect()
		if !token.WaitTimeout(3 * time.Second) {
			t.Fatal("MQTT connect timeout")
		}
		if (token.Error() == nil) != allowed {
			t.Fatalf("connect allowed=%v: %v", allowed, token.Error())
		}
		if allowed {
			t.Cleanup(func() { cl.Disconnect(20) })
		}
		return cl
	}
	wait := func(token paho.Token) {
		t.Helper()
		if !token.WaitTimeout(3*time.Second) || token.Error() != nil {
			t.Fatalf("MQTT operation: %v", token.Error())
		}
	}
	alpha := connect("alpha", "alpha-key", "alpha:one", true)
	beta := connect("beta", "beta-key", "beta:one", true)
	internal := connect(mqttInternalUsername, "process-local-test-secret", mqttInternalUsername, true)
	connect("alpha", "alpha-key", "beta:one", false)
	connect("beta", "alpha-key", "beta:attacker", false)
	connect("alpha", "xact", "alpha:attacker", false)
	for _, topic := range []string{"#", "xact/data/+/#", "xact/data/beta/#"} {
		token := alpha.Subscribe(topic, 1, func(paho.Client, paho.Message) {})
		wait(token)
		if sub := token.(*paho.SubscribeToken); sub.Result()[topic] != 0x80 {
			t.Fatalf("forbidden subscription %q accepted", topic)
		}
	}
	alphaMessages, betaMessages, ingestMessages := make(chan string, 8), make(chan string, 8), make(chan string, 8)
	wait(alpha.Subscribe("xact/data/alpha/#", 1, func(_ paho.Client, m paho.Message) { alphaMessages <- string(m.Payload()) }))
	wait(beta.Subscribe("xact/data/beta/#", 1, func(_ paho.Client, m paho.Message) { betaMessages <- string(m.Payload()) }))
	wait(internal.Subscribe(ingestmqtt.TopicPattern, 1, func(_ paho.Client, m paho.Message) { ingestMessages <- string(m.Payload()) }))
	wait(alpha.Publish("xact/data/alpha/sensor/one", 1, false, "alpha-value"))
	wait(beta.Publish("xact/data/beta/sensor/one", 1, false, "beta-value"))
	for channel, expected := range map[chan string]string{alphaMessages: "alpha-value", betaMessages: "beta-value"} {
		select {
		case got := <-channel:
			if got != expected {
				t.Fatalf("cross-tenant delivery: %q", got)
			}
		case <-time.After(time.Second):
			t.Fatal("own-tenant message missing")
		}
	}
	for i := 0; i < 2; i++ {
		select {
		case <-ingestMessages:
		case <-time.After(time.Second):
			t.Fatal("internal ingest message missing")
		}
	}
	// A denied QoS0 publication is discarded. A following allowed message
	// provides a processing barrier on the same connection.
	wait(alpha.Publish("xact/data/beta/sensor/one", 0, false, "forged"))
	wait(alpha.Publish("xact/data/alpha/sensor/one", 1, false, "barrier"))
	select {
	case got := <-alphaMessages:
		if got != "barrier" {
			t.Fatalf("unexpected alpha message %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("processing barrier missing")
	}
	select {
	case got := <-betaMessages:
		t.Fatalf("forged message delivered: %q", got)
	default:
	}
}
