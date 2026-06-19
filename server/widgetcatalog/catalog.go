package widgetcatalog

type Field struct {
	Name        string         `json:"name"`
	Type        string         `json:"type"`
	Label       string         `json:"label"`
	Description string         `json:"description,omitempty"`
	Default     any            `json:"default,omitempty"`
	Context     map[string]any `json:"context,omitempty"`
}

type Widget struct {
	Type        string         `json:"type"`
	Name        string         `json:"name"`
	Icon        string         `json:"icon,omitempty"`
	Category    string         `json:"category"`
	Description string         `json:"description,omitempty"`
	DefaultW    int            `json:"defaultW"`
	DefaultH    int            `json:"defaultH"`
	MinW        int            `json:"minW,omitempty"`
	MinH        int            `json:"minH,omitempty"`
	Properties  []Field        `json:"properties"`
	ConfigHints map[string]any `json:"configHints,omitempty"`
}

type Catalog struct {
	Version string   `json:"version"`
	Widgets []Widget `json:"widgets"`
}

func BuiltIn() Catalog {
	return Catalog{
		Version: "1",
		Widgets: []Widget{
			widget("area-map-widget", "Area Map", "map", "General", "Interactive map view with configured device layers, markers, zoom widgets, click side panels, and map-layer plugins.", 12, 20, 8, 12, areaMapFields(), areaMapHints()),
			widget("device-list-widget", "Device List", "list", "General", "Tabbed table of devices under one or more parent nodes.", 16, 20, 10, 10, []Field{
				stringFieldDesc("headerText", "Header text", "Device List", "Widget heading shown in the card title bar. Leave blank to hide the title bar in view mode."),
				field("parentNodes", "path-list", "Device Parent Nodes", []any{}, "Each selected node becomes a tab. The node's direct children are the device rows.", nil),
				boolField("showPaging", "Show Paging Controls", false),
				numberField("pageSize", "Rows per page", 10),
			}, map[string]any{"dynamicProperties": "Per-parent-node row click dashboards and table columns are added after parentNodes are configured."}),
			widget("text-widget", "Text", "text", "General", "Static text with dashboard context substitutions.", 6, 1, 2, 1, []Field{
				stringFieldDesc("text", "Text", "Text", "Supports {deviceName}, {deviceType}, {orgName}."),
				numberField("fontSize", "Font size (px)", 14),
				colorField("color", "Color", "#e2e8f0"),
				selectField("textAlign", "Text align", "left", []option{{"left", "Left"}, {"center", "Center"}, {"right", "Right"}}),
			}, nil),
			widget("html-widget", "HTML", "html", "General", "Sanitized HTML content block.", 8, 4, 2, 1, []Field{
				field("html", "string", "HTML", "", "Sanitized before rendering.", nil),
			}, nil),
			widget("previous-period-widget", "Previous Period", "time", "General", "Selects the previous period for the shared dashboard time range.", 6, 1, 3, 1, []Field{
				stringField("headerText", "Header text", "Previous Period"),
			}, nil),
			widget("time-range-widget", "Time Range", "calendar", "General", "Edits the shared dashboard start/end time range.", 6, 1, 4, 1, []Field{
				stringField("headerText", "Header text", "Time Range"),
			}, nil),
			widget("big-number-widget", "Big Number", "number", "Metrics", "Live numeric value with optional icon, sparkline, and color bands.", 6, 5, 1, 1, append(metricBaseFields("Metric", true),
				numberField("fontSize", "Font size (px)", 56),
				numberField("decimals", "Decimal places", 2),
				boolField("showIcon", "Show icon", false),
				field("icon", "icon", "Icon", "mdi:car", "", nil),
				numberField("iconSize", "Icon size (px)", 48),
				colorField("iconColor", "Icon color", ""),
			), colorBandHints()),
			widget("gauge-widget", "Gauge", "gauge", "Metrics", "Live gauge with optional sparkline, max-value tag, and color bands.", 5, 6, 3, 4, append(metricBaseFields("Gauge", true),
				numberField("minValue", "Min value", 0),
				numberField("maxValue", "Max value (constant)", 100),
				pathField("maxTagPath", "Max value tag (overrides constant if set)", "", true),
			), colorBandHints()),
			widget("status-table-widget", "Status Table", "table", "Metrics", "Two-column or three-column live status table with optional commands and hide condition.", 6, 8, 3, 3, []Field{
				stringField("headerText", "Header text", "Status Table"),
				pathField("tagPrefix", "Tag prefix (use * for dashboard device name)", "", false),
				pathField("hideTagPath", "Hide tag path", "", true),
				stringField("hideTagValue", "Hide when value equals", ""),
				stringField("colHeader1", "Column 1 header", ""),
				stringField("colHeader2", "Column 2 header", ""),
				stringField("colHeader3", "Column 3 header", ""),
				field("rows", "array", "Rows", []any{}, "Rows contain labels, tag paths, formatters, color bands, and optional command/value columns.", nil),
			}, map[string]any{"formatters": []string{"text", "number", "bar", "date/time", "icon"}, "commandInputs": []string{"switch", "text", "number", "enum", "slider"}}),
			widget("timeseries-chart-widget", "Timeseries Chart", "chart", "Metrics", "Historical line chart with up to five series.", 12, 8, 6, 4, timeseriesFields(), nil),
			widget("binary-status-line-widget", "Binary Status Line", "status", "Metrics", "Horizontal binary state indicator for one or more tags.", 12, 3, 4, 2, []Field{
				stringField("headerText", "Header text", "Binary Status"),
				pathField("tagPrefix", "Tag prefix (use * for dashboard device name)", "", false),
				field("items", "array", "Items", []any{}, "Each item maps a tag path/value to display text and colors.", nil),
			}, nil),
			widget("dashboard-nav-widget", "Dashboard Nav", "dashboard", "Layout", "Navigation buttons for related dashboards.", 6, 4, 3, 3, []Field{
				stringField("headerText", "Header text", "Dashboard Nav"),
				field("items", "array", "Items", []any{}, "Each item references a dashboard id and display label.", nil),
			}, nil),
			widget("array-layout-widget", "Array Layout", "grid", "Layout", "Repeats child widgets over array-style nodes.", 12, 8, 4, 3, []Field{
				pathField("arrayPath", "Array path", "", false),
				field("children", "array", "Child widgets", []any{}, "Nested widget definitions rendered per array element.", nil),
			}, nil),
			widget("tabs-widget", "Tabs", "tabs", "Layout", "Tabbed container for child widgets.", 12, 8, 4, 3, []Field{
				field("tabs", "array", "Tabs", []any{}, "Each tab has a label and child widget definitions.", nil),
			}, nil),
			widget("svg-diagram-widget", "SVG Diagram", "svg", "Custom", "SVG display with tag-bound text, style, and interaction bindings.", 16, 18, 8, 8, []Field{
				stringField("headerText", "Header text", "SVG Diagram"),
				field("svg", "string", "SVG markup", "", "Inline SVG markup or imported diagram content.", nil),
				field("bindings", "array", "Bindings", []any{}, "Bindings connect SVG selectors to tag values, styles, text, or actions.", nil),
			}, nil),
			systemWidget("users-widget", "User Manager", "user", 12, 24, 8, 12),
			systemWidget("organisations-widget", "Organisation Manager", "org", 12, 28, 10, 18),
			systemWidget("agentkeys-widget", "Agent Keys", "key", 16, 18, 10, 12),
			systemWidget("permissions-widget", "Permissions Manager", "lock", 12, 20, 8, 10),
			systemWidget("tags-manager-widget", "Tags Manager", "tag", 12, 16, 8, 8),
			systemWidget("pdf-template-widget", "PDF Reports", "pdf", 12, 24, 10, 16),
			systemWidget("events-viewer-widget", "Events Viewer", "events", 20, 28, 1, 1),
			systemWidget("notifications-widget", "Notifications", "bell", 12, 24, 8, 12),
			systemWidget("tagcalcs-widget", "Tag Calcs", "calc", 12, 24, 8, 10),
			systemWidget("manual-widget", "Help Manual", "book", 24, 28, 12, 8),
			systemWidget("scheduler-widget", "Scheduler", "clock", 24, 16, 12, 8),
		},
	}
}

