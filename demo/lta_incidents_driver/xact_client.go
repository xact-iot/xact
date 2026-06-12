package lta_incidents_driver

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const incidentAPIKeyName = "LTA Incidents Demo"

var ErrXACTLoginUnauthorized = errors.New("login to XACT unauthorized")

type XACTClient struct {
	baseURL  string
	apiKey   string
	username string
	password string
	token    string
	tokenOrg string
	client   *http.Client
}

type nodeResponse struct {
	Name     string      `json:"name"`
	Type     string      `json:"type"`
	Children []childInfo `json:"children"`
}

type childInfo struct {
	Name      string      `json:"name"`
	Type      string      `json:"type"`
	Children  []childInfo `json:"children"`
	Value     any         `json:"value"`
	ValueType string      `json:"value_type"`
}

type apiKeyResponse struct {
	ID      int    `json:"id"`
	OrgName string `json:"orgName"`
	Name    string `json:"name"`
	Key     string `json:"key"`
}

func NewXACTClient(baseURL, apiKey, username, password string, client *http.Client) *XACTClient {
	if baseURL == "" {
		baseURL = DefaultXACTBaseURL
	}
	if client == nil {
		client = http.DefaultClient
	}
	return &XACTClient{
		baseURL:  strings.TrimRight(baseURL, "/"),
		apiKey:   strings.TrimSpace(apiKey),
		username: username,
		password: password,
		client:   client,
	}
}

func (c *XACTClient) IngestIncident(tenant, zone string, incident TrafficIncident) error {
	if err := c.ensureAPIKey(tenant); err != nil {
		return err
	}
	payload := map[string]any{
		"meta": map[string]any{
			"name": incident.DeviceName(),
			"lat":  incident.Latitude,
			"lon":  incident.Longitude,
		},
		"kpi": map[string]any{
			"type":    incident.Type,
			"message": incident.Message,
		},
	}
	return c.postJSON(c.ingestURL(tenant, zone, IncidentsDeviceType, incident.DeviceName()), payload, true)
}

func (c *XACTClient) ensureAPIKey(tenant string) error {
	if c.apiKey != "" {
		return nil
	}
	if err := c.ensureTokenForOrg(tenant); err != nil {
		return err
	}

	key, err := c.createAPIKey(incidentAPIKeyName)
	if err != nil {
		return err
	}
	if key.Key == "" {
		return fmt.Errorf("created API key %q for %s but response did not include key value", incidentAPIKeyName, tenant)
	}
	c.apiKey = key.Key
	return nil
}

func (c *XACTClient) createAPIKey(name string) (*apiKeyResponse, error) {
	body, err := json.Marshal(map[string]string{"name": name})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/api/v1/api-keys", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create API key request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.setBearer(req)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("create API key %q: %w", name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("create API key %q returned HTTP %d: %s", name, resp.StatusCode, readErrorBody(resp))
	}

	var key apiKeyResponse
	if err := json.NewDecoder(resp.Body).Decode(&key); err != nil {
		return nil, fmt.Errorf("decode created API key: %w", err)
	}
	return &key, nil
}

func (c *XACTClient) ExistingIncidents(tenant, zone string) (map[string]string, error) {
	if err := c.ensureTokenForOrg(tenant); err != nil {
		return nil, err
	}
	path := zone + "/" + IncidentsDeviceType
	req, err := http.NewRequest(http.MethodGet, c.baseURL+"/api/v1/nodes/"+path+"?depth=-1", nil)
	if err != nil {
		return nil, fmt.Errorf("create existing incidents request: %w", err)
	}
	c.setBearer(req)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("load existing incidents: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return map[string]string{}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("load existing incidents returned HTTP %d", resp.StatusCode)
	}

	var node nodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&node); err != nil {
		return nil, fmt.Errorf("decode existing incidents: %w", err)
	}
	return mapExistingIncidents(node), nil
}

func (c *XACTClient) DeleteIncidentDevice(tenant, zone, deviceName string) error {
	if err := c.ensureTokenForOrg(tenant); err != nil {
		return err
	}
	path := zone + "/" + IncidentsDeviceType + "/" + url.PathEscape(deviceName)
	req, err := http.NewRequest(http.MethodDelete, c.baseURL+"/api/v1/nodes/"+path, nil)
	if err != nil {
		return fmt.Errorf("create delete incident request: %w", err)
	}
	c.setBearer(req)

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("delete incident %s: %w", deviceName, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("delete incident %s returned HTTP %d", deviceName, resp.StatusCode)
	}
	return nil
}

func (c *XACTClient) GetIncident(tenant, zone, deviceName string) (TrafficIncident, error) {
	if err := c.ensureTokenForOrg(tenant); err != nil {
		return TrafficIncident{}, err
	}
	path := zone + "/" + IncidentsDeviceType + "/" + url.PathEscape(deviceName)
	req, err := http.NewRequest(http.MethodGet, c.baseURL+"/api/v1/nodes/"+path, nil)
	if err != nil {
		return TrafficIncident{}, fmt.Errorf("create get incident request: %w", err)
	}
	c.setBearer(req)

	resp, err := c.client.Do(req)
	if err != nil {
		return TrafficIncident{}, fmt.Errorf("get incident %s: %w", deviceName, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return TrafficIncident{}, fmt.Errorf("incident %s not found", deviceName)
	}
	if resp.StatusCode != http.StatusOK {
		return TrafficIncident{}, fmt.Errorf("get incident %s returned HTTP %d", deviceName, resp.StatusCode)
	}

	var node nodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&node); err != nil {
		return TrafficIncident{}, fmt.Errorf("decode incident %s: %w", deviceName, err)
	}
	return extractIncident(node), nil
}

