# Preparing for Production

Before exposing XACT beyond a trusted local evaluation network, review the server settings, privileged automation options, restore procedures, and plugin trust model.

## Production Configuration Checklist

Set `XACT_ENV=production` for production or production-like installs. In production mode, startup fails if required secrets are missing or use known defaults.

Review these `.env` settings before exposing XACT beyond localhost:

| Setting | Secure installation guidance |
| --- | --- |
| `JWT_SECRET` | Must be unique and high entropy. Used for JWT signing and as fallback API-key hashing pepper. |
| `API_KEY_HASH_SECRET` | Recommended for production so API-key hashes use a dedicated server-side pepper. |
| `NATS_INTERNAL_PASSWORD` | Must be unique. Internal credentials are not exposed unless explicitly enabled. Browser/mobile clients use their own authenticated session, scoped to their current organisation; the legacy `NATS_BROWSER_TOKEN` is ignored. |
| `MQTT_BROKER_PASSWORD` | Used only by the ingest client with an external MQTT broker; configure a unique password and tenant ACLs there. The embedded broker authenticates devices with tenant API keys and generates its own internal ingest credential. |
| `XACT_BOOTSTRAP_SETUP_TOKEN` | Optional operator-only, randomly generated token of at least 32 characters. Required to claim an unset admin password through the browser; setup is disabled when absent. Remove after setup. |
| `API_HOST`, `NATS_WS_HOST` | Packaged evaluation defaults bind these to `0.0.0.0` for browser access from a trusted local network. For production, bind to loopback or a trusted interface behind a reverse proxy. |
| `NATS_HOST` | Defaults to `127.0.0.1` for the internal NATS listener. Keep it private unless clustering explicitly requires otherwise. |
| `NATS_LOG_FILE` | Defaults to `./logs/nats.log`. Check this file when embedded NATS fails to start; startup also prints the last log lines on failure. |
| `ENABLE_HTTPS`, `HTTP_CERTS_DIR` | Enable HTTPS directly or terminate TLS at a trusted reverse proxy. |
| `START_NGINX` | Defaults to `no`. Set `yes` only after preparing `nginx.conf`, certificates, and proxy/TLS settings. |
| `CORS_ALLOWED_ORIGINS` | Set to the exact UI origins allowed to call the API. In production, no wildcard development CORS is assumed. |
| `MAX_REQUEST_BODY_BYTES` | Caps API request bodies. Defaults to 8 MiB. |
| `EXPOSE_NATS_INTERNAL_CONFIG` | Keep `no`. Only enable for controlled test harness use; the route still requires `SystemAdmin`. |
| `NATS_BROWSER_ALLOW_COMMANDS` | Keep `no` to use the server-mediated command endpoint. If enabled, direct publishing is limited to the current organisation and users with tag-write permission. |
| `EVENT_RETENTION_DAYS` | Production default is `0`, which disables application-side audit/event purging. Set a positive value only when retention policy allows deletion. |

API keys for REST and embedded MQTT ingest are stored as keyed hashes. The full raw key is shown only when it is created; later list views show masked metadata. Store new keys in your device secret manager when they are issued.

## Security upgrade steps

Rebuild and deploy both the server and UI, restart the server, and refresh browser clients. Restarting drops existing messaging connections. NATS permissions are rechecked on reconnect and at session expiry; an existing connection can retain its grants until then.

Existing agent tokens are invalidated by the authentication-version migration and must be deleted and reissued. New tokens stop working when the owner is disabled, loses membership or roles, changes password, or has their sessions revoked. Issuance and retrieval require all token roles to be held by the caller, unless the caller is SystemAdmin. Agent tokens cannot create user sessions through organisation switching or use personal profile/password endpoints.

For each MQTT device, create an ingest API key in its organisation. Set the MQTT username to the organisation slug, password to that API key, and client ID to `<organisation>:<device-id>`. Publish to `xact/data/<organisation>/<device-type>/<device-name>` or `xact/data/<organisation>/zone/<zone>/<device-type>/<device-name>`. Shared passwords and unrestricted wildcard subscriptions no longer work. The embedded ingest connection is configured automatically; keep external brokers' authentication and ACLs equally restrictive.

For a fresh database, provision `XACT_BOOTSTRAP_ADMIN_PASSWORD` or its password file before first startup, or generate `XACT_BOOTSTRAP_SETUP_TOKEN` using `openssl rand -hex 32` and enter it in the setup form. For an existing database with an unset admin password, use the setup token. Setup can claim the account only once and cannot overwrite a configured password.

Report RTDB variables must use fully qualified paths beginning with their own organisation, such as `default.device.temperature` or `/default/device/temperature`. Invalid or foreign paths resolve to empty values.

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

TimescaleDB must be loaded when PostgreSQL starts. Some packages configure this automatically, but if `CREATE EXTENSION` or XACT startup reports `FATAL: extension "timescaledb" must be preloaded`, find the active PostgreSQL configuration file:

```sh
sudo -u postgres psql -c "SHOW config_file;"
```

Open the returned `postgresql.conf` path as an administrator. Find `shared_preload_libraries` (it may be commented out) and add `timescaledb`:

```conf
shared_preload_libraries = 'timescaledb'
```

If other libraries are already configured, keep them and add TimescaleDB to the comma-separated list. For example:

```conf
shared_preload_libraries = 'pg_stat_statements,timescaledb'
```

Restart PostgreSQL for the preload setting to take effect, then verify it:

```sh
sudo systemctl restart postgresql
sudo -u postgres psql -c "SHOW shared_preload_libraries;"
```

Service names vary by distribution and may include the PostgreSQL major version or cluster name. Use the appropriate service name for your installation if `postgresql` is not available.

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

Browser NATS connections cannot access JetStream administration or KV buckets. Initial tree data is loaded through the authenticated REST API. Reconnects revalidate the session against the database, and connections close when their session expires. Account changes take effect on reconnect; already-connected sessions remain valid until their expiry.
