package water_driver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Client sends device telemetry to the XACT REST ingest API.
type Client struct {
	serverURL string
	apiKey    string
	http      *http.Client
}

// NewClient creates a REST client targeting serverURL with the given API key.
func NewClient(serverURL, apiKey string) *Client {
	return &Client{
		serverURL: strings.TrimRight(serverURL, "/"),
		apiKey:    strings.TrimSpace(apiKey),
		http:      &http.Client{Timeout: 10 * time.Second},
	}
}

// Post sends a telemetry payload for one device to /api/v1/ingest/{org}/zone/{zone}/{deviceType}/{deviceName}.
func (c *Client) Post(deviceType, deviceName string, payload map[string]any) error {
	if c.apiKey == "" {
		return fmt.Errorf("XACT_API_KEY is required")
	}
	_, err := c.postWithAPIKey(deviceType, deviceName, payload)
	return err
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

func (c *Client) ingestURL(deviceType, deviceName string) string {
	return fmt.Sprintf("%s/api/v1/ingest/default/zone/WaterWorks/%s/%s", c.serverURL, deviceType, deviceName)
}