func extractIncident(node nodeResponse) TrafficIncident {
	meta, _ := findChild(node.Children, "meta")
	kpi, _ := findChild(node.Children, "kpi")

	latChild, hasLat := findChild(meta.Children, "lat")
	lonChild, hasLon := findChild(meta.Children, "lon")
	lat, _ := numberValue(latChild.Value)
	lon, _ := numberValue(lonChild.Value)

	typeChild, hasType := findChild(kpi.Children, "type")
	msgChild, hasMsg := findChild(kpi.Children, "message")

	incident := TrafficIncident{Latitude: lat, Longitude: lon}
	if hasLat && hasLon {
		// no-op
	}
	if hasType {
		if s, ok := typeChild.Value.(string); ok {
			incident.Type = s
		}
	}
	if hasMsg {
		if s, ok := msgChild.Value.(string); ok {
			incident.Message = s
		}
	}
	return incident
}

func (c *XACTClient) CreateIncidentEvent(tenant, zone, deviceName, message string, params map[string]any) error {
	if err := c.ensureTokenForOrg(tenant); err != nil {
		return err
	}
	payload := map[string]any{
		"severity": "INFO",
		"device":   zone + "." + IncidentsDeviceType + "." + deviceName,
		"message":  message,
		"params":   params,
	}
	return c.postJSON(c.baseURL+"/api/v1/logs", payload, false)
}

func (c *XACTClient) ensureTokenForOrg(tenant string) error {
	if err := c.ensureToken(); err != nil {
		return err
	}
	if tenant == "" || c.tokenOrg == tenant {
		return nil
	}
	return c.switchOrg(tenant)
}

func (c *XACTClient) ensureToken() error {
	if c.token != "" {
		return nil
	}
	payload := map[string]string{
		"username": c.username,
		"password": c.password,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/login", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("login to XACT: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf("%w: HTTP %d", ErrXACTLoginUnauthorized, resp.StatusCode)
		}
		return fmt.Errorf("login to XACT returned HTTP %d", resp.StatusCode)
	}
	var login struct {
		Token string `json:"token"`
		User  struct {
			TenantID string `json:"tenant_id"`
		} `json:"user"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&login); err != nil {
		return fmt.Errorf("decode login response: %w", err)
	}
	if login.Token == "" {
		return fmt.Errorf("login response did not include a token")
	}
	c.token = login.Token
	c.tokenOrg = login.User.TenantID
	return nil
}

func (c *XACTClient) switchOrg(tenant string) error {
	body, err := json.Marshal(map[string]string{"org": tenant})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/api/v1/auth/switch-org", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create switch-org request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.setBearer(req)

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("switch to organisation %s: %w", tenant, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("switch to organisation %s returned HTTP %d: %s", tenant, resp.StatusCode, readErrorBody(resp))
	}

	var switched struct {
		Token string `json:"token"`
		User  struct {
			TenantID string `json:"tenant_id"`
		} `json:"user"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&switched); err != nil {
		return fmt.Errorf("decode switch-org response: %w", err)
	}
	if switched.Token == "" {
		return fmt.Errorf("switch-org response did not include a token")
	}
	c.token = switched.Token
	c.tokenOrg = switched.User.TenantID
	return nil
}

func (c *XACTClient) postJSON(endpoint string, payload any, apiKeyAuth bool) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create POST %s: %w", endpoint, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKeyAuth {
		req.Header.Set("Authorization", "ApiKey "+c.apiKey)
	} else {
		c.setBearer(req)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("POST %s: %w", endpoint, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("POST %s returned HTTP %d: %s", endpoint, resp.StatusCode, readErrorBody(resp))
	}
	return nil
}

func (c *XACTClient) setBearer(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.token)
}

func (c *XACTClient) ingestURL(tenant, zone, deviceType, deviceName string) string {
	return fmt.Sprintf("%s/api/v1/ingest/%s/zone/%s/%s/%s",
		c.baseURL,
		url.PathEscape(tenant),
		url.PathEscape(zone),
		url.PathEscape(deviceType),
		url.PathEscape(deviceName),
	)
}

func mapExistingIncidents(node nodeResponse) map[string]string {
	result := map[string]string{}
	for _, device := range node.Children {
		if device.Type != "node" {
			continue
		}
		meta, ok := findChild(device.Children, "meta")
		if !ok {
			if isIncidentDeviceName(device.Name) {
				result[device.Name] = device.Name
			}
			continue
		}
		latChild, hasLat := findChild(meta.Children, "lat")
		lonChild, hasLon := findChild(meta.Children, "lon")
		if !hasLat || !hasLon {
			if isIncidentDeviceName(device.Name) {
				result[device.Name] = device.Name
			}
			continue
		}
		lat, latOK := numberValue(latChild.Value)
		lon, lonOK := numberValue(lonChild.Value)
		if !latOK || !lonOK {
			if isIncidentDeviceName(device.Name) {
				result[device.Name] = device.Name
			}
			continue
		}
		result[coordinateKey(lat, lon)] = device.Name
	}
	return result
}

func isIncidentDeviceName(name string) bool {
	return strings.HasPrefix(name, "INC_")
}

func findChild(children []childInfo, name string) (childInfo, bool) {
	for _, child := range children {
		if child.Name == name {
			return child, true
		}
	}
	return childInfo{}, false
}

func numberValue(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

func readErrorBody(resp *http.Response) string {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(body))
}
