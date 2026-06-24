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
	"sync"
)

var ErrXACTManagementUnavailable = errors.New("XACT management API unavailable")

type XACTClient struct {
	baseURL   string
	apiKey    string
	client    *http.Client
	knownMu   sync.Mutex
	known     map[string]string
	incidents map[string]TrafficIncident
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

func NewXACTClient(baseURL, apiKey string, client *http.Client) *XACTClient {
	if baseURL == "" {
		baseURL = DefaultXACTBaseURL
	}
	if client == nil {
		client = http.DefaultClient
	}
	return &XACTClient{
		baseURL:   strings.TrimRight(baseURL, "/"),
		apiKey:    strings.TrimSpace(apiKey),
		client:    client,
		known:     map[string]string{},
		incidents: map[string]TrafficIncident{},
	}
}

func (c *XACTClient) IngestIncident(tenant, zone string, incident TrafficIncident) error {
	if c.apiKey == "" {
		return fmt.Errorf("XACT_API_KEY is required")
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
	if err := c.postJSON(c.ingestURL(tenant, zone, IncidentsDeviceType, incident.DeviceName()), payload); err != nil {
		return err
	}
	c.rememberIncident(incident)
	return nil
}

func (c *XACTClient) ExistingIncidents(tenant, zone string) (map[string]string, error) {
	c.knownMu.Lock()
	defer c.knownMu.Unlock()
	existing := make(map[string]string, len(c.known))
	for key, deviceName := range c.known {
		existing[key] = deviceName
	}
	return existing, nil
}

func (c *XACTClient) DeleteIncidentDevice(tenant, zone, deviceName string) error {
	if c.apiKey == "" {
		return fmt.Errorf("XACT_API_KEY is required")
	}
	if err := c.delete(c.deleteURL(tenant, zone, IncidentsDeviceType, deviceName)); err != nil {
		return err
	}
	c.forgetIncident(deviceName)
	return nil
}

func (c *XACTClient) GetIncident(tenant, zone, deviceName string) (TrafficIncident, error) {
	c.knownMu.Lock()
	defer c.knownMu.Unlock()
	if incident, ok := c.incidents[deviceName]; ok {
		return incident, nil
	}
	return TrafficIncident{}, ErrXACTManagementUnavailable
}

func (c *XACTClient) CreateIncidentEvent(tenant, zone, deviceName, message string, params map[string]any) error {
	return nil
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

func (c *XACTClient) postJSON(endpoint string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create POST %s: %w", endpoint, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "ApiKey "+c.apiKey)

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

func (c *XACTClient) delete(endpoint string) error {
	req, err := http.NewRequest(http.MethodDelete, endpoint, nil)
	if err != nil {
		return fmt.Errorf("create DELETE %s: %w", endpoint, err)
	}
	req.Header.Set("Authorization", "ApiKey "+c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("DELETE %s: %w", endpoint, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("DELETE %s returned HTTP %d: %s", endpoint, resp.StatusCode, readErrorBody(resp))
	}
	return nil
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

func (c *XACTClient) deleteURL(tenant, zone, deviceType, deviceName string) string {
	return c.ingestURL(tenant, zone, deviceType, deviceName)
}

func (c *XACTClient) rememberIncident(incident TrafficIncident) {
	deviceName := incident.DeviceName()
	c.knownMu.Lock()
	defer c.knownMu.Unlock()
	c.known[incident.CoordinateKey()] = deviceName
	c.incidents[deviceName] = incident
}

func (c *XACTClient) forgetIncident(deviceName string) {
	c.knownMu.Lock()
	defer c.knownMu.Unlock()
	if incident, ok := c.incidents[deviceName]; ok {
		delete(c.known, incident.CoordinateKey())
	}
	delete(c.incidents, deviceName)
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