func widget(t, name, icon, category, desc string, defaultW, defaultH, minW, minH int, props []Field, hints map[string]any) Widget {
	return Widget{Type: t, Name: name, Icon: icon, Category: category, Description: desc, DefaultW: defaultW, DefaultH: defaultH, MinW: minW, MinH: minH, Properties: props, ConfigHints: hints}
}

func systemWidget(t, name, icon string, defaultW, defaultH, minW, minH int) Widget {
	return widget(t, name, icon, "System", "System administration widget.", defaultW, defaultH, minW, minH, []Field{}, nil)
}

type option struct {
	Value string
	Label string
}

func field(name, typ, label string, def any, desc string, ctx map[string]any) Field {
	return Field{Name: name, Type: typ, Label: label, Default: def, Description: desc, Context: ctx}
}

func stringField(name, label, def string) Field { return field(name, "string", label, def, "", nil) }
func stringFieldDesc(name, label, def, desc string) Field {
	return field(name, "string", label, def, desc, nil)
}
func numberField(name, label string, def float64) Field {
	return field(name, "number", label, def, "", nil)
}
func boolField(name, label string, def bool) Field {
	return field(name, "boolean", label, def, "", nil)
}
func colorField(name, label, def string) Field { return field(name, "color", label, def, "", nil) }

