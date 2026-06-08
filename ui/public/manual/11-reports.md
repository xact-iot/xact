# PDF Reports

XACT can generate PDF reports from configurable templates. Reports pull live and historical data into a formatted document for printing, archiving, or distribution.

## Report Templates

A report template defines the structure and content of a PDF document. Templates are created and managed through the **PDF Reports** widget (System category).

Each template consists of:

- **Metadata** - name, description, page size (A4 or Letter), orientation (portrait or landscape), margins, and optional watermark
- **Elements** - ordered content blocks that form the report body
- **Variables** - dynamic values resolved at generation time

Templates are organisation-specific - each organisation maintains its own set of report templates.

## Creating a Template

1. Open the **PDF Reports** widget from the System category.
2. Click **New Template**.
3. Enter a template **name** and **description**.
4. On the **Editor** tab, configure the page settings in **Document Settings**:
   - Page size
   - Orientation
   - Document title
   - Watermark
   - Margins
5. Use the **Components** palette to add elements to the page canvas.
6. Define **variables** for dynamic content (see below).
7. Save the template.

## Template Elements

Elements are the building blocks of a report. They are rendered in order from top to bottom. Available element types:

### Title

A heading or banner area. Titles are built from one or more styled cells and support configurable text, font size, alignment, borders, and colours. Supports variable substitution using `{{variable_name}}`.

### Table

Tabular data with configurable columns. Each cell can contain static text, variable references, or tag values. Supports:

- Column headers with custom styling
- Per-cell borders, colours, and alignment
- Variable substitution in cell content using `{{variable_name}}`

### Spacer

A vertical gap between elements. Configure the height in points to control the spacing.

### Footer

Single-line text element commonly used for page numbers, report titles, or generation dates. Supports alignment, font styling, borders, and variable substitution.

### Image

A static image such as a logo, diagram, or photo. Images can be uploaded into the template and sized within the page.

### Chart

An embedded time-series chart rendered from historical metric data. Configure:

- **Tag paths** - which metrics to plot
- **Lookback period** - how far back to chart (1 hour to 30 days)
- **Chart title** - optional heading shown above the chart
- **Y-axis label / min / max** - optional axis settings
- **Chart height** - height within the report
- **Series options** - colours, labels, axis configuration

Charts are rendered server-side using the same metrics database as the dashboard Time Series Chart widget.

### Events

An embedded list of events filtered by criteria. Configure:

- **Lookback period** - the period to include
- **Severity filter** - selected severity level
- **Device filter** - limit to specific devices
- **Search** - filter by matching event text
- **Maximum rows** - cap the number of events shown
- **Columns** - select which event fields to include

### Pie Chart

A pie chart built from live RTDB values. Use it to compare a small number of related values in a report. Configure:

- **Height** - chart height in the report
- **Slices** - one row per slice
  - **Tag path** - RTDB path for the slice value
  - **Colour** - hex colour for the slice
  - **Label** - legend label
- **Show legend** - show or hide the legend under the chart

Pie chart values are often obtained from 'Tag Calcs'. For example, you might create a Tag Calc that calculates the percentage of total energy consumption for each device.

Pie charts are resolved at report generation time, so the final PDF uses current values when it is produced.

## Variables

Variables make reports dynamic. Instead of hard-coding values, you define variables that are resolved when the report is generated. Reference variables in element content or tag paths using `{{variable_name}}` syntax.

### Variable Types

| Type | Description | When Resolved |
|------|-------------|---------------|
| **Builtin** | System-provided values: current date/time, organisation name, report name | Automatically at generation time |
| **RTDB** | Live tag values fetched from the real-time database by tag path | Automatically at generation time |
| **SQL** | Custom database query results | Automatically at generation time |
| **Custom** | User-provided values via an input prompt | The user enters the value when generating the report |

### Defining Variables

1. In the template editor, navigate to the **Variables** section.
2. Click **Add Variable**.
3. Enter a **name** (used in `{{name}}` references), select the **type**, and configure type-specific settings:
   - **Builtin** - select from available system values
   - **RTDB** - enter the tag path to read
   - **SQL** - enter the SQL query
   - **Custom** - enter a label and optional default value

## Template Editor

The template editor provides:

- **Visual canvas** - drag and position elements to see an approximation of the final layout
- **Element palette** - drag element types from the palette onto the canvas
- **Property panel** - configure element properties (text, colours, borders, sizing)
- **JSON editor** - switch to a raw JSON view for advanced editing and precise control
- **Live preview** - zoom from 25% to 400% to inspect the layout
- **Element styling** - per-element control over fonts, colours, borders, padding, and alignment

## Generating a Report

1. Open the **PDF Reports** widget.
2. Select a saved template from the list and click the **download** button, or open the template in the editor and click **Download**.
3. If the template has **Custom** variables, enter the requested values before previewing or generating.
4. The server renders the PDF - resolving all variables, fetching tag values, querying metrics, and assembling the document.
5. The completed PDF is returned for download.

Reports are generated entirely server-side using the Go PDF library, ensuring consistent output regardless of browser, operating system, or device.

## Permissions

| Action | Permission Required |
|--------|-------------------|
| Create, edit, generate, preview, or delete templates | `reports.manage` |

## Tips

- **Use builtin variables** for dates and organisation names rather than hard-coding them - this ensures reports remain correct when regenerated later or in different organisations.
- **Test with preview** before generating the final PDF - the preview shows how elements will be positioned and whether variable substitution is working correctly.
- **Keep templates modular** - use variables for anything that might change between reports (date ranges, device selections, notes) rather than creating duplicate templates.
- **Charts respect the lookback period** - make sure the configured period matches your reporting needs (e.g. a daily report should use a 24-hour lookback).
- **Pie charts work best with a small number of slices** - if you need many categories, use a table instead.
- **Use the tag picker buttons** in chart and pie chart properties to browse RTDB paths rather than typing them manually.

## Pie Chart Example

Use a **Pie Chart** element when you want to compare a small set of live values in one report graphic.

1. Add **Pie Chart** from the Components palette.
2. Set the chart **Height**.
3. Under **Slices**, add one row per slice.
4. For each slice, enter:
   - A **tag path** such as `plant.area1.power_kw`
   - A **colour** in hex format
   - A **label** such as `Area 1`
5. Enable or disable **Show legend**.

Example:

| Tag path | Colour | Label |
|------|------|------|
| `loads.boiler.kw` | `#ef4444` | `Boiler` |
| `loads.pump_station.kw` | `#3b82f6` | `Pump Station` |
| `loads.hvac.kw` | `#22c55e` | `HVAC` |

Notes:

- Pie chart values are read when the PDF is generated.
- You can use variables in tag paths, for example `loads.{{device}}.kw`.
- The editor shows a lightweight placeholder preview; the final chart is rendered in the generated PDF.
