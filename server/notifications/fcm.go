package notifications

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/xact-iot/xact/events"
)

const (
	googleTokenURL = "https://oauth2.googleapis.com/token"
	fcmScope       = "https://www.googleapis.com/auth/firebase.messaging"
)

// FCMConfig holds a Firebase service-account credential used by the FCM HTTP v1 API.
type FCMConfig struct {
	ServiceAccountJSON string `json:"serviceAccountJson"`
	GoogleServicesJSON string `json:"googleServicesJson"`
}

const AndroidPackageName = "com.xact.iot.mobile"

// FirebaseClientConfig is the non-secret subset of google-services.json that
// the binary Android client needs for runtime Firebase initialization.
type FirebaseClientConfig struct {
	Configured        bool   `json:"configured"`
	ProjectID         string `json:"projectId,omitempty"`
	AppID             string `json:"appId,omitempty"`
	APIKey            string `json:"apiKey,omitempty"`
	MessagingSenderID string `json:"messagingSenderId,omitempty"`
}

// ParseFirebaseClientConfig selects XACT's Android application from a
// google-services.json document and returns only public client identifiers.
func ParseFirebaseClientConfig(data string) (FirebaseClientConfig, error) {
	if strings.TrimSpace(data) == "" {
		return FirebaseClientConfig{}, nil
	}
	var document struct {
		ProjectInfo struct {
			ProjectNumber string `json:"project_number"`
			ProjectID     string `json:"project_id"`
		} `json:"project_info"`
		Clients []struct {
			ClientInfo struct {
				MobileSDKAppID string `json:"mobilesdk_app_id"`
				Android        struct {
					PackageName string `json:"package_name"`
				} `json:"android_client_info"`
			} `json:"client_info"`
			APIKeys []struct {
				CurrentKey string `json:"current_key"`
			} `json:"api_key"`
		} `json:"client"`
	}
	if err := json.Unmarshal([]byte(data), &document); err != nil {
		return FirebaseClientConfig{}, fmt.Errorf("fcm: invalid google-services.json: %w", err)
	}
	for _, client := range document.Clients {
		if client.ClientInfo.Android.PackageName != AndroidPackageName {
			continue
		}
		apiKey := ""
		if len(client.APIKeys) > 0 {
			apiKey = client.APIKeys[0].CurrentKey
		}
		cfg := FirebaseClientConfig{
			Configured:        true,
			ProjectID:         strings.TrimSpace(document.ProjectInfo.ProjectID),
			AppID:             strings.TrimSpace(client.ClientInfo.MobileSDKAppID),
			APIKey:            strings.TrimSpace(apiKey),
			MessagingSenderID: strings.TrimSpace(document.ProjectInfo.ProjectNumber),
		}
		if cfg.ProjectID == "" || cfg.AppID == "" || cfg.APIKey == "" || cfg.MessagingSenderID == "" {
			return FirebaseClientConfig{}, fmt.Errorf("fcm: google-services.json is missing project_id, project_number, mobilesdk_app_id, or api_key")
		}
		return cfg, nil
	}
	return FirebaseClientConfig{}, fmt.Errorf("fcm: google-services.json does not contain Android package %s", AndroidPackageName)
}

type serviceAccount struct {
	ProjectID   string `json:"project_id"`
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	TokenURI    string `json:"token_uri"`
}

func (cfg FCMConfig) Validate() error {
	clientCfg, err := ParseFirebaseClientConfig(cfg.GoogleServicesJSON)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.ServiceAccountJSON) == "" {
		return nil
	}
	sa, err := parseServiceAccount(cfg.ServiceAccountJSON)
	if err != nil {
		return err
	}
	if clientCfg.Configured && clientCfg.ProjectID != sa.ProjectID {
		return fmt.Errorf("fcm: service account project %s does not match Android project %s", sa.ProjectID, clientCfg.ProjectID)
	}
	return nil
}