func pathField(name, label, def string, includeLeaves bool) Field {
	return field(name, "path", label, def, "", map[string]any{"includeLeaves": includeLeaves})
}

func selectField(name, label, def string, options []option) Field {
	items := make([]map[string]string, 0, len(options))
	for _, opt := range options {
		items = append(items, map[string]string{"value": opt.Value, "label": opt.Label})
	}
	return field(name, "select", label, def, "", map[string]any{"options": items})
}

func areaMapFields() []Field {
	return []Field{
		stringFieldDesc("heading", "Heading", "", "Widget heading shown in the card title bar. Leave blank to hide the title bar in view mode."),
		boolField("showSearch", "Show search", true),
		boolField("showLegend", "Show legend", true),
		numberField("baseOpacity", "Base map opacity", 1),
		field("savedBounds", "object", "Saved map bounds", nil, "Initial map bounds as north/south/east/west numbers. Null opens at the organisation area.", nil),
		stringField("tomtomApiKey", "TomTom API key", ""),
		boolField("showTraffic", "Show TomTom traffic flow", false),
		selectField("trafficStyle", "Traffic style", "relative0", []option{{"relative0", "relative0"}, {"relative0-dark", "relative0-dark"}, {"absolute", "absolute"}, {"relative", "relative"}, {"relative-delay", "relative-delay"}, {"reduced-sensitivity", "reduced-sensitivity"}}),
		boolField("showIncidents", "Show TomTom incidents", false),
		stringField("incidentStyle", "Incident style", "night"),
		field("layers", "array", "Device/map layers", []any{}, "Array of LayerConfig objects. Device layers must use pathPattern and device coordinates at meta.lat/meta.lon.", nil),
	}
}

