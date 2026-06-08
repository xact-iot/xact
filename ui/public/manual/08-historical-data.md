# Historical Data

While tags in the RTDB hold current values only, XACT also records historical time-series data for trending, charting, and reporting. This chapter explains how historical data is captured, stored, and accessed.

## How Historical Data Works

There are two types of data in XACT:

- **Real-time status data** - the current value of each tag, held in the RTDB and published via NATS for live dashboard updates.
- **Historical data** - timestamped snapshots of tag values archived in a time-series database (TSDB) for long-term storage and analysis.

Historical data is **not** published in real time. It is written to the database and queried on demand - for example, when a time-series chart loads, when a report is generated, or when a user explores trends.

## Recording Historical Data

The most common way to record historical data is through the **History Recorder** processing block in a tag's pipeline. When configured:

1. Each time a new value arrives at the tag, the history recorder checks whether the change is significant (exceeds the tag's **deadband** setting).
2. If the change is significant, a timestamped record is written to the metrics database.
3. Trivial changes (within the deadband) are skipped to avoid recording noise.

Only **numeric values** on **device tags** are recorded. String and boolean tags, and tags outside device nodes, are not eligible for history recording.

### Configuring History Recording

To enable history recording on a tag:

1. Open the **Tags Manager** widget.
2. Navigate to the tag you want to record.
3. Open the tag's pipeline editor.
4. Add a **History Recorder** block to the pipeline.
5. Set the tag's **deadband** value - this controls the minimum change that triggers a recording. For example, a deadband of `0.5` on a temperature tag means changes smaller than 0.5 degrees are not recorded.

The history recorder has no parameters of its own - it relies on the tag's deadband configuration.

### Batch Ingestion

Historical data can also arrive in batches from field devices that collect and transmit readings at intervals. When data arrives via MQTT or the REST API with timestamps, those timestamps are preserved in the database rather than using server time.

## Storage

XACT uses **TimescaleDB** (a PostgreSQL extension) for time-series storage. Data is organised into:

- **metric_devices** - a lookup table mapping device paths to compact integer IDs
- **metric_definitions** - a lookup table mapping metric names to IDs per device
- **device_metrics** - the main hypertable storing timestamped values, partitioned into weekly chunks for performance

### Data Lifecycle

- **Compression** - older data chunks are automatically compressed to reduce storage requirements.
- **Retention** - data older than the configured retention period (default: 6 months) is automatically purged. The retention period is configurable via server environment variables.

## Viewing Historical Data

### Time Series Charts

The **Time Series Chart** widget is the primary tool for viewing historical data. It queries the metrics database and plots up to 5 data series on a time axis. See the [Widgets](#widgets) chapter for configuration details.

Charts can operate in two time modes:

- **Fixed period** - always shows the most recent N hours of data
- **UI time range** - synchronises with the global time range selector, allowing the user to zoom into any historical period

### Sparklines

The **Sparkline** and **Big Number** (with sparkline enabled) widgets also display recent historical data as compact trend indicators.

### PDF Reports

Historical data can be embedded in PDF reports as chart images and tables. Report templates can query metrics over configurable lookback periods (1 hour to 30 days). See the [PDF Reports](#reports) chapter for details.

## Querying Historical Data via the API

Two REST API endpoints are available for querying metrics:

### Range Query

```
GET /api/v1/metrics/{devicepath}?start={timestamp}&end={timestamp}&metrics={name1,name2}
```

Returns metric values within the specified time range. The `end` parameter is optional and defaults to the current time.

### Since Query

```
GET /api/v1/metrics/{devicepath}/since?start={timestamp}&metrics={name1,name2}
```

Returns metric values recorded after the specified timestamp. This is used by charts to incrementally fetch new data without re-loading the entire history.

### Parameters

| Parameter | Description |
|-----------|-------------|
| `devicepath` | The device path in the tag tree (using slashes as separators) |
| `start` | Start time as an RFC3339 timestamp (e.g. `2026-01-15T08:00:00Z`) |
| `end` | End time as an RFC3339 timestamp (optional, defaults to now) |
| `metrics` | Comma-separated list of metric names to retrieve |

Both endpoints return JSON formatted for direct consumption by charting libraries.

## Best Practices

- **Set appropriate deadbands** - too small and you record noise; too large and you miss meaningful changes. A good starting point is 0.1% to 1% of the tag's expected range.
- **Use history recording selectively** - not every tag needs historical recording. Focus on values that are useful for trending, reporting, or troubleshooting.
- **Monitor storage** - time-series data grows over time. Review the retention period setting and adjust based on your storage capacity and data requirements.
- **Place the history recorder correctly in the pipeline** - typically after the scale block (so recorded values are in engineering units) but the exact position depends on your needs.
