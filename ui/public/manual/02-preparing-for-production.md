# Preparing for Production

Before exposing XACT beyond a trusted local evaluation network, review the server settings, privileged automation options, restore procedures, and plugin trust model.

## Production Configuration Checklist

Set `XACT_ENV=production` for production or production-like installs. In production mode, startup fails if required secrets are missing or use known defaults.

Review these `.env` settings before exposing XACT beyond localhost:

| Setting | Secure installation guidance |
| --- | --- |
| `JWT_SECRET` | Must be unique and high entropy. Used for JWT signing and as fallback API-key hashing pepper. |
| `API_KEY_HASH_SECRET` | Recommended for production so API-key hashes use a dedicated server-side pepper. |
| `NATS_INTERNAL_PASSWORD` / `NATS_BROWSER_TOKEN` | Must be unique. Internal credentials are not exposed unless explicitly enabled. |
| `MQTT_BROKER_PASSWORD` | Must be unique when embedded MQTT or MQTT ingest is enabled. |
| `API_HOST`, `NATS_WS_HOST` | Packaged evaluation defaults bind these to `0.0.0.0` for browser access from a trusted local network. For production, bind to loopback or a trusted interface behind a reverse proxy. |
| `NATS_HOST` | Defaults to `127.0.0.1` for the internal NATS listener. Keep it private unless clustering explicitly requires otherwise. |
| `NATS_LOG_FILE` | Defaults to `./logs/nats.log`. Check this file when embedded NATS fails to start; startup also prints the last log lines on failure. |
| `ENABLE_HTTPS`, `HTTP_CERTS_DIR` | Enable HTTPS directly or terminate TLS at a trusted reverse proxy. |
| `START_NGINX` | Defaults to `no`. Set `yes` only after preparing `nginx.conf`, certificates, and proxy/TLS settings. |
| `CORS_ALLOWED_ORIGINS` | Set to the exact UI origins allowed to call the API. In production, no wildcard development CORS is assumed. |
| `MAX_REQUEST_BODY_BYTES` | Caps API request bodies. Defaults to 8 MiB. |
| `EXPOSE_NATS_INTERNAL_CONFIG` | Keep `no`. Only enable for controlled test harness use; the route still requires `SystemAdmin`. |
| `NATS_BROWSER_ALLOW_COMMANDS` | Keep `no`. Browser command publishing should use the server-mediated command endpoint. |
| `EVENT_RETENTION_DAYS` | Production default is `0`, which disables application-side audit/event purging. Set a positive value only when retention policy allows deletion. |

API keys for REST ingest are stored as keyed hashes. The full raw key is shown only when it is created; later list views show masked metadata. Store new keys in your device secret manager when they are issued.

## PostgreSQL and TimescaleDB

For production deployments, use PostgreSQL with the TimescaleDB extension rather than an embedded SQLite database. PostgreSQL provides the durable relational store for users, organisations, dashboards, reports, schedules, events, and API-key metadata. TimescaleDB adds hypertables, compression, and retention policies for XACT's time-series data.

The simplest installation method on Linux is usually your distribution's package manager. Install PostgreSQL first, then install the TimescaleDB package that matches your PostgreSQL major version. Package names vary by distribution and repository, but they are commonly similar to:

```sh
# Debian/Ubuntu example package names
sudo apt install postgresql postgresql-contrib timescaledb-2-postgresql-16

# RHEL/Fedora-family example package names vary by enabled repositories
sudo dnf install postgresql-server postgresql-contrib timescaledb-2-postgresql-16
```

After installing packages, initialise and start PostgreSQL if your distribution does not do that automatically. On many Linux systems this is handled with `systemctl`:

```sh
sudo systemctl enable --now postgresql
```

Create a database and user for XACT. Use a strong password and keep it in the server's environment file rather than in shell history or shared notes:

```sh
sudo -u postgres psql
```

```sql
CREATE USER xact WITH PASSWORD 'replace-with-a-strong-password';
CREATE DATABASE xact OWNER xact;
\c xact
CREATE EXTENSION IF NOT EXISTS timescaledb;
GRANT ALL PRIVILEGES ON DATABASE xact TO xact;
```

Configure XACT to use PostgreSQL by setting `DATABASE_URL` in `.env`:

```sh
DATABASE_URL=postgres://xact:replace-with-a-strong-password@127.0.0.1:5432/xact?sslmode=disable
```

When XACT starts, it connects to `DATABASE_URL`, creates the TimescaleDB extension if the database user has permission, runs schema migrations, creates Timescale hypertables for events and device metrics, and configures the metric retention policy. If you create the extension as the PostgreSQL administrator first, the XACT database user does not need extension-management privileges during normal operation.

For production, prefer local socket or private-network access to PostgreSQL, restrict PostgreSQL listener addresses and firewall rules, and use TLS or a trusted private network when the database is not on the same host. Set `METRICS_RETENTION_DAYS` to match your storage and compliance needs; the default is 180 days for PostgreSQL/TimescaleDB metrics.

## Scheduler Security

The scheduler supports safe built-in task types such as report, backup, and command tasks. Shell and Yaegi script tasks are privileged background execution and are disabled by default.

To enable them for a trusted deployment, configure the server-side execution gate and output directories:

| Setting | Purpose |
| --- | --- |
| `ENABLE_UNSAFE_SCHEDULER_TASKS=yes` | Allows shell and Yaegi task types to be created or run. |
| `SCHEDULER_OUTPUT_DIR` | Root directory for scheduled report and backup outputs. Defaults to `backups`. |
| `SCHEDULER_WORK_DIR` | Working directory for shell tasks. Defaults to `SCHEDULER_OUTPUT_DIR`. |

Do not grant scheduler management permissions casually. Shell and Yaegi tasks run without an attached user session and should be treated as administrator-controlled automation.

## Restore Safety

Restore archives are validated before SQL is generated, including archive paths, schema identifiers, table names, column names, primary keys, indexes, extension metadata, and column types.

Before replacing the target database, the restore utility saves the existing database in `XACT_RESTORE_SAFETY_DIR`, which defaults to `./backups`. SQLite restores copy the current database file. PostgreSQL restores write a XACT-format backup archive of the current public tables, then drop the public tables with `CASCADE` before importing.

API key records are intentionally not included in XACT backup archives. Generated API keys are shown only once, cannot be recovered from their stored hashes, and are usually invalid after restore unless the same server-side hashing secret is reused. Create new API keys after restoring and update the affected devices or integrations.

The restore command requires explicit operator confirmation:

```sh
./restore --confirm --sha256 <expected-sha256> <backup.tar.gz>
```

For non-interactive restores, use:

```sh
XACT_RESTORE_CONFIRM=yes XACT_RESTORE_SHA256=<expected-sha256> ./restore <backup.tar.gz>
```

Set `XACT_RESTORE_SAFETY_DIR=<dir>` to place pre-restore safety copies somewhere other than `./backups`.

Run restores during a maintenance window, stop the XACT server first, and only restore from trusted backup archives.

## Plugins

Plugin directories are trusted code. Keep `PLUGIN_DIR` writable only by trusted administrators. World-writable plugin directories are disabled at startup, and group-writable directories produce a trust warning.

Runtime authentication plugins are disabled unless explicitly enabled:

```sh
ENABLE_AUTH_PLUGIN=yes
```

Static widget, map-layer, and theme plugins are served as JavaScript and should be installed only from trusted sources.
