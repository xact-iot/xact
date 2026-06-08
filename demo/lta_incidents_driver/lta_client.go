package lta_incidents_driver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type LTAClient struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

func NewLTAClient(apiKey, baseURL string, client *http.Client) *LTAClient {
	if baseURL == "" {
		baseURL = DefaultLTABaseURL
	}
	if client == nil {
		client = http.DefaultClient
	}
	return &LTAClient{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  client,
	}
}

func (c *LTAClient) FetchTrafficIncidents() ([]TrafficIncident, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+"/TrafficIncidents", nil)
	if err != nil {
		return nil, fmt.Errorf("create LTA incidents request: %w", err)
	}
	req.Header.Set("AccountKey", c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch LTA incidents: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("LTA incidents returned HTTP %d", resp.StatusCode)
	}

	var data TrafficIncidentsResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("decode LTA incidents response: %w", err)
	}
	return data.Value, nil
}
