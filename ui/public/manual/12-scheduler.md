# Scheduler

The Scheduler allows time-based automated execution of tasks within XACT. Tasks run on a cron schedule and results are recorded in the run history and posted to the Events log, where they can be viewed in the **Events Viewer** widget.

---

## Scheduler Widget

Add the **Scheduler** widget (category: *System*) to any dashboard to manage scheduled tasks. The widget requires the `scheduler.manage` permission (Manager role or above by default).

The widget displays a table of all scheduled tasks for the current organisation showing:

| Column | Description |
|--------|-------------|
| Name / Description | Task name and optional description |
| Type | Task type: Report, Backup, Shell, Script, or Command |
| Schedule | Human-readable schedule (e.g. *Daily at 08:00*) and the underlying cron expression |
| Last Run | Timestamp of the most recent execution with a status badge |
| Actions | **▶ Run Now**, **Edit**, **Delete**, and **▼ History** |

### Creating a Task

Click **+ New Task** to open the task dialog. Fill in:

- **Name** - a short label shown in the widget table
- **Description** - optional free-text description
- **Enabled** - toggle to activate or pause the task without deleting it
- **Task Type** - select one of the task types (see below)
- **Task Config** - fields specific to the chosen task type
- **Schedule** - use the frequency selector (*Hourly*, *Daily*, *Weekly*, *Monthly*) and the time/day pickers; the resulting 5-field cron expression is shown as a preview
- **Email Profile** - optionally select a notification profile to email results when the task finishes

### Running a Task Manually

Click **▶** in the Actions column to run the task immediately, bypassing the schedule. The run result is written to the history log and the Events table.

### Run History

Click **▼** on any task row to expand the run history for that task. Each entry shows the fire time, completion time, status, and any output path or error message.

---

## Task Types

### Report

Generates a PDF report from an existing PDF template and saves it to a directory on the server.

| Field | Description |
|-------|-------------|
| **Report Template** | Select a PDF template defined in the PDF Reports section |
| **Output Directory** | Filesystem path where the generated PDF is saved. Defaults to `.` (current working directory) if left blank. Example: `/var/xact/reports` |

Template variables are resolved automatically at run time. Variable values defined in the task config override the template defaults.

---

### Backup

Creates a compressed `.tar.gz` database backup and writes it to a directory on the server.

| Field | Description |
|-------|-------------|
| **Output Directory** | Filesystem path where the backup archive is saved. Defaults to `.` if left blank. Example: `/var/xact/backups` |

The archive filename is generated automatically: `backup-YYYYMMDD-HHMMSS.tar.gz`.

> **Note:** To restore a backup archive see *Restoring a Backup* at the end of this chapter.

---

### Shell

Runs an arbitrary shell command via `sh -c` on the server.

| Field | Description |
|-------|-------------|
| **Shell Command** | The command string passed to `sh -c`. Can be a script path or an inline command. Example: `/usr/local/bin/cleanup.sh` |
| **Timeout (seconds)** | Maximum execution time in seconds. `0` uses the default of **300 seconds** (5 minutes). |

Both stdout and stderr are captured. If the command exits with a non-zero status the task is marked as failed and the combined output is saved to the run log.

> **Security:** Shell tasks run with the same OS user as the XACT server process. Restrict `scheduler.manage` permission to trusted roles.

---

### Script