func parseServiceAccount(data string) (serviceAccount, error) {
	var sa serviceAccount
	if strings.TrimSpace(data) == "" {
		return sa, fmt.Errorf("fcm: service account JSON not configured")
	}
	if err := json.Unmarshal([]byte(data), &sa); err != nil {
		return sa, fmt.Errorf("fcm: invalid service account JSON: %w", err)
	}
	if sa.ProjectID == "" || sa.ClientEmail == "" || sa.PrivateKey == "" {
		return sa, fmt.Errorf("fcm: service account must include project_id, client_email, and private_key")
	}
	return sa, nil
}

// FCMSender sends Android push notifications through Firebase Cloud Messaging.
type FCMSender struct {
	cfg        FCMConfig
	client     *http.Client
	tokenURL   string
	fcmBaseURL string

	mu          sync.Mutex
	accessToken string
	tokenExpiry time.Time
}

func NewFCMSender(cfg FCMConfig) *FCMSender {
	return &FCMSender{
		cfg:        cfg,
		client:     &http.Client{Timeout: 10 * time.Second},
		tokenURL:   googleTokenURL,
		fcmBaseURL: "https://fcm.googleapis.com",
	}
}

func (s *FCMSender) Name() string { return "fcm" }

func (s *FCMSender) Send(ctx context.Context, target events.NotificationTarget, subject, body string) error {
	if strings.TrimSpace(target.FCMToken) == "" {
		return fmt.Errorf("fcm: no registration token for user %s", target.UserName)
	}
	sa, err := s.credentials()
	if err != nil {
		return err
	}
	if target.FCMProjectID != "" && target.FCMProjectID != sa.ProjectID {
		return fmt.Errorf("fcm: registration token belongs to project %s, sender is configured for %s", target.FCMProjectID, sa.ProjectID)
	}
	token, err := s.token(ctx, sa)
	if err != nil {
		return err
	}

	payload := map[string]any{"message": map[string]any{
		"token":        target.FCMToken,
		"notification": map[string]string{"title": subject, "body": body},
		"data":         map[string]string{"device": target.Device, "orgName": target.OrgName},
		"android": map[string]any{
			"priority":     "high",
			"notification": map[string]string{"channel_id": "xact_alerts"},
		},
	}}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("fcm: marshal message: %w", err)
	}
	endpoint := fmt.Sprintf("%s/v1/projects/%s/messages:send", strings.TrimRight(s.fcmBaseURL, "/"), url.PathEscape(sa.ProjectID))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("fcm: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("fcm: send: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("fcm: API returned %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	return nil
}

func (s *FCMSender) credentials() (serviceAccount, error) {
	return parseServiceAccount(s.cfg.ServiceAccountJSON)
}

func (s *FCMSender) token(ctx context.Context, sa serviceAccount) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.accessToken != "" && time.Until(s.tokenExpiry) > time.Minute {
		return s.accessToken, nil
	}

	now := time.Now()
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT"})
	claims, _ := json.Marshal(map[string]any{
		"iss": sa.ClientEmail, "scope": fcmScope, "aud": s.tokenURL,
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
	})
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(claims)
	block, _ := pem.Decode([]byte(sa.PrivateKey))
	if block == nil {
		return "", fmt.Errorf("fcm: invalid private key PEM")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("fcm: parse private key: %w", err)
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return "", fmt.Errorf("fcm: private key is not RSA")
	}
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, rsaKey, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("fcm: sign assertion: %w", err)
	}
	assertion := unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)
	form := url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {assertion},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("fcm: create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fcm: get access token: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("fcm: token endpoint returned %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("fcm: decode access token: %w", err)
	}
	if result.AccessToken == "" {
		return "", fmt.Errorf("fcm: token endpoint returned an empty access token")
	}
	s.accessToken = result.AccessToken
	s.tokenExpiry = now.Add(time.Duration(result.ExpiresIn) * time.Second)
	return s.accessToken, nil
}
