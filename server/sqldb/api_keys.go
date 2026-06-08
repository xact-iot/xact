package sqldb

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
)

const apiKeyHashSecretFallback = "xact-development-api-key-hash-secret"

func NewRawAPIKey() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func HashAPIKey(raw string) string {
	key := apiKeyHashSecret()
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(strings.TrimSpace(raw)))
	return hex.EncodeToString(mac.Sum(nil))
}

func APIKeyPrefix(raw string) string {
	raw = strings.TrimSpace(raw)
	if len(raw) <= 8 {
		return raw
	}
	return raw[:8]
}

func APIKeyLast4(raw string) string {
	raw = strings.TrimSpace(raw)
	if len(raw) <= 4 {
		return raw
	}
	return raw[len(raw)-4:]
}

func MaskAPIKey(prefix, last4 string) string {
	prefix = strings.TrimSpace(prefix)
	last4 = strings.TrimSpace(last4)
	if prefix == "" && last4 == "" {
		return "redacted"
	}
	if prefix == "" {
		return "..." + last4
	}
	if last4 == "" {
		return prefix + "..."
	}
	return prefix + "..." + last4
}

func APIKeyPlaceholder(id int, hash string) string {
	if len(hash) > 16 {
		hash = hash[:16]
	}
	return fmt.Sprintf("hmac:%d:%s", id, hash)
}

func apiKeyHashSecret() string {
	if secret := strings.TrimSpace(os.Getenv("API_KEY_HASH_SECRET")); secret != "" {
		return secret
	}
	if secret := strings.TrimSpace(os.Getenv("JWT_SECRET")); secret != "" {
		return secret
	}
	return apiKeyHashSecretFallback
}