func areaMapHints() map[string]any {
	return map[string]any{
		"layerArrayProperty": "layers",
		"deviceLayerSchema": map[string]any{
			"id":                    "string; stable unique layer id",
			"name":                  "string; shown in the map legend and layer editor",
			"pathPattern":           "string; org-relative or absolute RTDB pattern. Use * as a complete path segment to enumerate full device node names, e.g. Sites.North.Pumps.*. Do not use partial wildcards such as AQ-B-*.",
			"enabled":               "boolean; false hides the layer",
			"itemType":              "icon or plugin. Use icon for built-in device markers.",
			"pluginType":            "string; only used when itemType is plugin",
			"pluginConfig":          "object; only used when itemType is plugin",
			"defaultGlyph":          "icon id or text glyph used when no icon rule matches, e.g. mdi:map-marker",
			"defaultColor":          "CSS color for the default marker",
			"defaultSize":           "number; marker icon size in pixels",
			"offsetX":               "number; marker x offset in pixels",
			"offsetY":               "number; marker y offset in pixels",
			"iconRules":             "array of rule objects. First matching rule controls glyph/color/animation.",
			"zoomThreshold":         "number; at or above this zoom, show divTemplate or zoomWidgetType marker",
			"refreshInterval":       "number; marker refresh interval in milliseconds, 0 disables polling",
			"divTemplate":           "template literal HTML string for zoomed marker and hover tooltip. Use ${deviceName}, ${deviceDescription}, and ${tag('relative.path')}.",
			"zoomWidgetType":        "widget type rendered as zoomed marker and hover tooltip, e.g. status-table-widget",
			"zoomWidgetConfig":      "config object for zoomWidgetType",
			"divWidgetConfig":       "legacy alias used for status-table-widget zoom config",
			"divWidgetWidth":        "number; zoom widget card width in pixels",
			"zoomedMode":            "widget, status-table, or div-template. Current save path usually writes widget when a zoom widget is selected.",
			"sidePanelWidgetType":   "widget type rendered in the click side panel",
			"sidePanelWidgetConfig": "config object for sidePanelWidgetType",
			"detailDashboardId":     "string dashboard id opened when the device name in the side panel is clicked",
		},
		"iconRuleSchema": map[string]any{
			"tag":       "relative tag path under each device, or absolute org-prefixed path. Example: meta.online or status.commonAlarmPresent",
			"cond":      "one of eq, ne, gt, lt, gte, lte",
			"value":     "comparison value as a string; the widget coerces live values for comparison",
			"glyph":     "icon id or text glyph, e.g. mdi:alert-circle or !",
			"color":     "CSS color",
			"animation": "none, pulse, or shake",
			"size":      "optional number; marker size in pixels",
		},
		"coordinates": map[string]any{
			"latitudeTag":  "meta.lat",
			"longitudeTag": "meta.lon",
			"note":         "Latitude and longitude field names are not configurable. Each matched device node must have numeric meta.lat and meta.lon tag values.",
		},
		"pathPatternRules": []string{
			"pathPattern is converted through the UI store to an absolute path.",
			"If the pattern has no *, it is treated as one device path.",
			"If the pattern contains *, the widget lists child node names at that segment. The asterisk must be the whole segment between dots.",
			"To map all direct child devices under a parent node, use Parent.Path.*.",
			"Do not wildcard part of a device name. Use LA_LongBeach.AirQuality.* to match devices such as AQ-B-001, not LA_LongBeach.AirQuality.AQ-B-*.",
			"Partial name filters are not supported by area-map-widget pathPattern. If you need only some devices, place those devices under a separate parent node or create a specific layer per exact device path.",
		},
		"clickThrough": map[string]any{
			"detailDashboardId":           "Opens this dashboard id from the side panel device-name button.",
			"emittedEvent":                "dashboard-open with dashboard, id, and devicePath.",
			"deviceContext":               "Before opening or rendering side widgets, the UI store sets deviceName and deviceType from the selected devicePath.",
			"dashboardVariationSelection": "Dashboard links automatically resolve to the matching dashboard variation for the selected device. When several dashboards share the requested dashboard name, XACT compares each dashboard variation with the selected device's meta.deviceSubtype, meta.subtype, or meta.variation tag and opens the matching variation.",
			"agentGuidance":               "For detailDashboardId, link to the base dashboard/name or any dashboard in that variation group; do not create separate map rules per device subtype just to select dashboard variations.",
		},
		"embeddedWidgetConfig": map[string]any{
			"tagPrefixSubstitution": "If embedded widget config has tagPrefix containing *, * is replaced with the selected device name.",
			"statusTableDefault":    "For status-table-widget, tagPrefix defaults to the selected devicePath when omitted.",
			"htmlWidgetContext":     "html-widget receives tagPrefix, devicePath, deviceName, and orgName defaults for the selected device.",
		},
		"alarmBindingGuidance": []string{
			"There is no special alarm-state property. Model alarm styling as iconRules.",
			"Use tag meta.commonAlarmPresent, status.commonAlarmPresent, :status, or any boolean/numeric tag that exists under the device.",
			"For thresholds, use cond gt/gte/lt/lte and a numeric string value.",
		},
		"canonicalDeviceLayerExample": map[string]any{
			"id":           "pumps-layer",
			"name":         "Pumps",
			"pathPattern":  "Waterworks.Pumps.*",
			"enabled":      true,
			"itemType":     "icon",
			"defaultGlyph": "mdi:pump",
			"defaultColor": "#22c55e",
			"defaultSize":  28,
			"offsetX":      0,
			"offsetY":      0,
			"iconRules": []map[string]any{
				{"tag": "meta.commonAlarmPresent", "cond": "eq", "value": "true", "glyph": "mdi:alert-circle", "color": "#ef4444", "animation": "pulse", "size": 32},
				{"tag": "meta.online", "cond": "ne", "value": "true", "glyph": "mdi:close-circle", "color": "#6b7280", "animation": "none", "size": 28},
			},
			"zoomThreshold":   13,
			"refreshInterval": 0,
			"zoomWidgetType":  "status-table-widget",
			"zoomWidgetConfig": map[string]any{
				"headerText": "Pump Status",
				"rows": []map[string]any{
					{"label": "Flow", "tagPath": "flow", "formatter": "number"},
					{"label": "Pressure", "tagPath": "pressure", "formatter": "number"},
					{"label": "Alarm", "tagPath": "meta.commonAlarmPresent", "formatter": "text"},
				},
			},
			"divWidgetWidth":        280,
			"sidePanelWidgetType":   "status-table-widget",
			"sidePanelWidgetConfig": map[string]any{"headerText": "Device Details"},
			"detailDashboardId":     "pump-detail-dashboard-id",
		},
	}
}

