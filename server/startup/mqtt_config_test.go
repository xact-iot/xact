package main

import "testing"

func TestEmbeddedMQTTListenURL(t *testing.T) {
	t.Setenv("EMBEDDED_MQTT_LISTEN_URL", "mqtts://0.0.0.0:8883")
	t.Setenv("MQTT_BROKER_URL", "mqtts://localhost:8883")
	if got := embeddedMQTTListenURL(); got != "mqtts://0.0.0.0:8883" {
		t.Fatalf("embeddedMQTTListenURL() = %q", got)
	}
}

func TestEmbeddedMQTTListenURLCompatibilityFallback(t *testing.T) {
	t.Setenv("EMBEDDED_MQTT_LISTEN_URL", "")
	t.Setenv("MQTT_BROKER_URL", "mqtt://127.0.0.1:1883")
	if got := embeddedMQTTListenURL(); got != "mqtt://127.0.0.1:1883" {
		t.Fatalf("embeddedMQTTListenURL() = %q", got)
	}
}

func TestEmbeddedMQTTTLSEnabledByURL(t *testing.T) {
	t.Setenv("ENABLE_TLS", "false")
	if !embeddedMQTTTLSEnabled("mqtts://0.0.0.0:8883") {
		t.Fatal("mqtts listener must enable TLS")
	}
	if embeddedMQTTTLSEnabled("mqtt://0.0.0.0:1883") {
		t.Fatal("plain mqtt listener unexpectedly enabled TLS")
	}
}

func TestEmbeddedMQTTTLSFilesOverride(t *testing.T) {
	t.Setenv("MQTT_TLS_CERT_FILE", "./certs/mqtt-server.crt")
	t.Setenv("MQTT_TLS_KEY_FILE", "./certs/mqtt-server.key")
	crt, key := embeddedMQTTTLSFiles()
	if crt != "./certs/mqtt-server.crt" || key != "./certs/mqtt-server.key" {
		t.Fatalf("embeddedMQTTTLSFiles() = %q, %q", crt, key)
	}
}
