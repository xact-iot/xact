package lta_incidents_driver

import (
	"crypto/tls"
	"net/http"
	"os"
	"time"
)

const DefaultIncidentPollInterval = 2 * time.Minute

type Config struct {
	LTAAPIKey    string
	LTABaseURL   string
	XACTBaseURL  string
	XACTAPIKey   string
	XACTUsername string
	XACTPassword string
	Tenant       string
	Zone         string
	PollInterval time.Duration
	HTTPClient   *http.Client // optional override used for both clients
	LTAClient    *http.Client
	XACTClient   *http.Client
}

func ConfigFromEnv() Config {
	pollInterval := DefaultIncidentPollInterval
	if raw := os.Getenv("LTA_INCIDENTS_POLL_INTERVAL"); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil {
			pollInterval = parsed
		}
	}

	return Config{
		LTAAPIKey:    os.Getenv("LTA_API_KEY"),
		LTABaseURL:   getenvDefault("LTA_BASE_URL", DefaultLTABaseURL),
		XACTBaseURL:  getenvDefault("XACT_BASE_URL", DefaultXACTBaseURL),
		XACTAPIKey:   os.Getenv("XACT_API_KEY"),
		XACTUsername: getenvDefault("XACT_USERNAME", "admin"),
		XACTPassword: getenvDefault("XACT_PASSWORD", "admin"),
		Tenant:       getenvDefault("XACT_TENANT", DefaultTenant),
		Zone:         getenvDefault("LTA_INCIDENTS_ZONE", DefaultZone),
		PollInterval: pollInterval,
		LTAClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		XACTClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // local XACT uses self-signed TLS by default
			},
		},
	}
}

func getenvDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
