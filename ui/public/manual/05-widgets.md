# Widgets

Widgets are the building blocks of XACT dashboards. Each widget displays data or provides functionality in a specific way. This chapter covers every widget type available in the system.

## Widget Categories

| Category | Purpose | Widgets |
|----------|---------|---------|
| **General** | Common display widgets | Text, Device List, Area Map |
| **Metrics** | Data visualisation and charting | Big Number, Gauge, Sparkline, Time Series Chart, Status Table |
| **Layout** | Organising content | Tabs, Array Layout |
| **System** | Administration and configuration | Tags Manager, Users, Organisations, Permissions, Events Viewer, Tag Calcs, Notifications, PDF Reports, Help Manual |
| **Custom** | Plugin-provided widgets | Varies - loaded dynamically from the server |

## Common Widget Features

### Widget Header

Most widgets display a header bar showing the widget title. In edit mode the header also shows:

- **Gear icon** - opens the widget configuration dialog
- **X icon** - removes the widget from the dashboard (with confirmation)

Some widgets (Text, Tabs, Help Manual) hide their header for a full-bleed appearance.

### Resizing

Drag the bottom-right corner of any widget to resize it. Widgets snap to the 24-column grid and respect minimum size constraints specific to each widget type.

### Real-Time Updates

Widgets that display tag data subscribe to the NATS real-time data store. Values update automatically as new data arrives from connected devices - no page refresh is required.

### Tag Prefix

Many widgets support a **Tag Prefix** configuration. This is prepended to all tag paths within the widget. When the prefix contains `*`, it is replaced with the current dashboard/UI `deviceName` variable - allowing a single widget configuration to display data for whichever device the user selects.

---

## General Widgets

### Text Widget

Renders formatted text content within a dashboard. Useful for headings, descriptions, instructions, or dynamic labels.

**Features:**
- Supports dashboard/UI variables: `{deviceName}`, `{deviceType}`, `{orgName}` - these are replaced with the current UI context values at render time
- Configurable font size, text colour, and text alignment
- Hides the widget header for a clean, full-bleed appearance

**Configuration:**

| Property | Description |
|----------|-------------|
| Text | The text content to display. Use `{deviceName}`, `{deviceType}`, or `{orgName}` for dynamic values. |
| Font Size | Text size in pixels |
| Colour | Text colour |
| Alignment | Left, centre, or right |

---

### Device List Widget

A tabular display of devices with live tag values. Designed for fleet overview - showing many devices at once with key status indicators.

**Features:**
- Devices are organised into **tabs** by their parent node in the tag tree
- Each tab has independently configurable columns
- Live values update in real time via RTDB subscriptions
- Sorting by clicking column headers
- Fuzzy search across device names
- Pagination for large device lists
- CSV export of the current view
- Click a device row to navigate to a configured dashboard

**Configuration:**

Each tab is configured separately with:

| Property | Description |
|----------|-------------|
| Header | The column heading text |
| Tag Path | Path to the tag value relative to each device |
| Formatter | How to render the value: `text`, `number`, `okfail` (tick/cross), `cross` (cross only), `bar` (progress bar) |

---

### Area Map Widget

An interactive geographic map displaying device locations with live status indicators. Built on OpenStreetMap with the Leaflet library.

**Features:**
- Device markers displayed on a zoomable, pannable map
- **Icon markers** - show a glyph (icon) with conditional colouring based on tag values
- **Div template markers** - render custom HTML overlays at high zoom levels, with access to device tag values
- **Layers** - group devices by tag path patterns for organised display
- **Animations** - markers can pulse or shake to draw attention to alarm conditions
- **Click interaction** - clicking a device marker opens a side panel showing device details (either a status table or a list of tag values)
- **Detail dashboard link** - each map layer can select a dashboard; when set, the side-panel device name links to that dashboard and carries the clicked device context. If the selected dashboard has device subtype variations, XACT opens the matching subtype dashboard automatically.
- Supports TomTom traffic overlay integration
- Default map bounds set from the organisation's geographic area configuration

