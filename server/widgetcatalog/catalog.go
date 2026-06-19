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
			widget("device-list-widget", "Device List", "list", "General", "Tabbed searchable table of devices under one or more parent nodes, with configurable columns and row click-through.", 16, 20, 10, 10, []Field{
				stringFieldDesc("headerText", "Header text", "Device List", "Widget heading shown in the card title bar. Leave blank to hide the title bar in view mode."),
				field("parentNodes", "path-list", "Device Parent Nodes", []any{}, "Each selected node becomes a tab. The node's direct children are the device rows.", nil),
				boolField("showPaging", "Show Paging Controls", false),
				numberField("pageSize", "Rows per page", 10),
				boolField("showXlsxExport", "Show CSV Export Button", false),
				stringFieldDesc("clickDashboardId", "Default row click dashboard id", "", "Dashboard opened when a row is clicked unless clickDashboards overrides the row's parent node."),
				field("clickDashboards", "string", "Per-parent row click dashboards", "{}", "JSON object mapping parent node path to dashboard id/name.", nil),
				field("columns", "object", "Columns by parent node", map[string]any{}, "Map of parent node path to column definitions. The UI also writes dynamic __col_N fields through the properties dialog.", nil),
			}, deviceListHints()),
			widget("text-widget", "Text", "text", "General", "Static text with dashboard context substitutions.", 6, 1, 2, 1, []Field{
				stringFieldDesc("text", "Text", "Text", "Supports {deviceName}, {deviceType}, {orgName}."),
				numberField("fontSize", "Font size (px)", 14),
				colorField("color", "Color", "#e2e8f0"),
				selectField("textAlign", "Text align", "left", []option{{"left", "Left"}, {"center", "Center"}, {"right", "Right"}}),
			}, textHints()),
			widget("html-widget", "HTML", "html", "General", "Sanitized HTML content block with tag and dashboard context substitution.", 8, 4, 2, 1, htmlFields(), htmlHints()),
			widget("previous-period-widget", "Previous Period", "time", "General", "Selects the previous period for the shared dashboard time range.", 6, 1, 3, 1, []Field{
				stringField("headerLabel", "Header label", "Time Period"),
			}, map[string]any{"periodButtons": []string{"1h", "3h", "6h", "12h", "24h", "48h", "7d"}, "effect": "Sets shared UI timeStart/timeEnd to now minus the selected period through now."}),
			widget("time-range-widget", "Time Range", "calendar", "General", "Edits the shared dashboard start/end time range.", 6, 1, 4, 1, []Field{
				stringField("headerText", "Header text", "Time Range"),
			}, map[string]any{"effect": "Displays and edits shared UI timeStart/timeEnd using datetime-local inputs."}),
			widget("big-number-widget", "Big Number", "number", "Metrics", "Live numeric value with optional icon, sparkline, and color bands.", 6, 5, 1, 1, append(metricBaseFields("Metric", true),
				numberField("fontSize", "Font size (px)", 56),
				numberField("decimals", "Decimal places", 2),
				boolField("showIcon", "Show icon", false),
				field("icon", "icon", "Icon", "mdi:car", "", nil),
				numberField("iconSize", "Icon size (px)", 48),
				colorField("iconColor", "Icon color", ""),
			), mergeHints(metricHints("Displays the current scalar tag value; tagPath is resolved relative to tagPrefix when set."), colorBandHints())),
			widget("gauge-widget", "Gauge", "gauge", "Metrics", "Live gauge with optional sparkline, max-value tag, and color bands.", 5, 6, 3, 4, append(metricBaseFields("Gauge", true),
				numberField("minValue", "Min value", 0),
				numberField("maxValue", "Max value (constant)", 100),
				pathField("maxTagPath", "Max value tag (overrides constant if set)", "", true),
			), mergeHints(metricHints("Displays a scalar tag as a gauge. maxTagPath is resolved like tagPath and overrides maxValue when present."), colorBandHints())),
			widget("status-table-widget", "Status Table", "table", "Metrics", "Two-column or three-column live status table with optional commands and hide condition.", 6, 8, 3, 3, []Field{
				stringField("headerText", "Header text", "Status Table"),
				pathField("tagPrefix", "Tag prefix (use * for dashboard device name)", "", false),
				pathField("hideTagPath", "Hide tag path", "", true),
				stringField("hideTagValue", "Hide when value equals", ""),
				stringField("colHeader1", "Column 1 header", ""),
				stringField("colHeader2", "Column 2 header", ""),
				stringField("colHeader3", "Column 3 header", ""),
				field("rows", "array", "Rows", []any{}, "Rows contain labels, tag paths, formatters, color bands, and optional command/value columns.", nil),
			}, statusTableHints()),
			widget("timeseries-chart-widget", "Timeseries Chart", "chart", "Metrics", "Historical line chart with up to five series.", 12, 8, 6, 4, timeseriesFields(), metricHints("Queries historical metric series for enabled seriesNTagPath fields. Use tagPrefix fields to keep configs reusable across dashboard device context.")),
			widget("binary-status-line-widget", "Binary Status Line", "status", "Metrics", "Historical binary status strip for one tag.", 12, 3, 4, 2, binaryStatusFields(), metricHints("Queries historical values for a single tag and renders truthy/active periods as a colored bar.")),
			widget("dashboard-nav-widget", "Dashboard Nav", "dashboard", "Layout", "Device-aware dashboard navigation control.", 6, 4, 3, 3, []Field{
				stringField("headerText", "Header text", "Dashboard Nav"),
				stringFieldDesc("targetDashboardId", "Target dashboard", "", "Stored as the dashboard id; blank navigates to the current dashboard."),
				pathField("deviceParentPath", "Device selection list parent node", "", false),
			}, dashboardNavHints()),
			widget("array-layout-widget", "Array Layout", "grid", "Layout", "Repeats one child widget for each child node matched by an RTDB array path.", 12, 8, 4, 3, []Field{
				pathField("arrayPath", "Array path", "", false),
				stringFieldDesc("widgetType", "Repeated widget type", "", "Built-in or plugin widget type instantiated for each matched array element."),
				field("widgetConfig", "object", "Repeated widget config", map[string]any{}, "Shared child-widget config. Each child also receives arrayElementPath.", nil),
				numberField("tileWidth", "Tile width (px)", 320),
				numberField("tileHeight", "Tile height (px)", 240),
				numberField("tileGap", "Tile gap (px)", 8),
			}, arrayLayoutHints()),
			widget("tabs-widget", "Tabs", "tabs", "Layout", "Tabbed container where each tab hosts one child widget.", 12, 8, 4, 3, []Field{
				field("tabs", "array", "Tabs", []any{}, "Array of tab entries: id, label, widgetType, widgetConfig.", nil),
				stringField("activeTabId", "Active tab id", ""),
			}, tabsHints()),
			widget("svg-diagram-widget", "SVG Diagram", "svg", "Custom", "Editable SVG/canvas-style diagram with tag-bound elements and overlay widgets.", 16, 18, 8, 8, svgDiagramFields(), svgDiagramHints()),
			systemWidget("users-widget", "User Manager", "user", "Lists users, roles, notification preferences, and supports user create/edit/password reset.", 12, 24, 8, 12, []string{"users.view", "users.manage"}, nil),
			systemWidget("organisations-widget", "Organisation Manager", "org", "Manages organisations, display names, active state, logos/favicons, geographic area, and ingest API keys.", 12, 28, 10, 18, []string{"organisations.view", "organisations.change"}, map[string]any{"usesLeaflet": true, "areaModel": "Org area is edited as map bounds/polygon-style geographic metadata."}),
			systemWidget("agentkeys-widget", "Agent Keys", "key", "Creates, lists, reveals, and deletes agent bearer tokens used by MCP clients and automation.", 16, 18, 10, 12, []string{"agentkeys.manage", "agentkeys.personal", "agentkeys.access"}, map[string]any{"agentUse": "Use generated tokens as Authorization: Bearer <token> for the embedded MCP endpoint."}),
			systemWidget("permissions-widget", "Permissions Manager", "lock", "Views and edits role permissions for registered UI resources.", 12, 20, 8, 10, []string{"permissions.view", "permissions.manage"}, map[string]any{"systemAdmin": "SystemAdmin is automatically granted all registered permissions and hidden from normal editing."}),
			widget("tags-manager-widget", "Tags Manager", "tag", "System", "RTDB tree browser and editor for nodes, tags, values, metadata, enum values, pipelines, templates, and debug tools.", 12, 16, 8, 8, []Field{
				boolField("showHeader", "Show Header", true),
			}, map[string]any{"permissions": []string{"tags/tree API permissions from server authorization"}, "configNotes": "Only showHeader is persisted as widget config; tree edits are stateful RTDB operations."}),
			systemWidget("pdf-template-widget", "PDF Reports", "pdf", "Designs PDF report templates, variables, charts, event tables, images, previews, and generated PDFs.", 12, 24, 10, 16, []string{"reports.view", "reports.manage"}, pdfReportHints()),
			widget("events-viewer-widget", "Events Viewer", "events", "System", "Queries, filters, sorts, resizes, exports, and refreshes system event records.", 20, 28, 1, 1, eventViewerFields(), map[string]any{"permissions": []string{"events.read"}, "filters": []string{"search", "severity", "start/end time", "previous period", "auto refresh"}, "export": "Loads SheetJS and exports visible records."}),
			systemWidget("notifications-widget", "Notifications", "bell", "Configures notification profiles, role/user recipients, and email/Telegram channel settings.", 12, 24, 8, 12, []string{"notifications.view", "notifications.manage"}, map[string]any{"tabs": []string{"profiles", "channels"}, "channels": []string{"email", "telegram"}}),
			systemWidget("tagcalcs-widget", "Tag Calcs", "calc", "Lists, creates, edits, tests, enables, disables, and deletes calculated tags.", 12, 24, 8, 10, []string{"tagcalcs.view", "tagcalcs.manage"}, map[string]any{"expressionFunctions": []string{"avg", "sum", "min", "max", "count", "countWhere", "listHighest", "listLowest", "abs", "round", "sqrt", "pow", "floor", "ceil", "log", "log10", "sin", "cos", "tan", "if"}}),
			widget("manual-widget", "Help Manual", "book", "System", "Displays the XACT user manual with chapter navigation and full-text search.", 24, 28, 12, 8, []Field{
				stringFieldDesc("chapter", "Chapter id", "", "Last viewed manual chapter id from /manual/manifest.json."),
			}, map[string]any{"contentSource": "/manual/manifest.json and chapter markdown files under /manual", "noAgentConfigNeeded": "Usually omit config; the widget stores chapter as the user browses."}),
			systemWidget("scheduler-widget", "Scheduler", "clock", "Manages recurring scheduled tasks, run history, report jobs, backups, commands, shell, and script tasks.", 24, 16, 12, 8, []string{"scheduler.view", "scheduler.manage"}, schedulerHints()),
		},
	}
}

