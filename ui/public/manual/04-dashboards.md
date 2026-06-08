# Dashboards

Dashboards are the primary way to visualise and interact with your data in XACT. Each dashboard is a grid-based canvas where you can place, resize, and configure **widgets** to build custom monitoring views.

## Concepts

- A **dashboard** is a named page in the sidebar. It can contain widgets or provide a built-in system view.
- A **category** is a folder that groups related dashboards together in the sidebar.
- **Widgets** are the individual components placed on a dashboard - charts, tables, gauges, maps, and more.

Dashboards are shared across all users within an organisation. Any user with edit permissions can modify the layout, and all other users see the changes immediately on their next visit.

## Creating a New Dashboard

1. Navigate to the **Dashboards** configuration view (accessible from the sidebar if you have the `dashboards-setup.edit` permission).
2. Click **Add Dashboard**.
3. Enter a name for your new dashboard.
4. Optionally assign it to a category, set an icon, and configure its sort order.
5. Save your changes.

The new dashboard appears in the sidebar. Click it to open it, and enter edit mode to start adding widgets.

## Organising Dashboards

The Dashboards configuration view lets you organise your sidebar navigation:

### Categories

Categories are collapsible groups in the sidebar that contain dashboards. To create a category:

1. Open the **Dashboards** configuration view.
2. Click **Add Category**.
3. Enter a name - this becomes the group heading in the sidebar.

Drag dashboards into categories to group them logically (e.g. "Operations", "Administration", "Reports").

### Dashboard Properties

Each dashboard has the following configurable properties:

| Property | Description |
|----------|-------------|
| **Name** | The dashboard title shown in the sidebar |
| **Description** | Optional description for administrators |
| **Icon** | An icon displayed next to the dashboard name in the sidebar (supports Iconify icon syntax such as `mdi:icon-name` or `material-symbols:icon-name`) |
| **Device Type** | Optional device type associated with this dashboard |
| **Device Subtype** | Optional device subtype, also called variation, associated with this dashboard |
| **Permission** | The permission key required to view this dashboard |
| **Sort Order** | Controls the display order within its category |

### Device Subtype Variations

Device subtypes, also called variations, are used when devices are operationally the same from a user's point of view but have different internal tag structures. The device type describes the common family, while the subtype identifies the specific supplier, model, or variant.

For example, an air quality monitoring system may use AQ sensors from two suppliers. Operators still think of every unit as an AQ sensor: each device measures the same basic values such as particulate matter, CO2, temperature, and humidity, and all devices can be placed together on one consolidated overview map. At the detail level, however, Supplier A and Supplier B devices may expose different tags, different diagnostics, or extra subsystems. One supplier's device might include a UPS with tags such as battery voltage and mains fail, while the other supplier's device does not.

XACT handles this by using the device's `meta.deviceSubtype` tag when a dashboard is opened for a selected device. Dashboard links from the sidebar, Device List widget, Dashboard Nav widget, and Area Map detail link all use the same subtype-aware selection step after setting the current `deviceName`.

To configure subtype-specific dashboards:

1. Create one dashboard for each device subtype.
2. Give the dashboards the same **Name** and place them in the same sidebar category, or both at the top level.
3. Set each dashboard's **Device Subtype** property to match the corresponding device `meta.deviceSubtype` value.
4. Build each dashboard for that subtype's actual tag layout.

For the air quality example, create two dashboards both named `Air Quality Device`. Set one dashboard's **Device Subtype** to `supplier-a` and configure it for the Supplier A tag layout. Set the other dashboard's **Device Subtype** to `supplier-b` and configure it for the Supplier B tag layout, including the UPS widgets or status tables if that subtype has a UPS.

In the sidebar, dashboards with the same **Name** in the same category are shown as one entry. The same rule applies to top-level dashboards. When that shared entry is opened, XACT reads the selected device's subtype and opens the matching dashboard automatically. If no matching subtype dashboard is found, XACT opens the first dashboard in that variation group and writes a message to the browser console.

### Reordering

Drag dashboards and categories to reorder them. Changes are saved automatically with a short debounce delay. The sidebar updates to reflect the new order.

### Deleting Dashboards

