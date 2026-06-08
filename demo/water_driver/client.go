package water_driver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const waterAPIKeyName = "Waterworks Demo"

// Client sends device telemetry to the XACT REST ingest API.
type Client struct {
	serverURL string
	apiKey    string
	username  string
	password  string
	token     string
	http      *http.Client
}

// NewClient creates a REST client targeting serverURL with the given API key.
func NewClient(serverURL, apiKey string) *Client {
	return &Client{
		serverURL: strings.TrimRight(serverURL, "/"),
		apiKey:    strings.TrimSpace(apiKey),
		username:  "admin",
		password:  "admin",
		http:      &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *Client) SetCredentials(username, password string) {
	if username != "" {
		c.username = username
	}
	if password != "" {
		c.password = password
	}
}

// Post sends a telemetry payload for one device to /api/v1/ingest/{org}/zone/{zone}/{deviceType}/{deviceName}.
func (c *Client) Post(deviceType, deviceName string, payload map[string]any) error {
	if err := c.ensureAPIKey(); err != nil {
		return err
	}
	status, err := c.postWithAPIKey(deviceType, deviceName, payload)
	if err == nil {
		return nil
	}
	if status != http.StatusUnauthorized {
		return err
	}
	c.apiKey = ""
	if keyErr := c.ensureAPIKey(); keyErr != nil {
		return fmt.Errorf("%w; refresh API key: %v", err, keyErr)
	}
	_, retryErr := c.postWithAPIKey(deviceType, deviceName, payload)
	return retryErr
}

func (c *Client) postWithAPIKey(deviceType, deviceName string, payload map[string]any) (int, error) {
	url := c.ingestURL(deviceType, deviceName)
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("marshal %s/%s: %w", deviceType, deviceName, err)
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("request %s/%s: %w", deviceType, deviceName, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "ApiKey "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("post %s/%s: %w", deviceType, deviceName, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return resp.StatusCode, fmt.Errorf("post %s/%s: HTTP %d", deviceType, deviceName, resp.StatusCode)
	}
	return resp.StatusCode, nil
}

func (c *Client) ensureAPIKey() error {
	if c.apiKey != "" {
		return nil
	}
	if err := c.ensureToken(); err != nil {
		return err
	}
	key, err := c.createAPIKey(waterAPIKeyName)
	if err != nil {
		return err
	}
	if key == "" {
		return fmt.Errorf("created API key %q but response did not include key value", waterAPIKeyName)
	}
	c.apiKey = key
	return nil
}

func (c *Client) ensureToken() error {
	if c.token != "" {
		return nil
	}
	body, err := json.Marshal(map[string]string{
		"username": c.username,
		"password": c.password,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, c.serverURL+"/login", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("login to XACT: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("login to XACT returned HTTP %d: %s", resp.StatusCode, readErrorBody(resp))
	}
	var login struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&login); err != nil {
		return fmt.Errorf("decode login response: %w", err)
	}
	if login.Token == "" {
		return fmt.Errorf("login response did not include a token")
	}
	c.token = login.Token
	return nil
}

func (c *Client) createAPIKey(name string) (string, error) {
	body, err := json.Marshal(map[string]string{"name": name})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequest(http.MethodPost, c.serverURL+"/api/v1/api-keys", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create API key request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("create API key %q: %w", name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("create API key %q returned HTTP %d: %s", name, resp.StatusCode, readErrorBody(resp))
	}

	var key struct {
		Key string `json:"key"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&key); err != nil {
		return "", fmt.Errorf("decode created API key: %w", err)
	}
	return strings.TrimSpace(key.Key), nil
}

func (c *Client) ingestURL(deviceType, deviceName string) string {
	return fmt.Sprintf("%s/api/v1/ingest/default/zone/WaterWorks/%s/%s", c.serverURL, deviceType, deviceName)
}

func readErrorBody(resp *http.Response) string {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return err.Error()
	}
	text := strings.TrimSpace(string(body))
	if text == "" {
		return resp.Status
	}
	return text
}
