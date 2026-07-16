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
	"strings"

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

func embeddedMQTTListenURL() string {
	listenURL := strings.TrimSpace(os.Getenv("EMBEDDED_MQTT_LISTEN_URL"))
	if listenURL != "" {
		return listenURL
	}

	// Retain the historical behaviour when a separate listener URL has not
	// been configured. Clustered installations should set both variables:
	// the listener URL identifies the local interface and MQTT_BROKER_URL
	// identifies the client-facing broker endpoint.
	brokerURL := strings.TrimSpace(os.Getenv("MQTT_BROKER_URL"))
	if brokerURL != "" {
		return brokerURL
	}
	return "mqtt://127.0.0.1:1883"
}

func embeddedMQTTTLSEnabled(listenURL string) bool {
	if u, err := url.Parse(listenURL); err == nil {
		switch strings.ToLower(u.Scheme) {
		case "mqtts", "ssl", "tls":
			return true
		}
	}
	enabled, err := strconv.ParseBool(strings.TrimSpace(os.Getenv("ENABLE_TLS")))
	return err == nil && enabled
}

func embeddedMQTTTLSFiles() (string, string) {
	crtFile := strings.TrimSpace(os.Getenv("MQTT_TLS_CERT_FILE"))
	keyFile := strings.TrimSpace(os.Getenv("MQTT_TLS_KEY_FILE"))
	if crtFile != "" || keyFile != "" {
		return crtFile, keyFile
	}

	certsDir := strings.TrimSpace(os.Getenv("HTTP_CERTS_DIR"))
	if certsDir == "" {
		certsDir = strings.TrimSpace(os.Getenv("HTTPS_CERTS_DIR"))
	}
	return filepath.Join(certsDir, "server.crt"), filepath.Join(certsDir, "server.key")
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

	listenURL := embeddedMQTTListenURL()
	tlsEnabled := embeddedMQTTTLSEnabled(listenURL)

	var tlsConfig *tls.Config
	if tlsEnabled {
		crtFile, keyFile := embeddedMQTTTLSFiles()
		if crtFile == "" || keyFile == "" {
			return fmt.Errorf("MQTT broker TLS certificate and key must both be configured")
		}

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

	// listeners.NewTCP expects a bare host:port, not a full URL.
	// Strip any scheme prefix (e.g. "mqtt://") before passing it.
	listenAddr := listenURL
	if u, err := url.Parse(listenURL); err == nil && u.Host != "" {
		listenAddr = u.Host
	}

	log.Printf("MQTT broker listening on %s (TLS=%t)\n", listenAddr, tlsEnabled)
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

	log.Println("MQTT broker started on", listenURL)
	return nil
}
