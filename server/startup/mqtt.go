package main

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strconv"

	"github.com/wind-c/comqtt/v2/mqtt"
	"github.com/wind-c/comqtt/v2/mqtt/listeners"
	"github.com/wind-c/comqtt/v2/mqtt/packets"
)

// MqttPasswordHook provides MQTT authentication via password
type MqttPasswordHook struct {
	mqtt.HookBase
}

// ID returns the hook identifier
func (h *MqttPasswordHook) ID() string {
	return "password-auth"
}

// Provides indicates which events this hook handles
func (h *MqttPasswordHook) Provides(b byte) bool {
	return bytes.Contains([]byte{
		mqtt.OnConnectAuthenticate,
		mqtt.OnACLCheck,
		mqtt.OnConnect,
	}, []byte{b})
}

// OnConnectAuthenticate validates client password
func (h *MqttPasswordHook) OnConnectAuthenticate(cl *mqtt.Client, pk packets.Packet) bool {
	expectedPassword := os.Getenv("MQTT_BROKER_PASSWORD")
	if expectedPassword == "" {
		expectedPassword = "xact"
	}
	return string(pk.Connect.Password) == expectedPassword
}

// OnACLCheck allows all topics for all clients
func (h *MqttPasswordHook) OnACLCheck(cl *mqtt.Client, topic string, write bool) bool {
	return true
}
func (h *MqttPasswordHook) OnConnect(cl *mqtt.Client, pk packets.Packet) error {
	fmt.Println("MQTT Client connected:", cl.ID)
	return nil
}

// StartMqttBroker starts the embedded MQTT broker and returns once the broker
// is listening and ready to accept connections. The broker serve loop runs in
// a background goroutine. Returns a non-nil error if the broker could not bind
// its listener (e.g. port already in use).
func StartMqttBroker() error {
	log.Printf("MQTT broker starting\n")
	// Create the new MQTT broker.
	broker := mqtt.New(nil)
	_ = broker.AddHook(new(MqttPasswordHook), nil)

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
