package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/wind-c/comqtt/v2/mqtt"
	"github.com/wind-c/comqtt/v2/mqtt/listeners"
	"github.com/wind-c/comqtt/v2/mqtt/packets"
	ingestmqtt "github.com/xact-iot/xact/rtdb/ingest/mqtt"
)

const mqttInternalUsername = "xact:ingest"
const mqttIdentityKey = "xact.auth.identity"

type mqttKeyResolver interface {
	GetAPIKeyOrg(context.Context, string) (string, error)
}

type mqttIdentity struct {
	org, key string
	internal bool
}

// MqttPasswordHook binds devices to the organisation owning their ingest API key.
// The embedded ingest client has a separate, process-local credential.
type MqttPasswordHook struct {
	mqtt.HookBase
	keys             mqttKeyResolver
	internalPassword string
}

func (h *MqttPasswordHook) ID() string { return "tenant-auth" }
func (h *MqttPasswordHook) Provides(b byte) bool {
	return bytes.Contains([]byte{mqtt.OnConnectAuthenticate, mqtt.OnACLCheck, mqtt.OnDisconnect}, []byte{b})
}

func (h *MqttPasswordHook) validKey(org, key string) bool {
	if h.keys == nil || key == "" || !mqttLiteral(org) {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	owner, err := h.keys.GetAPIKeyOrg(ctx, key)
	return err == nil && owner == org
}

func (h *MqttPasswordHook) OnConnectAuthenticate(cl *mqtt.Client, pk packets.Packet) bool {
	h.OnDisconnect(cl, nil, false)
	username, password := string(pk.Connect.Username), string(pk.Connect.Password)
	identity := mqttIdentity{org: username, key: password}
	if username == mqttInternalUsername {
		provided, expected := sha256.Sum256([]byte(password)), sha256.Sum256([]byte(h.internalPassword))
		if h.internalPassword == "" || subtle.ConstantTimeCompare(provided[:], expected[:]) != 1 || cl.ID != mqttInternalUsername || pk.Connect.WillFlag {
			return false
		}
		identity = mqttIdentity{internal: true}
	} else {
		// MQTT persistent sessions are keyed solely by client ID. Namespace IDs
		// as well as topics to prevent cross-tenant session takeover.
		if !h.validKey(username, password) || !strings.HasPrefix(cl.ID, username+":") || !mqttLiteral(strings.TrimPrefix(cl.ID, username+":")) {
			return false
		}
		if pk.Connect.WillFlag && !mqttDeviceTopic(username, pk.Connect.WillTopic, true) {
			return false
		}
	}
	// Keep credentials on the connection so aborted handshakes do not leave
	// entries in a separate authentication cache.
	cl.Lock()
	if cl.Ext == nil {
		cl.Ext = make(map[string]interface{})
	}
	cl.Ext[mqttIdentityKey] = identity
	cl.Unlock()
	return true
}

func (h *MqttPasswordHook) OnACLCheck(cl *mqtt.Client, topic string, write bool) bool {
	cl.RLock()
	identity, ok := cl.Ext[mqttIdentityKey].(mqttIdentity)
	cl.RUnlock()
	if !ok {
		return false
	}
	if identity.internal {
		if write {
			return false
		}
		if topic == ingestmqtt.TopicPattern || topic == ingestmqtt.TopicPatternZoned {
			return true
		}
		parts := strings.Split(topic, "/")
		return len(parts) >= 3 && mqttDeviceTopic(parts[2], topic, true)
	}
	return h.validKey(identity.org, identity.key) && mqttDeviceTopic(identity.org, topic, write)
}

func (h *MqttPasswordHook) OnDisconnect(cl *mqtt.Client, _ error, _ bool) {
	cl.Lock()
	delete(cl.Ext, mqttIdentityKey)
	cl.Unlock()
}

func mqttLiteral(value string) bool {
	return value != "" && !strings.ContainsAny(value, "/.\\+#:*>") && strings.IndexFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) < 0
}

func mqttDeviceTopic(org, topic string, write bool) bool {
	parts := strings.Split(topic, "/")
	if len(parts) < 4 || parts[0] != "xact" || !mqttLiteral(org) || parts[2] != org {
		return false
	}
	if write {
		if parts[1] != "data" || !(len(parts) == 5 || (len(parts) == 7 && parts[3] == "zone")) {
			return false
		}
	} else if parts[1] != "data" && parts[1] != "control" {
		return false
	}
	for i, part := range parts[3:] {
		if !write && (part == "+" || (part == "#" && i == len(parts)-4)) {
			continue
		}
		if !mqttLiteral(part) {
			return false
		}
	}
	return true
}

// StartMqttBroker starts the embedded MQTT broker and returns once the broker
// is listening and ready to accept connections. The broker serve loop runs in
// a background goroutine. Returns a non-nil error if the broker could not bind
// its listener (e.g. port already in use).
func StartMqttBroker(keys mqttKeyResolver, internalPassword string) error {
	log.Printf("MQTT broker starting\n")
	// Create the new MQTT broker.
	broker := mqtt.New(nil)
	if err := broker.AddHook(&MqttPasswordHook{keys: keys, internalPassword: internalPassword}, nil); err != nil {
		return err
	}

	tlsEnabled, parseError := strconv.ParseBool(os.Getenv("ENABLE_TLS"))
	if parseError != nil {
		tlsEnabled = false
	}

	var tlsConfig *tls.Config
	if tlsEnabled {
		certsDir := os.Getenv("HTTP_CERTS_DIR")
		if certsDir == "" {
			certsDir = os.Getenv("HTTPS_CERTS_DIR")
		}
		crtFile := filepath.Join(certsDir, "server.crt")
		keyFile := filepath.Join(certsDir, "server.key")

		cert, err := tls.LoadX509KeyPair(crtFile, keyFile)
		if err != nil {
			log.Println("MQTT broker TLS error:", err)
			return err
		}

		tlsConfig = &tls.Config{
			Certificates: []tls.Certificate{cert},
			ClientAuth:   tls.NoClientCert,
		}
	}

	// Get MQTT URL from environment or use default
	mqttURL := os.Getenv("MQTT_BROKER_URL")
	if mqttURL == "" {
		mqttURL = "mqtt://127.0.0.1:1883"
	}

	// listeners.NewTCP expects a bare host:port, not a full URL.
	// Strip any scheme prefix (e.g. "mqtt://") before passing it.
	listenAddr := mqttURL
	if u, err := url.Parse(mqttURL); err == nil && u.Host != "" {
		listenAddr = u.Host
	}

	log.Printf("MQTT broker listening\n")
	// Create a listener on a standard port.
	tcp := listeners.NewTCP("broker_tcp", listenAddr, &listeners.Config{
		TLSConfig: tlsConfig,
	})

	// Add the listener to the server
	if err := broker.AddListener(tcp); err != nil {
		return fmt.Errorf("MQTT broker add listener: %w", err)
	}

	go func() {
		if err := broker.Serve(); err != nil {
			log.Println("MQTT broker serve error:", err)
		}
	}()

	log.Println("MQTT broker started on", mqttURL)
	return nil
}