Select a dashboard and click **Delete** to remove it. This permanently deletes the dashboard and all its widget configurations.

## Edit Mode

Toggle edit mode using the **pencil icon** in the header bar. When edit mode is active:

- A **widget toolbar** appears at the top of the dashboard, organised into dropdown menus by category (General, Metrics, Layout, System, Custom).
- Existing widgets show **drag handles**, **resize handles**, and **action buttons** (gear icon to configure, X to delete).
- You can **drag widgets** from the toolbar dropdowns onto the canvas.
- The canvas uses a **24-column grid** with configurable cell heights - widgets snap to grid positions.

When edit mode is off, the dashboard is locked and interactive - widgets display live data without the risk of accidental rearrangement.

## Adding Widgets

With edit mode active:

1. Open a **category dropdown** in the toolbar (General, Metrics, Layout, System, or Custom).
2. **Click or drag** a widget type onto the canvas.
3. The widget appears with its default size. Resize it by dragging its bottom-right corner.
4. Click the **gear icon** on the widget to open its configuration dialog and set up data sources and appearance.

## Configuring Widgets

Each widget type has its own configuration properties. Click the **gear icon** on a widget to open its settings. Common properties across many widgets include:

- **Title** - the text shown in the widget header bar
- **Tag Path** - the data source path(s) from the real-time tag tree
- **Tag Prefix** - a prefix applied to all tag paths in the widget (useful for device-specific views where a wildcard `*` substitutes the current device name)
- **Appearance** - colours, font sizes, thresholds, and display options

Changes are applied immediately as you configure them. Remember to save the dashboard to persist your layout.

## Dashboard/UI Variables

Dashboard widgets can read shared UI variables for the current browser session. These variables are set by organisation switching, device selection controls, dashboard navigation widgets, and the Time Range widget. They let one dashboard layout adapt to the selected organisation, device, or time window without hard-coding those values into every widget.

The available variables are:

| Variable | Description |
|----------|-------------|
| `orgName` | The active organisation name |
| `deviceType` | The currently selected device type |
| `deviceName` | The currently selected device name |
| `timeStart` | The start of the active UI time range as a Unix millisecond timestamp, or unset when no range is selected |
| `timeEnd` | The end of the active UI time range as a Unix millisecond timestamp, or unset when no range is selected |
| `serverTimezone` | The server's IANA timezone, such as `America/New_York`, when reported by the server |

Widget support depends on the widget type:

- **Text Widget** - supports `{deviceName}`, `{deviceType}`, and `{orgName}` inside the text field.
- **Tag Prefix** - in widgets that support Tag Prefix, `*` is replaced with the current `deviceName`.
- **Time-aware widgets** - widgets such as Time Series Chart can use `timeStart` and `timeEnd` when **Use UI Time Range** is enabled.

UI variables are not RTDB tags. They are in-memory values for the current user interface session, so they reset when the page is reloaded.

## Moving and Resizing Widgets

In edit mode:

- **Move** - drag a widget by its header bar to reposition it on the grid.
- **Resize** - drag the bottom-right corner handle to change the widget size.
- Widgets have minimum size constraints to ensure they remain usable.
- The grid automatically repositions other widgets to avoid overlap.

## Saving Your Dashboard

Click the **Save** button that appears in the header when you have unsaved changes. The entire layout - widget positions, sizes, and configurations - is saved to the server.

If you try to navigate away with unsaved changes, XACT will prompt you to confirm.

## Import & Export

Dashboards can be exported and imported as JSON files for backup or transfer between XACT instances:

- **Export** - in edit mode, click the **Export** button. A `.json` file containing the full dashboard layout and widget configurations is downloaded.
- **Import** - click **Import** and select a previously exported JSON file. The dashboard layout is replaced with the imported configuration.

This is useful for:
- Backing up dashboard configurations before making changes
- Copying a dashboard layout from a staging environment to production
- Sharing dashboard templates between organisations or XACT instances

## Permissions

Dashboard operations require these permissions:

| Action | Permission |
|--------|-----------|
| View dashboards | `dashboards-setup.read` |
| Create, edit, delete dashboards | `dashboards-setup.edit` |
| View individual dashboards | Dashboard-specific permission (if configured) |
