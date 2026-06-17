package sqldb

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
)

const apiKeyHashSecretFallback = "xact-development-api-key-hash-secret"
const encryptedSecretPrefix = "v1:"

func NewRawAPIKey() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func NewRawAgentToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "xat_" + hex.EncodeToString(raw[:]), nil
}

func HashAPIKey(raw string) string {
	key := apiKeyHashSecret()
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(strings.TrimSpace(raw)))
	return hex.EncodeToString(mac.Sum(nil))
}

func HashAgentToken(raw string) string {
	key := apiKeyHashSecret()
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte("agent:"))
	mac.Write([]byte(strings.TrimSpace(raw)))
	return hex.EncodeToString(mac.Sum(nil))
}

func EncryptAgentToken(raw string) (string, error) {
	block, err := aes.NewCipher(agentTokenEncryptionKey())
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(strings.TrimSpace(raw)), nil)
	return encryptedSecretPrefix + base64.StdEncoding.EncodeToString(ciphertext), nil
}

func DecryptAgentToken(encoded string) (string, error) {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return "", fmt.Errorf("token secret is not available")
	}
	if !strings.HasPrefix(encoded, encryptedSecretPrefix) {
		return "", fmt.Errorf("unsupported token secret format")
	}
	payload, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(encoded, encryptedSecretPrefix))
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(agentTokenEncryptionKey())
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(payload) < gcm.NonceSize() {
		return "", fmt.Errorf("token secret is malformed")
	}
	nonce := payload[:gcm.NonceSize()]
	ciphertext := payload[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
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

func agentTokenEncryptionKey() []byte {
	if secret := strings.TrimSpace(os.Getenv("AGENT_TOKEN_ENCRYPTION_SECRET")); secret != "" {
		sum := sha256.Sum256([]byte(secret))
		return sum[:]
	}
	if secret := strings.TrimSpace(os.Getenv("JWT_SECRET")); secret != "" {
		sum := sha256.Sum256([]byte(secret))
		return sum[:]
	}
	sum := sha256.Sum256([]byte(apiKeyHashSecret()))
	return sum[:]
}
