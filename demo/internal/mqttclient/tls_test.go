package mqttclient

import (
	"encoding/pem"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeBrokerURL(t *testing.T) {
	tests := map[string]string{
		"":                        "",
		"localhost:1883":          "tcp://localhost:1883",
		"mqtt://127.0.0.1:1883":   "tcp://127.0.0.1:1883",
		"mqtts://broker.example":  "ssl://broker.example",
		"ssl://broker.example":    "ssl://broker.example",
		" tcp://127.0.0.1:1883  ": "tcp://127.0.0.1:1883",
	}
	for input, want := range tests {
		if got := NormalizeBrokerURL(input); got != want {
			t.Fatalf("NormalizeBrokerURL(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestTLSConfigFromEnvUsesCAAndOverrides(t *testing.T) {
	caFile := writeTestCert(t, t.TempDir())
	t.Setenv("MQTT_TLS_CA_FILE", caFile)
	t.Setenv("MQTT_TLS_SERVER_NAME", "localhost")
	t.Setenv("MQTT_TLS_INSECURE_SKIP_VERIFY", "true")

	cfg := TLSConfigFromEnv("ssl://127.0.0.1:1883")
	if cfg == nil {
		t.Fatal("TLSConfigFromEnv returned nil for TLS URL")
	}
	if cfg.RootCAs == nil {
		t.Fatal("RootCAs was not configured")
	}
	if cfg.ServerName != "localhost" {
		t.Fatalf("ServerName = %q, want localhost", cfg.ServerName)
	}
	if !cfg.InsecureSkipVerify {
		t.Fatal("InsecureSkipVerify was not enabled")
	}
}

func TestTLSConfigFromEnvUsesDefaultCertDir(t *testing.T) {
	dir := t.TempDir()
	writeTestCert(t, dir)
	t.Setenv("HTTPS_CERTS_DIR", dir)

	cfg := TLSConfigFromEnv("mqtts://localhost:1883")
	if cfg == nil {
		t.Fatal("TLSConfigFromEnv returned nil for mqtts URL")
	}
	if cfg.RootCAs == nil {
		t.Fatal("RootCAs was not configured from HTTPS_CERTS_DIR/server.crt")
	}
}

func TestTLSConfigFromEnvIgnoresPlainURLs(t *testing.T) {
	if cfg := TLSConfigFromEnv("tcp://127.0.0.1:1883"); cfg != nil {
		t.Fatalf("TLSConfigFromEnv returned %#v for plain TCP URL", cfg)
	}
}

func writeTestCert(t *testing.T, dir string) string {
	t.Helper()
	srv := httptest.NewTLSServer(nil)
	defer srv.Close()

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	caFile := filepath.Join(dir, "server.crt")
	if err := os.WriteFile(caFile, certPEM, 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	return caFile
}