**Icon Rules:**

Icon rules determine the appearance of device markers based on live tag values:

| Field | Description |
|-------|-------------|
| Tag | The tag path to evaluate (relative to the device) |
| Condition | Comparison: `eq`, `ne`, `gt`, `lt`, `gte`, `lte` |
| Value | The comparison value |
| Glyph | The icon to display when the condition matches |
| Colour | The marker colour when the condition matches |
| Animation | `none`, `pulse`, or `shake` |

Rules are evaluated in order - the first matching rule determines the marker appearance.

**Div Templates:**

At higher zoom levels, markers can switch to custom HTML rendered from a template. Templates have access to variables: `deviceName`, `deviceDescription`, and individual tag values. Use JavaScript template literal syntax within the HTML.

**Plugin Items:**

The map supports extensible item types via the plugin system. Custom renderers can be registered using `window.XACT.registerMapItemType(name, renderer)`.

**Subtype-aware detail dashboards:**

An overview map can show many devices of the same operational family on one layer even when the devices come from different suppliers or have different internal tag layouts. For example, an Air Quality layer can show all AQ sensors on one consolidated map. Configure the layer's **Detail Dashboard** to point at the shared detail dashboard name, such as `Air Quality Device`. When a user clicks a marker, XACT sets the clicked device as the current `deviceName`; when the device name in the side panel is clicked, dashboard selection uses that device's subtype to open the correct detail dashboard variation.

---

## Metrics Widgets

### Big Number Widget

Displays a single tag value in large, prominent text. Ideal for at-a-glance monitoring of critical values on operational dashboards.

**Features:**
- Large, configurable font size for visibility at a distance
- Flash animation when the value changes
- Optional **sparkline** - a mini time-series chart displayed below the number showing recent history
- **Colour bands** - threshold-based colouring (e.g. green for normal, yellow for warning, red for alarm)
- Tag prefix with `*` wildcard for device name substitution

**Configuration:**

| Property | Description |
|----------|-------------|
| Tag Path | The tag to display |
| Label | Descriptive text below the value |
| Unit | Appended to the value (e.g. "C", "bar", "%") |
| Font Size | Size of the number display |
| Colour Bands | Threshold ranges with associated colours |
| Show Sparkline | Toggle the mini chart on/off |

---

### Gauge Widget

A circular gauge that displays a numeric tag value on an arc with a needle indicator.

**Features:**
- SVG-rendered gauge with smooth needle animation
- Configurable minimum and maximum values
- Optional second tag for a dynamic maximum value
- Colour bands with threshold zones displayed on the arc
- Fade animation on value updates

**Configuration:**

| Property | Description |
|----------|-------------|
| Tag Path | The tag to display |
| Min | Minimum scale value |
| Max | Maximum scale value (or a tag path for dynamic max) |
| Label | Text displayed below the gauge |
| Unit | Unit label |
| Colour Bands | Threshold ranges shown as coloured arcs |

---

### Sparkline Widget

A compact line chart showing recent historical data for a single tag. Useful for embedding small trend indicators alongside other content.

**Features:**
- Hover tooltip showing value, timestamp, and units
- Auto-refresh on a configurable interval
- Up to 300 historical data points
- Gradient fill beneath the line
- Responsive - adapts to the widget size

**Configuration:**

| Property | Description |
|----------|-------------|
| Tag Path | The tag to chart |
| Period | How far back to display (in hours) |
| Refresh Interval | How often to fetch new data |

---

### Time Series Chart Widget

A full-featured multi-series line chart built on ECharts. The primary tool for analysing historical trends and comparing metrics over time.

**Features:**
- Up to **5 data series** on a single chart
- Independent left and right Y-axes
- Per-series configuration: name, colour, axis assignment, gradient fill
- Time range selection (hours of historical data)
- Optional synchronisation with the global UI time range
- Auto-refresh interval
- Interactive legend, crosshair tooltip, and zoom
- Optional background image
- Responsive to widget resizing

