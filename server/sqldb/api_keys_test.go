package sqldb

import "testing"

func TestAPIKeyHelpers(t *testing.T) {
	t.Setenv("API_KEY_HASH_SECRET", "secret-one")
	raw, err := NewRawAPIKey()
	if err != nil {
		t.Fatalf("NewRawAPIKey: %v", err)
	}
	if len(raw) != 64 {
		t.Fatalf("raw key length = %d", len(raw))
	}
	hash := HashAPIKey(" " + raw + " ")
	if hash == "" || hash != HashAPIKey(raw) {
		t.Fatalf("hash mismatch: %q", hash)
	}
	if APIKeyPrefix(" 123456789 ") != "12345678" || APIKeyPrefix("short") != "short" {
		t.Fatal("APIKeyPrefix mismatch")
	}
	if APIKeyLast4(" 123456789 ") != "6789" || APIKeyLast4("key") != "key" {
		t.Fatal("APIKeyLast4 mismatch")
	}
	for _, tt := range []struct {
		prefix string
		last4  string
		want   string
	}{
		{"", "", "redacted"},
		{"", "abcd", "...abcd"},
		{"prefix", "", "prefix..."},
		{"prefix", "abcd", "prefix...abcd"},
	} {
		if got := MaskAPIKey(tt.prefix, tt.last4); got != tt.want {
			t.Fatalf("MaskAPIKey(%q,%q)=%q want %q", tt.prefix, tt.last4, got, tt.want)
		}
	}
	if got := APIKeyPlaceholder(12, "abcdefghijklmnopqrstuvwxyz"); got != "hmac:12:abcdefghijklmnop" {
		t.Fatalf("placeholder = %q", got)
	}
}

func TestAPIKeyHashSecretFallbackOrder(t *testing.T) {
	t.Setenv("API_KEY_HASH_SECRET", "")
	t.Setenv("JWT_SECRET", "jwt-secret")
	if apiKeyHashSecret() != "jwt-secret" {
		t.Fatal("JWT secret fallback not used")
	}
	t.Setenv("JWT_SECRET", "")
	if apiKeyHashSecret() != apiKeyHashSecretFallback {
		t.Fatal("development fallback not used")
	}
}