func widget(t, name, icon, category, desc string, defaultW, defaultH, minW, minH int, props []Field, hints map[string]any) Widget {
	return Widget{Type: t, Name: name, Icon: icon, Category: category, Description: desc, DefaultW: defaultW, DefaultH: defaultH, MinW: minW, MinH: minH, Properties: props, ConfigHints: hints}
}

func systemWidget(t, name, icon, desc string, defaultW, defaultH, minW, minH int, permissions []string, hints map[string]any) Widget {
	merged := map[string]any{"noDashboardConfig": true}
	if len(permissions) > 0 {
		merged["permissions"] = permissions
	}
	for k, v := range hints {
		merged[k] = v
	}
	return widget(t, name, icon, "System", desc, defaultW, defaultH, minW, minH, []Field{}, merged)
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

func mergeHints(maps ...map[string]any) map[string]any {
	out := map[string]any{}
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

func textHints() map[string]any {
	return map[string]any{
		"substitutions": []string{"{deviceName}", "{deviceType}", "{orgName}"},
		"header":        "The widget body is the configured text; it does not set a card title.",
	}
}

func htmlFields() []Field {
	return []Field{
		field("html", "string", "HTML", `<div style="padding:12px;">
  <h3>{deviceName}</h3>
  <p>Organisation: {orgName}</p>
  <p>Value: {tag:meta.online}</p>
</div>`, "Sanitized HTML. Use {tag:path} placeholders for live tag values.", nil),
		pathField("tagPrefix", "Tag prefix", "", false),
		pathField("devicePath", "Device path", "", false),
		stringField("deviceName", "Device name", ""),
		stringField("deviceType", "Device type", ""),
		stringField("orgName", "Organisation name", ""),
	}
}

func htmlHints() map[string]any {
	return map[string]any{
		"substitutions": []string{"{deviceName}", "{deviceType}", "{orgName}", "{timeStart}", "{timeEnd}", "{tag:relative.or.absolute.path}"},
		"tagResolution": "When tagPrefix or devicePath is set, relative {tag:...} paths resolve below that prefix. In map side panels html-widget receives tagPrefix/devicePath/deviceName/orgName for the selected device.",
		"sanitization":  "HTML is sanitized before rendering; scripts and unsafe markup are not preserved.",
		"header":        "The card header is hidden by this widget.",
	}
}

func deviceListHints() map[string]any {
	return map[string]any{
		"columnSchema": map[string]any{
			"header":    "Column title.",
			"tagPath":   "Relative tag path below each device, or a path containing the selected parent prefix.",
			"formatter": "text, number, okfail, or cross.",
			"width":     "Optional column width.",
		},
		"defaultColumns": []map[string]string{
			{"header": "Name", "tagPath": "meta.name", "formatter": "text"},
			{"header": "Subtype", "tagPath": "meta.deviceSubtype", "formatter": "text"},
			{"header": "Online", "tagPath": "meta.online", "formatter": "okfail"},
			{"header": "In Alarm", "tagPath": "meta.commonAlarmPresent", "formatter": "cross"},
		},
		"dynamicProperties":       "After parentNodes are configured, the UI adds __click_N and __col_N fields for per-parent row-click dashboard and columns.",
		"dashboardVariationLinks": "Row click emits dashboard-open with devicePath; dashboard variation selection then uses meta.deviceSubtype, meta.subtype, or meta.variation.",
	}
}

func statusTableHints() map[string]any {
	return map[string]any{
		"formatters":    []string{"text", "number", "bar", "date/time", "icon"},
		"commandInputs": []string{"switch", "text", "number", "enum", "slider"},
		"rowSchema": map[string]any{
			"id":                   "Stable row id. Generated by UI if omitted.",
			"label":                "Left-column label.",
			"tagPath":              "Primary value tag path relative to tagPrefix unless absolute org-prefixed.",
			"formatter":            "text, number, bar, date/time, or icon.",
			"bold":                 "Boolean; bold label/value styling.",
			"invert":               "Boolean; inverts bar/color interpretation.",
			"upperLimit":           "Number used by bar formatter.",
			"colorBandsEnabled":    "Boolean; apply row color bands.",
			"colorBandsThreshold1": "First threshold.",
			"colorBandsThreshold2": "Second threshold.",
			"colorBandsColor1":     "CSS color below threshold 1.",
			"colorBandsColor2":     "CSS color between thresholds.",
			"colorBandsColor3":     "CSS color above threshold 2.",
			"col2":                 "ColumnDef for second value/command column.",
			"col3":                 "ColumnDef for optional third column; set type none to hide.",
		},
		"columnDefSchema": map[string]any{
			"type":                "none, value, or command.",
			"tagPath":             "Value tag path.",
			"formatter":           "text, number, bar, date/time, or icon.",
			"iconMap":             "Array of {value, icon, color, size, animation}; animation is none, pulse, or shake.",
			"commandPath":         "Command target tag/path for command columns.",
			"input":               "switch, text, number, enum, or slider.",
			"commandValue":        "Value written by switch/button style commands.",
			"enumOptions":         "Comma-separated enum options.",
			"min":                 "Minimum for number/slider input.",
			"max":                 "Maximum for number/slider input.",
			"timeoutSeconds":      "Command timeout.",
			"colorBandProperties": "Same color band keys as row.",
		},
		"hideCondition":  "If hideTagPath and hideTagValue are set, the widget hides when String(tag value) equals hideTagValue.",
		"tagPrefixRules": "tagPrefix supports * replacement with current dashboard device name. Map/array embedding may set tagPrefix to a selected device path.",
	}
}

func binaryStatusFields() []Field {
	return []Field{
		stringField("headerText", "Header text", "Status"),
		pathField("tagPrefix", "Tag prefix (use * for dashboard device name)", "", false),
		pathField("tagPath", "Tag path", "", true),
		colorField("barColor", "Bar color", "#22d3ee"),
		numberField("timePeriod", "History period (hours)", 24),
		boolField("useUiTimeRange", "Use UI time range", false),
		boolField("showZoomControl", "Show zoom control", true),
		selectField("refreshInterval", "Refresh interval", "0", []option{{"0", "Off"}, {"30", "30 s"}, {"60", "1 min"}, {"300", "5 min"}, {"600", "10 min"}}),
	}
}

func metricHints(summary string) map[string]any {
	return map[string]any{
		"tagResolution": "tagPath is relative to tagPrefix when tagPrefix is set. tagPrefix may contain * for the current dashboard device name.",
		"summary":       summary,
	}
}

func dashboardNavHints() map[string]any {
	return map[string]any{
		"deviceList":                  "deviceParentPath children populate the device selector.",
		"navigation":                  "Emits dashboard-open with targetDashboardId and selected devicePath. Blank targetDashboardId means current dashboard.",
		"dashboardVariationSelection": "Dashboard links automatically resolve to a matching dashboard variation using selected device meta.deviceSubtype, meta.subtype, or meta.variation.",
	}
}

func arrayLayoutHints() map[string]any {
	return map[string]any{
		"arrayPathRules":     "arrayPath supports * path-segment wildcards through the mirror store. Matched child paths are treated as array elements.",
		"childConfig":        "Each repeated child receives widgetConfig plus arrayElementPath set to the element path.",
		"configurationModel": "Use widgetType and widgetConfig for the repeated widget. The UI configures the child widget once and applies it to every element.",
	}
}

func tabsHints() map[string]any {
	return map[string]any{
		"tabEntrySchema": map[string]any{
			"id":           "Stable tab id.",
			"label":        "Tab label shown in the tab strip.",
			"widgetType":   "Built-in or plugin widget type hosted by the tab.",
			"widgetConfig": "Config object passed to the hosted widget.",
		},
		"events": "Child widget-config-save events update the owning tab entry and re-emit the full tabs config.",
	}
}

func svgDiagramFields() []Field {
	return []Field{
		stringField("title", "Title", "SVG Diagram"),
		numberField("width", "Canvas width", 1200),
		numberField("height", "Canvas height", 700),
		colorField("background", "Background", "#0f172a"),
		field("templateSvg", "object", "Template SVG", nil, "Optional {viewBox, content} imported SVG template.", nil),
		field("elements", "array", "Diagram elements", []any{}, "Editable line/shape/rect/circle/text/icon elements.", nil),
		field("widgets", "array", "Overlay widgets", []any{}, "Embedded dashboard widgets positioned over the diagram.", nil),
	}
}

func svgDiagramHints() map[string]any {
	return map[string]any{
		"elementTypes":   []string{"line", "shape", "rect", "circle", "text", "icon"},
		"bindingTargets": []string{"fill", "stroke", "text", "opacity"},
		"elementSchema": map[string]any{
			"id":           "Stable element id.",
			"type":         "line, shape, rect, circle, text, or icon.",
			"x/y/w/h":      "Position and size in diagram coordinates.",
			"x2/y2/points": "Line/shape geometry.",
			"text/icon":    "Text content or icon id.",
			"fill/stroke/strokeWidth/fontSize/opacity": "Visual style.",
			"bindings": "Array of tag bindings.",
		},
		"bindingSchema": map[string]any{
			"tagPath":      "RTDB tag path.",
			"target":       "fill, stroke, text, or opacity.",
			"defaultValue": "Value used when no rule matches.",
			"rules":        "Array of {cond,value,result}; cond is eq, ne, gt, gte, lt, or lte.",
		},
		"overlayWidgetSchema": map[string]any{"id": "Stable id", "type": "Widget type", "x/y/w/h": "Position and size", "config": "Widget config"},
	}
}

func eventViewerFields() []Field {
	return []Field{
		numberField("timestampColWidthPx", "Timestamp column width (px)", 0),
		numberField("severityColWidthPx", "Severity column width (px)", 0),
		numberField("userNameColWidthPx", "User column width (px)", 0),
		numberField("deviceColWidthPx", "Device column width (px)", 0),
		numberField("messageColWidthPx", "Message column width (px)", 0),
		numberField("paramsColWidthPx", "Params column width (px)", 0),
	}
}

func pdfReportHints() map[string]any {
	return map[string]any{
		"templateModel": "Templates are stored through the PDF reports API as name, description, templateJson, and variables.",
		"elementTypes":  []string{"text/cell", "table", "chart", "events", "image", "pie chart"},
		"chartConfig": map[string]any{
			"metrics":                             "Full org-relative tag paths; may include {{variable}} tokens.",
			"lookback":                            "1h, 6h, 24h, 7d, or 30d.",
			"title/yLabel":                        "Optional chart labels.",
			"yMin/yMax":                           "Optional numeric axis bounds.",
			"colors":                              "Optional series colors.",
			"seriesNames":                         "Optional legend labels.",
			"smooth/showLegend/fillArea/showGrid": "Boolean rendering options.",
		},
		"eventsConfig": map[string]any{
			"severity": "Empty for all or DEBUG, INFO, WARN, ERROR, CRITICAL.",
			"device":   "Optional device filter.",
			"search":   "Text search.",
			"lookback": "1h, 6h, 24h, 7d, or 30d.",
			"limit":    "Maximum event rows.",
			"columns":  "timestamp, severity, user, device, message, params.",
		},
	}
}

func schedulerHints() map[string]any {
	return map[string]any{
		"taskTypes": []string{"report", "backup", "shell", "yaegi", "command"},
		"schedule":  "Stored as a 5-field cron expression. UI offers hourly/daily/weekly/monthly presets.",
		"taskConfig": map[string]any{
			"report":  "PDF template/report generation options.",
			"backup":  "Backup task options.",
			"command": "Command target/value options, with tag browsing.",
			"shell":   "Shell command; availability depends on server safety settings.",
			"yaegi":   "Go script task; availability depends on server safety settings.",
		},
		"mcp": "Agents should prefer xact_provision_scheduler for CRUD/run/history operations.",
	}
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