**Configuration (per series):**

| Property | Description |
|----------|-------------|
| Enabled | Toggle the series on/off |
| Name | Legend label for this series |
| Tag Path | The metric tag to plot |
| Colour | Line and fill colour |
| Y-Axis | Left or Right axis |
| Gradient | Enable gradient fill beneath the line |

**Global settings:**

| Property | Description |
|----------|-------------|
| Period | Hours of history to display |
| Use UI Time Range | Sync with the global time range selector |
| Refresh Interval | Auto-refresh frequency |
| Background Image | Optional image URL |

---

### Status Table Widget

A compact table displaying labels, live tag values, and optional command controls. It is useful both as a read-only device summary and as a small control panel for common operator actions.

**Features:**
- Each row shows a label plus up to two configurable columns
- Columns can be unused, show a live value, or show a command input with an **Execute** button
- Value formatters include `text`, `number`, `bar`, `date/time`, and icon mapping
- Command inputs include switch, text, number, enum dropdown, and slider controls
- Command execution uses the standard XACT NATS command protocol and records the result in the Events table
- Per-row **colour bands** for visual threshold indication
- Bold styling option for row labels
- **Hide condition** - hide the entire widget based on a tag value (useful for conditional visibility)
- Tag prefix support, including `*` substitution for the currently selected device

**Configuration (per row):**

| Property | Description |
|----------|-------------|
| Label | Row label text |
| Column 2 / Column 3 Type | `unused`, `value`, or `command` |
| Tag Path | For value columns, the tag to display; for command columns, the relative path sent in the command payload |
| Formatter | Display format for value columns |
| Input Field | Control type for command columns: switch, text, number, enum, or slider |
| Default Value | Initial command value shown in the control |
| Enum Options | Comma-separated options for enum command inputs |
| Min / Max | Bounds for number and slider command inputs |
| Timeout | Seconds to wait for the driver response before marking the command failed |
| Bold | Whether to bold the label |
| Colour Bands | Threshold-based background colours |

For command controls, configure the widget **Tag Prefix** to the device path. The command subject is built as `xact.command.{org}.{tag-prefix}`, and the command column's tag path becomes the relative command path in the JSON payload.

---

## Layout Widgets

### Tabs Widget

A container widget that holds other widgets in a tabbed interface. Each tab displays a single child widget. This is useful for packing multiple views into a compact space.

**Features:**
- Tab bar across the top for switching between child widgets
- In edit mode: add, remove, and reorder tabs
- Each tab stores its child widget configuration independently
- Gear button opens a full tab management overlay
- Hides the widget header - the tabs themselves serve as navigation

**Configuration:**

Each tab has:

| Property | Description |
|----------|-------------|
| Label | The tab title |
| Widget Type | The child widget to display in this tab |
| Widget Config | The child widget's own configuration |

---

### Array Layout Widget

A container widget that repeats a child widget for each element in an RTDB array node. Children are laid out left-to-right and wrap at the widget edge. This is ideal for displaying a fleet of identical devices - such as a row of status tables or time series charts, one per device.

**Features:**
- Automatically discovers array elements from the configured RTDB path
- Supports wildcard (`*`) patterns in the array path
- Each repeated tile shows the element name in a header bar
- In edit mode, only the first tile shows the gear icon for configuring the child widget
- Child widget configuration is shared across all tiles - each tile receives the same settings plus an `arrayElementPath` and `tagPrefix` override pointing to its specific RTDB sub-tree
- Reacts to tree structure changes - tiles are added or removed automatically as array elements appear or disappear in real time
- Configurable tile dimensions and spacing

**Configuration:**

| Property | Description |
|----------|-------------|
| Array Path | Dot-separated path to an RTDB array node. Wildcards (`*`) supported. |
| Widget Type | The child widget to repeat for each array element |
| Tile Width | Width of each tile in pixels (default 320) |
| Tile Height | Height of each tile in pixels (default 240) |
| Gap | Spacing between tiles in pixels (default 8) |

