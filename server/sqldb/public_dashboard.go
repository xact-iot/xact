package sqldb

import (
	"encoding/json"
	"errors"
	"fmt"
)

// PublicDashboard returns a copy containing only widget types and settings that
// can be rendered in the anonymous read-only view.
func PublicDashboard(d *Dashboard) (*Dashboard, error) {
	if d == nil || !d.IsPublic || d.IsCategory || d.Permission != "" {
		return nil, errors.New("dashboard is not public")
	}
	var widgets []publicWidget
	widgetsJSON := d.Widgets
	if len(widgetsJSON) == 0 {
		widgetsJSON = json.RawMessage("[]")
	}
	if err := json.Unmarshal(widgetsJSON, &widgets); err != nil {
		return nil, fmt.Errorf("invalid dashboard widgets: %w", err)
	}
	if len(widgets) > 100 {
		return nil, errors.New("public dashboards support at most 100 widgets")
	}
	safe := make([]publicWidget, 0, len(widgets))
	for _, widget := range widgets {
		config, err := publicWidgetConfig(widget.Type, widget.Config)
		if err != nil {
			return nil, err
		}
		widget.Config = config
		safe = append(safe, widget)
	}
	encoded, err := json.Marshal(safe)
	if err != nil {
		return nil, err
	}
	copy := *d
	copy.Widgets = encoded
	return &copy, nil
}

type publicWidget struct {
	ID     string                     `json:"id"`
	Type   string                     `json:"type"`
	X      int                        `json:"x"`
	Y      int                        `json:"y"`
	W      int                        `json:"w"`
	H      int                        `json:"h"`
	Config map[string]json.RawMessage `json:"config"`
}

func publicWidgetConfig(kind string, config map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	if config == nil {
		config = map[string]json.RawMessage{}
	}
	var fields []string
	switch kind {
	case "text-widget":
		fields = []string{"text", "fontSize", "color", "textAlign"}
	case "big-number-widget":
		fields = []string{"headerText", "tagPrefix", "tagPath", "fontSize", "decimals", "showIcon", "icon", "iconSize", "iconColor", "colorBandsEnabled", "colorBandsThreshold1", "colorBandsThreshold2", "colorBandsColor1", "colorBandsColor2", "colorBandsColor3"}
	case "gauge-widget":
		fields = []string{"headerText", "tagPrefix", "tagPath", "minValue", "maxValue", "maxTagPath", "colorBandsEnabled", "colorBandsThreshold1", "colorBandsThreshold2", "colorBandsColor1", "colorBandsColor2", "colorBandsColor3"}
	case "area-map-widget":
		fields = []string{"heading", "showSearch", "showLegend", "baseOpacity", "savedBounds"}
	default:
		return nil, fmt.Errorf("widget %q is not supported on public dashboards", kind)
	}
	safe := make(map[string]json.RawMessage, len(fields)+1)
	for _, key := range fields {
		if value, ok := config[key]; ok {
			safe[key] = value
		}
	}
	if kind == "big-number-widget" || kind == "gauge-widget" {
		safe["showSparkline"] = json.RawMessage("false")
		safe["refreshInterval"] = json.RawMessage("0")
	}
	if kind == "area-map-widget" {
		safe["showTraffic"] = json.RawMessage("false")
		safe["showIncidents"] = json.RawMessage("false")
		var layers []map[string]json.RawMessage
		if raw, ok := config["layers"]; ok {
			if err := json.Unmarshal(raw, &layers); err != nil {
				return nil, fmt.Errorf("invalid map layers: %w", err)
			}
		}
		if len(layers) > 50 {
			return nil, errors.New("public maps support at most 50 layers")
		}
		filtered := make([]map[string]json.RawMessage, 0, len(layers))
		allowedLayer := map[string]bool{
			"id": true, "name": true, "pathPattern": true, "enabled": true, "itemType": true,
			"iconRules": true, "defaultGlyph": true, "defaultColor": true, "defaultSize": true,
			"iconRotationEnabled": true, "iconRotationTag": true, "zoomThreshold": true,
			"showZoomedTooltipAlways": true,
			"offsetX":                 true, "offsetY": true,
		}
		for _, layer := range layers {
			var itemType string
			_ = json.Unmarshal(layer["itemType"], &itemType)
			if itemType != "" && itemType != "icon" {
				return nil, errors.New("map plugin layers are not supported on public dashboards")
			}
			clean := make(map[string]json.RawMessage)
			for key, value := range layer {
				if allowedLayer[key] {
					clean[key] = value
				}
			}
			filtered = append(filtered, clean)
		}
		encoded, _ := json.Marshal(filtered)
		safe["layers"] = encoded
	}
	return safe, nil
}