func metricBaseFields(header string, sparkline bool) []Field {
	return []Field{
		stringField("headerText", "Header text", header),
		pathField("tagPrefix", "Tag prefix (use * for dashboard device name)", "", false),
		pathField("tagPath", "Tag path", "", true),
		boolField("showSparkline", "Show sparkline", sparkline),
		selectField("refreshInterval", "Refresh interval", "0", []option{{"0", "Off"}, {"30", "30 s"}, {"60", "1 min"}, {"300", "5 min"}, {"600", "10 min"}}),
	}
}

func colorBandHints() map[string]any {
	return map[string]any{
		"colorBandProperties": []Field{
			boolField("colorBandsEnabled", "Enable color bands", false),
			numberField("colorBandsThreshold1", "Color threshold 1", 50),
			numberField("colorBandsThreshold2", "Color threshold 2", 80),
			colorField("colorBandsColor1", "Color 1", "#22c55e"),
			colorField("colorBandsColor2", "Color 2", "#f59e0b"),
			colorField("colorBandsColor3", "Color 3", "#ef4444"),
		},
	}
}

func timeseriesFields() []Field {
	fields := []Field{
		stringField("headerText", "Header text", "Chart"),
		numberField("timePeriod", "History period (hours)", 24),
		boolField("useUiTimeRange", "Use UI time range", false),
		boolField("showZoomControl", "Show zoom control", true),
		boolField("showGrid", "Show grid", false),
		selectField("refreshInterval", "Refresh interval", "0", []option{{"0", "Off"}, {"30", "30 s"}, {"60", "1 min"}, {"300", "5 min"}, {"600", "10 min"}}),
		stringFieldDesc("backgroundImage", "Background image URL", "", "Optional URL to display behind the chart."),
	}
	for i := 1; i <= 5; i++ {
		prefix := "series" + string(rune('0'+i))
		fields = append(fields,
			boolField(prefix+"Enabled", "Series enabled", false),
			stringField(prefix+"Name", "Series name", "Series "+string(rune('0'+i))),
			pathField(prefix+"TagPrefix", "Series tag prefix", "", false),
			pathField(prefix+"TagPath", "Series tag path", "", true),
			colorField(prefix+"Color", "Series color", ""),
			selectField(prefix+"YAxis", "Y axis", "left", []option{{"left", "Left"}, {"right", "Right"}}),
			boolField(prefix+"GradientFill", "Gradient fill", true),
			boolField(prefix+"Smooth", "Smooth line", false),
		)
	}
	return fields
}