**How child widgets receive context:**

Each child widget instance receives the shared configuration plus three additional properties:

| Property | Description |
|----------|-------------|
| `arrayElementPath` | Full dot-path to this specific array element |
| `arrayElementName` | The element's name (last segment of the path) |
| `tagPrefix` | Same as `arrayElementPath` - used by widgets that support tag prefix substitution |

---

## System Widgets

System widgets provide administration and configuration capabilities. They are typically placed on dedicated system dashboards that only administrators and managers can access.

### Tags Manager Widget

A comprehensive tree browser and editor for the RTDB tag hierarchy. See the [Tags & Real-Time Data](#tags) chapter for detailed information about the tag system.

**Features:**
- Hierarchical tree browser with expand/collapse
- Search and filter by tag name or path
- View live tag values with status indicators:
  - **U** - unavailable (no value received yet)
  - **S** - stale (no update within timeout)
  - **A** - alarm (limit exceeded)
  - **D** - disabled
- Create, edit, and delete **nodes** (containers) and **tags** (data points)
- **Node editor** - configure name, description, type (Standard, Device, or Organisation), and template
- **Tag editor** - configure path, description, units, data type (integer, float, string, boolean, enum), and min/max values
- **Pipeline editor** - build and configure processing block chains with drag-and-drop. See [Processing Blocks](#tags) for details.
- **Pipeline debugger** - test a pipeline with a value and see step-by-step output
- Lock state tracking for concurrent editing protection

---

### Users Widget

User account administration. See [Users & Permissions](#users-permissions) for details.

---

### Organisations Widget

Organisation management and API key administration. See [Organisations](#organisations) for details.

---

### Permissions Widget

Role-based access control configuration. See [Users & Permissions](#users-permissions) for details.

---

### Events Viewer Widget

A filterable, sortable log of system events. See the [Events](#events) chapter for details.

**Features:**
- Filter by time range, severity level, device name, and free-text search
- Sortable columns: timestamp, severity, user, device, message
- Pagination with configurable page size
- CSV export of filtered results
- Auto-refresh interval (Off, 10s, 30s, 1m, 5m)
- Incremental loading - fetches only new events since the last update

---

### Tag Calcs Widget

Create and manage computed tag expressions. See the [Tag Calcs](#tag-calcs) chapter for comprehensive documentation.

**Features:**
- List all tag calculations with name, description, output tag, expression, interval, and enabled status
- Create and edit tag calcs with a syntax-highlighted expression editor
- Test expressions against live data before enabling
- Enable/disable individual calculations without deleting them

---

### Notifications Widget

Configure notification channels and profiles. See the [Notifications](#notifications) chapter for details.

**Features:**
- **Channels tab** - configure email (SMTP settings) and Telegram (bot token) delivery channels
- **Profiles tab** - create notification profiles that define who receives alerts by role or individual user

---

### PDF Reports Widget

Design and generate PDF report templates. See the [PDF Reports](#reports) chapter for details.

---

### Help Manual Widget

Displays this user manual within the XACT interface. Features a table-of-contents sidebar, full-text search across all chapters, and sub-heading navigation. The last-viewed chapter is remembered between sessions.

---

## Time Range Widget

A date/time range picker that sets the global UI time range. When placed on a dashboard, it allows users to control the time window used by time series charts and other time-aware widgets.

Charts configured with **Use UI Time Range** enabled will synchronise with the selection made in this widget.

---

## Plugin Widgets

XACT supports custom widgets loaded as JavaScript plugins. Plugin files are placed in the `plugins/widgets/` directory on the server and automatically discovered at startup.

Plugin widgets appear under the **Custom** category in the widget toolbar. They participate in the standard widget lifecycle - configuration, persistence, permissions, and real-time data subscriptions - just like built-in widgets.

See the [Plugins](#plugins) chapter in the developer section for information on creating custom widgets.
