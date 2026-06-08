package main

import (
	"encoding/hex"
	"testing"

	"github.com/wind-c/comqtt/v2/mqtt"
	"github.com/wind-c/comqtt/v2/mqtt/packets"
)

func TestValidateProductionSecretsRejectsDefaults(t *testing.T) {
	t.Setenv("XACT_ENV", "production")
	t.Setenv("JWT_SECRET", "xact-jwt-secret-change-me")
	t.Setenv("NATS_INTERNAL_PASSWORD", "safe-internal-secret")
	t.Setenv("NATS_BROWSER_TOKEN", "safe-browser-token")
	t.Setenv("MQTT_PASSWORD", "safe-mqtt-secret")
	t.Setenv("MQTT_CLIENT_PASSWORD", "safe-mqtt-secret")

	if err := validateProductionSecrets(); err == nil {
		t.Fatal("validateProductionSecrets() = nil, want default-secret error")
	}
}

func TestValidateProductionSecretsAllowsConfiguredProductionSecrets(t *testing.T) {
	t.Setenv("XACT_ENV", "production")
	t.Setenv("JWT_SECRET", "jwt-secret-with-enough-randomness")
	t.Setenv("NATS_INTERNAL_PASSWORD", "internal-secret-with-enough-randomness")
	t.Setenv("NATS_BROWSER_TOKEN", "browser-token-with-enough-randomness")
	t.Setenv("MQTT_PASSWORD", "mqtt-secret-with-enough-randomness")
	t.Setenv("MQTT_CLIENT_PASSWORD", "mqtt-client-secret-with-enough-randomness")

	if err := validateProductionSecrets(); err != nil {
		t.Fatalf("validateProductionSecrets() = %v, want nil", err)
	}
}

func TestEventRetentionDefaultsToAppendOnlyInProduction(t *testing.T) {
	t.Setenv("XACT_ENV", "production")

	if got := eventRetentionDays(); got != 0 {
		t.Fatalf("eventRetentionDays() = %d, want 0", got)
	}
}

func TestEventRetentionDefaultsToThirtyDaysOutsideProduction(t *testing.T) {
	if got := eventRetentionDays(); got != 30 {
		t.Fatalf("eventRetentionDays() = %d, want 30", got)
	}
}

func TestEventRetentionCanBeDisabledExplicitly(t *testing.T) {
	t.Setenv("EVENT_RETENTION_DAYS", "0")

	if got := eventRetentionDays(); got != 0 {
		t.Fatalf("eventRetentionDays() = %d, want 0", got)
	}
}

func TestEventRetentionCanBeConfigured(t *testing.T) {
	t.Setenv("XACT_ENV", "production")
	t.Setenv("EVENT_RETENTION_DAYS", "90")

	if got := eventRetentionDays(); got != 90 {
		t.Fatalf("eventRetentionDays() = %d, want 90", got)
	}
}

func TestEnvHelpers(t *testing.T) {
	t.Setenv("STR_VALUE", " value ")
	if envStringDefault("STR_VALUE", "fallback") != "value" || envStringDefault("MISSING", "fallback") != "fallback" {
		t.Fatal("envStringDefault mismatch")
	}
	t.Setenv("INT_VALUE", "42")
	t.Setenv("INT_BAD", "-1")
	if envIntDefault("INT_VALUE", 7) != 42 || envIntDefault("INT_BAD", 7) != 7 || envIntDefault("MISSING", 7) != 7 {
		t.Fatal("envIntDefault mismatch")
	}
	t.Setenv("NN_VALUE", "0")
	t.Setenv("I64_VALUE", "99")
	if envNonNegativeIntDefault("NN_VALUE", 7) != 0 || envInt64Default("I64_VALUE", 7) != 99 || envInt64Default("MISSING", 7) != 7 {
		t.Fatal("env numeric helpers mismatch")
	}
	t.Setenv("LIST_VALUE", " a, ,b ")
	list := envList("LIST_VALUE")
	if len(list) != 2 || list[0] != "a" || list[1] != "b" || envList("MISSING") != nil {
		t.Fatalf("envList = %#v", list)
	}
	for _, value := range []string{"1", "true", "yes", "y", "on"} {
		if !parseEnvBool(value, false) {
			t.Fatalf("%q should parse true", value)
		}
	}
	for _, value := range []string{"0", "false", "no", "n", "off"} {
		if parseEnvBool(value, true) {
			t.Fatalf("%q should parse false", value)
		}
	}
	if !parseEnvBool("unknown", true) || parseEnvBool("unknown", false) {
		t.Fatal("parseEnvBool fallback mismatch")
	}
}

func TestProductionModeAndSecretChecks(t *testing.T) {
	t.Setenv("APP_ENV", "prod")
	if !isProductionMode() {
		t.Fatal("APP_ENV=prod should be production")
	}
	t.Setenv("APP_ENV", "")
	t.Setenv("XACT_PRODUCTION", "true")
	if !isProductionMode() {
		t.Fatal("XACT_PRODUCTION=true should be production")
	}
	if !isKnownDefaultSecret(" change-me-now ") || isKnownDefaultSecret("long-random-value") {
		t.Fatal("default secret detection mismatch")
	}
}

func TestResolvePluginDirRandHexAndMQTTHook(t *testing.T) {
	got := mustRandHex(8)
	if len(got) != 16 {
		t.Fatalf("mustRandHex length = %d", len(got))
	}
	if _, err := hex.DecodeString(got); err != nil {
		t.Fatalf("mustRandHex invalid hex: %v", err)
	}
	if resolveDefaultPluginDir() == "" {
		t.Fatal("resolveDefaultPluginDir returned empty")
	}

	h := &MqttPasswordHook{}
	if h.ID() != "password-auth" {
		t.Fatalf("hook ID = %q", h.ID())
	}
	if !h.Provides(mqtt.OnConnectAuthenticate) || !h.Provides(mqtt.OnACLCheck) || h.Provides(0xff) {
		t.Fatal("hook Provides mismatch")
	}
	t.Setenv("MQTT_PASSWORD", "secret")
	if !h.OnConnectAuthenticate(&mqtt.Client{}, packets.Packet{Connect: packets.ConnectParams{Password: []byte("secret")}}) {
		t.Fatal("expected MQTT auth success")
	}
	if h.OnConnectAuthenticate(&mqtt.Client{}, packets.Packet{Connect: packets.ConnectParams{Password: []byte("wrong")}}) {
		t.Fatal("expected MQTT auth failure")
	}
	if !h.OnACLCheck(&mqtt.Client{}, "any/topic", true) {
		t.Fatal("ACL should allow")
	}
}
