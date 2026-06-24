package mqttclient

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// NormalizeBrokerURL converts XACT's friendly MQTT schemes to Paho schemes.
func NormalizeBrokerURL(broker string) string {
	broker = strings.TrimSpace(broker)
	if strings.HasPrefix(broker, "mqtt://") {
		return "tcp://" + strings.TrimPrefix(broker, "mqtt://")
	}
	if strings.HasPrefix(broker, "mqtts://") {
		return "ssl://" + strings.TrimPrefix(broker, "mqtts://")
	}
	if broker != "" && !strings.Contains(broker, "://") {
		return "tcp://" + broker
	}
	return broker
}

// TLSConfigFromEnv returns a TLS config for TLS broker URLs.
//
// MQTT uses normal TLS server authentication: clients do not need certificates
// unless the broker explicitly requires mutual TLS. XACT authenticates MQTT
// clients with MQTT_BROKER_PASSWORD, so these settings only control how the
// client verifies the broker certificate.
func TLSConfigFromEnv(brokerURL string) *tls.Config {
	if !IsTLSBrokerURL(brokerURL) {
		return nil
	}

	cfg := &tls.Config{} //nolint:gosec // Verification stays enabled unless explicitly disabled by env below.
	if serverName := strings.TrimSpace(firstEnv("MQTT_TLS_SERVER_NAME", "MQTT_CLIENT_TLS_SERVER_NAME")); serverName != "" {
		cfg.ServerName = serverName
	}
	if value := strings.TrimSpace(firstEnv("MQTT_TLS_INSECURE_SKIP_VERIFY", "MQTT_CLIENT_TLS_INSECURE_SKIP_VERIFY")); value != "" {
		if insecure, err := strconv.ParseBool(value); err == nil {
			cfg.InsecureSkipVerify = insecure //nolint:gosec // Explicit operator opt-in for local diagnostics.
		}
	}

	caFile := strings.TrimSpace(firstEnv("MQTT_TLS_CA_FILE", "MQTT_CLIENT_TLS_CA_FILE"))
	if caFile == "" {
		caFile = defaultServerCertFile()
	}
	if caFile != "" {
		roots, err := loadCertPool(caFile)
		if err != nil {
			log.Printf("MQTT TLS: could not load CA file %s: %v", caFile, err)
		} else {
			cfg.RootCAs = roots
		}
	}

	return cfg
}

func IsTLSBrokerURL(brokerURL string) bool {
	u, err := url.Parse(strings.TrimSpace(brokerURL))
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "ssl", "tls", "mqtts":
		return true
	default:
		return false
	}
}

func firstEnv(names ...string) string {
	for _, name := range names {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return ""
}

func defaultServerCertFile() string {
	certsDir := strings.TrimSpace(firstEnv("HTTP_CERTS_DIR", "HTTPS_CERTS_DIR"))
	if certsDir == "" {
		return ""
	}
	return filepath.Join(certsDir, "server.crt")
}

func loadCertPool(caFile string) (*x509.CertPool, error) {
	pemBytes, err := os.ReadFile(caFile)
	if err != nil {
		return nil, err
	}
	roots, err := x509.SystemCertPool()
	if err != nil {
		roots = x509.NewCertPool()
	}
	if !roots.AppendCertsFromPEM(pemBytes) {
		return nil, fmt.Errorf("no certificates found")
	}
	return roots, nil
}