Runs an embedded Go script using the [Yaegi](https://github.com/traefik/yaegi) interpreter. Useful for lightweight automation that needs access to Go standard library functions without deploying a separate binary.

| Field | Description |
|-------|-------------|
| **Go Script** | A complete Go source file in package `script` that exports a `Run() error` function |
| **Timeout (seconds)** | Maximum execution time in seconds. `0` uses the default of **60 seconds**. |

**Script template:**

```go
package script

import "fmt"

func Run() error {
    fmt.Println("hello from scheduler")
    return nil
}
```

The script must:
- Declare `package script`
- Export a function `Run() error`
- Use only packages available in the Go standard library

---

### Command

Sends a device command over the standard XACT NATS command protocol. Use this when an operating action should happen automatically on a schedule.

| Field | Description |
|-------|-------------|
| **Device path** | Target device path used in the command subject. Example: `WaterWorks.PUMP_STATION.RAW_WATER_PS` |
| **Tag Path** | Relative command path within the device. Example: `pumps.1.status` |
| **Value** | JSON-like value to send. `true`, `false`, numbers, strings, arrays, and objects are accepted |
| **Timeout (seconds)** | Maximum time to wait for the driver response. `0` uses the default of **10 seconds** |

At run time the scheduler publishes a request to:

```text
xact.command.{org}.{device-path}
```

with a payload similar to:

```json
{
  "id": "abcd1234",
  "pumps.1.status": true
}
```

The device driver must reply with:

```json
{
  "id": "abcd1234",
  "success": true,
  "message": "The command succeeded"
}
```

The scheduler writes the command result to the server console and records a success or failure event. If the driver returns `success: false`, sends an invalid response, or does not respond before the timeout, the task run is marked as failed.

---

## Schedules

Schedules are stored as standard 5-field cron expressions (`minute hour day-of-month month day-of-week`). The task dialog provides a friendly picker that builds the expression for you:

| Frequency | Fields used | Example cron |
|-----------|-------------|--------------|
| Hourly | Minute only | `30 * * * *` - at :30 every hour |
| Daily | Hour + Minute | `0 8 * * *` - daily at 08:00 |
| Weekly | Hour + Minute + Day | `0 8 * * 1` - Mondays at 08:00 |
| Monthly | Hour + Minute + Day of month | `0 8 1 * *` - 1st of each month at 08:00 |

Day-of-month values are capped at 28 to avoid issues in short months (February).

All schedule times are interpreted in the **server's local timezone**.

---

## Events Integration

Every task execution - whether triggered by the cron schedule or by **Run Now** - writes an entry to the Events table:

- **Success** - `INFO` severity, message: *Scheduled task "Name" completed successfully*
- **Failure** - `ERROR` severity, message: *Scheduled task "Name" failed: \<error detail\>*

Use the **Events Viewer** widget with a filter on `device = scheduler` to monitor all scheduled task outcomes.

---

## Restoring a Backup

Backup archives are standard `.tar.gz` files containing a `schema.json` schema manifest and one CSV file per table. A pre-built restore tool is included in the server build at `server/bin/restore`. It reads the same `.env` file as the server and automatically selects the correct database driver.

> **Warning:** Restore overwrites existing table data. 

> **Stop the XACT server before restoring.**

### Running a restore

From the `server/` directory:

```sh
./bin/restore --confirm ./backups/backup-20260409-080000.tar.gz
```

The tool reads `DATABASE_URL` (PostgreSQL) or `SQLITE_PATH` (SQLite) from `.env` - whichever is configured. Output:

```
2026/04/09 08:00:00 Using SQLite: ./bin/xact.db
2026/04/09 08:00:01 Restoring from /var/xact/backups/backup-20260409-080000.tar.gz …
2026/04/09 08:00:02 Restore complete.
```

### Building the restore tool

The restore tool is built automatically by `run.sh` alongside the main server binary. To build it manually:

```sh
cd server
go build -o bin/restore ./cmd/restore
```

### What the restore does

1. Reads `schema.json` from the archive and recreates any missing tables (`CREATE TABLE IF NOT EXISTS`)
2. Imports each table's CSV data - PostgreSQL uses `INSERT … ON CONFLICT DO NOTHING`; SQLite uses a plain `INSERT`
3. Finalises schema extensions - for PostgreSQL this recreates TimescaleDB hypertables where applicable

---

## Permissions

The scheduler uses the `scheduler` permission group. The single permission key is:

| Key | Default roles | Description |
|-----|---------------|-------------|
| `scheduler.manage` | Manager, Admin, SystemAdmin | Create, edit, delete, and manually trigger scheduled tasks |

Roles below Manager (Technician, Operator, User) cannot see or interact with the Scheduler widget.
