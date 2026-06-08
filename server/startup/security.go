package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

func validateProductionSecrets() error {
	if !isProductionMode() {
		return nil
	}

	var problems []string
	requireSecret := func(name string) {
		value := strings.TrimSpace(os.Getenv(name))
		if value == "" {
			problems = append(problems, name+" is required in production")
			return
		}
		if isKnownDefaultSecret(value) {
			problems = append(problems, name+" uses an insecure default value")
		}
	}

	requireSecret("JWT_SECRET")
	requireSecret("NATS_INTERNAL_PASSWORD")
	requireSecret("NATS_BROWSER_TOKEN")

	if envEnabledDefault("EMBEDDED_MQTT_SERVER", true) {
		requireSecret("MQTT_PASSWORD")
	}
	if envEnabledDefault("MQTT_CLIENT_ENABLED", true) {
		requireSecret("MQTT_CLIENT_PASSWORD")
	}
	if value := strings.TrimSpace(os.Getenv("XACT_BOOTSTRAP_ADMIN_PASSWORD")); value != "" && isKnownDefaultSecret(value) {
		problems = append(problems, "XACT_BOOTSTRAP_ADMIN_PASSWORD uses an insecure default value")
	}

	if len(problems) > 0 {
		return fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	return nil
}

func isProductionMode() bool {
	for _, name := range []string{"XACT_ENV", "APP_ENV", "ENV"} {
		switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
		case "prod", "production":
			return true
		}
	}
	return envEnabled("XACT_PRODUCTION")
}

func isKnownDefaultSecret(value string) bool {
	v := strings.ToLower(strings.TrimSpace(value))
	switch v {
	case "", "admin", "password", "changeme", "change-me", "xact",
		"xact-jwt-secret-change-me", "xact-internal-secret", "xact-browser-token":
		return true
	}
	return strings.Contains(v, "change-me") || strings.Contains(v, "changeme")
}

func envStringDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func envIntDefault(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	n, err := strconv.Atoi(value)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func envNonNegativeIntDefault(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 {
		return fallback
	}
	return n
}

func envInt64Default(name string, fallback int64) int64 {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func envList(name string) []string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func eventRetentionDays() int {
	fallback := 30
	if isProductionMode() {
		fallback = 0
	}
	return envNonNegativeIntDefault("EVENT_RETENTION_DAYS", fallback)
}

func envEnabled(name string) bool {
	return parseEnvBool(os.Getenv(name), false)
}

func envEnabledDefault(name string, fallback bool) bool {
	return parseEnvBool(os.Getenv(name), fallback)
}

func parseEnvBool(value string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return fallback
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return fallback
	}
}
