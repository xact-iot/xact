package psql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/xact-iot/xact/backups"
	"github.com/xact-iot/xact/sqldb"
)

type postgresPool interface {
	Begin(context.Context) (pgx.Tx, error)
	Close()
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

// PostgresDB implements the DB interface using pgxpool.
type PostgresDB struct {
	pool                postgresPool
	rawPool             *pgxpool.Pool
	metricOrgIDs        sync.Map
	metricDeviceIDs     sync.Map
	metricDefinitionIDs sync.Map
}

// NewPostgresDB connects to PostgreSQL and returns a ready-to-use database.
// It does NOT run migrations automatically - call Migrate() separately.
func NewPostgresDB(ctx context.Context, connString string) (sqldb.DB, error) {
	pool, err := pgxpool.New(ctx, connString)
	if err != nil {
		return nil, fmt.Errorf("connecting to database: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pinging database: %w", err)
	}

	// Install timescaledb
	_, err = pool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS timescaledb;")

	return &PostgresDB{pool: pool, rawPool: pool}, err
}

// Migrate creates tables if they don't exist. Safe to call on every startup.
func (db *PostgresDB) Migrate(ctx context.Context) error {
	migration := `
		CREATE TABLE IF NOT EXISTS organisations (
			id   SERIAL PRIMARY KEY,
			name TEXT NOT NULL UNIQUE
		);

		-- New organisation columns (idempotent)
		ALTER TABLE organisations ADD COLUMN IF NOT EXISTS area         BOX;
		ALTER TABLE organisations ADD COLUMN IF NOT EXISTS logo         BYTEA;
		ALTER TABLE organisations ADD COLUMN IF NOT EXISTS logo_data    TEXT DEFAULT '';
		ALTER TABLE organisations ADD COLUMN IF NOT EXISTS favicon      TEXT DEFAULT '';
		ALTER TABLE organisations ADD COLUMN IF NOT EXISTS active       BOOLEAN DEFAULT TRUE;
		ALTER TABLE organisations ADD COLUMN IF NOT EXISTS display_name TEXT DEFAULT '';
		ALTER TABLE organisations ALTER COLUMN logo_data SET DEFAULT '';
		ALTER TABLE organisations ALTER COLUMN favicon SET DEFAULT '';
		ALTER TABLE organisations ALTER COLUMN active SET DEFAULT TRUE;
		ALTER TABLE organisations ALTER COLUMN display_name SET DEFAULT '';
		UPDATE organisations SET logo_data = '' WHERE logo_data IS NULL;
		UPDATE organisations SET favicon = '' WHERE favicon IS NULL;
		UPDATE organisations SET active = TRUE WHERE active IS NULL;
		UPDATE organisations SET display_name = '' WHERE display_name IS NULL;
		ALTER TABLE organisations ALTER COLUMN logo_data SET NOT NULL;
		ALTER TABLE organisations ALTER COLUMN favicon SET NOT NULL;
		ALTER TABLE organisations ALTER COLUMN active SET NOT NULL;
		ALTER TABLE organisations ALTER COLUMN display_name SET NOT NULL;
		CREATE UNIQUE INDEX IF NOT EXISTS idx_organisations_name_unique
			ON organisations(name);

		INSERT INTO organisations (name) VALUES ('default') ON CONFLICT DO NOTHING;

		DO $$
		BEGIN
			IF to_regclass('public.panels') IS NOT NULL
			   AND to_regclass('public.dashboards') IS NULL THEN
				ALTER TABLE panels RENAME TO dashboards;
			END IF;
		END $$;

		CREATE TABLE IF NOT EXISTS dashboards (
			id            SERIAL PRIMARY KEY,
			org_id        INTEGER NOT NULL REFERENCES organisations(id),
			name          TEXT NOT NULL,
			description   TEXT NOT NULL DEFAULT '',
			icon          TEXT NOT NULL DEFAULT '',
			variation     TEXT NOT NULL DEFAULT '',
			device_type   TEXT NOT NULL DEFAULT '',
			is_category  BOOLEAN NOT NULL DEFAULT FALSE,
			parent_id     INTEGER REFERENCES dashboards(id) ON DELETE CASCADE,
			sort_order    INTEGER NOT NULL DEFAULT 0,
			widgets       JSONB NOT NULL DEFAULT '[]',
			created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);

		ALTER TABLE dashboards DROP COLUMN IF EXISTS dashboard_tag;
		ALTER TABLE dashboards DROP COLUMN IF EXISTS panel_tag;
		ALTER TABLE dashboards ADD COLUMN IF NOT EXISTS permission TEXT NOT NULL DEFAULT '';
		ALTER TABLE dashboards ADD COLUMN IF NOT EXISTS is_category BOOLEAN NOT NULL DEFAULT FALSE;

		DROP INDEX IF EXISTS idx_panels_org_name;
		DROP INDEX IF EXISTS idx_dashboards_org_name;
		CREATE INDEX IF NOT EXISTS idx_dashboards_org_name ON dashboards(org_id, name);
		ALTER TABLE dashboards ALTER COLUMN description SET DEFAULT '';
		ALTER TABLE dashboards ALTER COLUMN icon SET DEFAULT '';
		ALTER TABLE dashboards ALTER COLUMN variation SET DEFAULT '';
		ALTER TABLE dashboards ALTER COLUMN device_type SET DEFAULT '';
		ALTER TABLE dashboards ALTER COLUMN permission SET DEFAULT '';
		ALTER TABLE dashboards ALTER COLUMN is_category SET DEFAULT FALSE;
		ALTER TABLE dashboards ALTER COLUMN sort_order SET DEFAULT 0;
		ALTER TABLE dashboards ALTER COLUMN widgets SET DEFAULT '[]';
		ALTER TABLE dashboards ALTER COLUMN created_at SET DEFAULT NOW();
		ALTER TABLE dashboards ALTER COLUMN updated_at SET DEFAULT NOW();
		UPDATE dashboards SET description = '' WHERE description IS NULL;
		UPDATE dashboards SET icon = '' WHERE icon IS NULL;
		UPDATE dashboards SET variation = '' WHERE variation IS NULL;
		UPDATE dashboards SET device_type = '' WHERE device_type IS NULL;
		UPDATE dashboards SET permission = '' WHERE permission IS NULL;
		UPDATE dashboards SET is_category = FALSE WHERE is_category IS NULL;
		UPDATE dashboards SET sort_order = 0 WHERE sort_order IS NULL;
		UPDATE dashboards SET widgets = '[]' WHERE widgets IS NULL;
		UPDATE dashboards SET created_at = NOW() WHERE created_at IS NULL;
		UPDATE dashboards SET updated_at = NOW() WHERE updated_at IS NULL;

		-- Roles table (must exist before permissions FK)
		CREATE TABLE IF NOT EXISTS roles (
			id          SERIAL PRIMARY KEY,
			name        TEXT NOT NULL UNIQUE,
			description TEXT NOT NULL DEFAULT ''
		);
		ALTER TABLE roles ALTER COLUMN description SET DEFAULT '';
		UPDATE roles SET description = '' WHERE description IS NULL;
		CREATE UNIQUE INDEX IF NOT EXISTS idx_roles_name_unique
			ON roles(name);

		INSERT INTO roles (name, description) VALUES
			('SystemAdmin', 'Has unrestricted access to all features and organisations'),
			('Admin',       'Has administrative rights within an organisation'),
			('Manager',     'Ops manager, KPI dashboards, reports'),
			('Technician',  'System infrastructure status and maintenance information'),
			('Operator',    'Operations information and control'),
			('User',        'Readonly access to specified information')
		ON CONFLICT (name) DO NOTHING;

		CREATE TABLE IF NOT EXISTS permissions (
			id         SERIAL PRIMARY KEY,
			org_id     INTEGER NOT NULL REFERENCES organisations(id),
			role       TEXT NOT NULL,
			ui         JSONB NOT NULL DEFAULT '{}',
			server     JSONB NOT NULL DEFAULT '{}',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(org_id, role)
		);
		ALTER TABLE permissions ALTER COLUMN ui SET DEFAULT '{}';
		ALTER TABLE permissions ALTER COLUMN server SET DEFAULT '{}';
		ALTER TABLE permissions ALTER COLUMN created_at SET DEFAULT NOW();
		ALTER TABLE permissions ALTER COLUMN updated_at SET DEFAULT NOW();
		UPDATE permissions SET ui = '{}' WHERE ui IS NULL;
		UPDATE permissions SET server = '{}' WHERE server IS NULL;
		UPDATE permissions SET created_at = NOW() WHERE created_at IS NULL;
		UPDATE permissions SET updated_at = NOW() WHERE updated_at IS NULL;
		ALTER TABLE permissions ALTER COLUMN ui SET NOT NULL;
		ALTER TABLE permissions ALTER COLUMN server SET NOT NULL;
		ALTER TABLE permissions ALTER COLUMN created_at SET NOT NULL;
		ALTER TABLE permissions ALTER COLUMN updated_at SET NOT NULL;
		CREATE UNIQUE INDEX IF NOT EXISTS idx_permissions_org_role_unique
			ON permissions(org_id, role);

		CREATE TABLE IF NOT EXISTS system_config (
			id           SERIAL PRIMARY KEY,
			org_id       INTEGER NOT NULL REFERENCES organisations(id),
			version      INTEGER NOT NULL DEFAULT 1,
			config_name  TEXT NOT NULL,
			config       JSONB NOT NULL DEFAULT '{}',
			created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(org_id, config_name, version)
		);
		ALTER TABLE system_config ALTER COLUMN version SET DEFAULT 1;
		ALTER TABLE system_config ALTER COLUMN config SET DEFAULT '{}';
		ALTER TABLE system_config ALTER COLUMN created_at SET DEFAULT NOW();
		ALTER TABLE system_config ALTER COLUMN updated_at SET DEFAULT NOW();
		UPDATE system_config SET version = 1 WHERE version IS NULL;
		UPDATE system_config SET config = '{}' WHERE config IS NULL;
		UPDATE system_config SET created_at = NOW() WHERE created_at IS NULL;
		UPDATE system_config SET updated_at = NOW() WHERE updated_at IS NULL;
		ALTER TABLE system_config ALTER COLUMN version SET NOT NULL;
		ALTER TABLE system_config ALTER COLUMN config SET NOT NULL;
		ALTER TABLE system_config ALTER COLUMN created_at SET NOT NULL;
		ALTER TABLE system_config ALTER COLUMN updated_at SET NOT NULL;
		CREATE UNIQUE INDEX IF NOT EXISTS idx_system_config_org_name_version_unique
			ON system_config(org_id, config_name, version);

		CREATE TABLE IF NOT EXISTS events (
			id              BIGSERIAL,
			timestamp       TIMESTAMPTZ NOT NULL,
			server          TEXT NOT NULL DEFAULT '',
			org_name        TEXT NOT NULL DEFAULT '',
			user_id         INTEGER REFERENCES users(id),
			severity        TEXT NOT NULL,
			notification_id INTEGER NOT NULL DEFAULT 0,
			device          TEXT NOT NULL DEFAULT '',
			message         TEXT NOT NULL DEFAULT '',
			params          JSONB,
			PRIMARY KEY (id, timestamp)
		);

		-- TimescaleDB hypertable (if extension available)
		DO $$ BEGIN
			IF EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'timescaledb') THEN
				IF NOT EXISTS (
					SELECT 1 FROM timescaledb_information.hypertables
					WHERE hypertable_name = 'events'
				) THEN
					PERFORM create_hypertable('events', 'timestamp', migrate_data => TRUE);
				END IF;
			END IF;
		END $$;

		CREATE INDEX IF NOT EXISTS idx_events_severity ON events (severity, timestamp DESC);
		CREATE INDEX IF NOT EXISTS idx_events_device   ON events (device, timestamp DESC);
		CREATE INDEX IF NOT EXISTS idx_events_org      ON events (org_name, timestamp DESC);
		CREATE INDEX IF NOT EXISTS idx_events_user     ON events (user_id, timestamp DESC);
		CREATE INDEX IF NOT EXISTS idx_events_notif    ON events (notification_id, timestamp DESC);
		CREATE INDEX IF NOT EXISTS idx_events_params   ON events USING GIN (params);

		-- Users table
		CREATE TABLE IF NOT EXISTS users (
			id                   SERIAL PRIMARY KEY,
			first_name           TEXT NOT NULL DEFAULT '',
			last_name            TEXT NOT NULL DEFAULT '',
			login_name           TEXT NOT NULL UNIQUE,
			password_hash        TEXT NOT NULL,
			email                TEXT NOT NULL UNIQUE,
			notification_options JSONB NOT NULL DEFAULT '{}',
			active               BOOLEAN NOT NULL DEFAULT TRUE,
			last_login           TIMESTAMPTZ,
			token_version        INTEGER NOT NULL DEFAULT 1,
			created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		ALTER TABLE users ADD COLUMN IF NOT EXISTS token_version INTEGER NOT NULL DEFAULT 1;
		ALTER TABLE users ALTER COLUMN first_name SET DEFAULT '';
		ALTER TABLE users ALTER COLUMN last_name SET DEFAULT '';
		ALTER TABLE users ALTER COLUMN notification_options SET DEFAULT '{}';
		ALTER TABLE users ALTER COLUMN active SET DEFAULT TRUE;
		ALTER TABLE users ALTER COLUMN token_version SET DEFAULT 1;
		ALTER TABLE users ALTER COLUMN created_at SET DEFAULT NOW();
		ALTER TABLE users ALTER COLUMN updated_at SET DEFAULT NOW();
		UPDATE users SET first_name = '' WHERE first_name IS NULL;
		UPDATE users SET last_name = '' WHERE last_name IS NULL;
		UPDATE users SET notification_options = '{}' WHERE notification_options IS NULL;
		UPDATE users SET active = TRUE WHERE active IS NULL;
		UPDATE users SET token_version = 1 WHERE token_version IS NULL;
		UPDATE users SET created_at = NOW() WHERE created_at IS NULL;
		UPDATE users SET updated_at = NOW() WHERE updated_at IS NULL;
		ALTER TABLE users ALTER COLUMN first_name SET NOT NULL;
		ALTER TABLE users ALTER COLUMN last_name SET NOT NULL;
		ALTER TABLE users ALTER COLUMN notification_options SET NOT NULL;
		ALTER TABLE users ALTER COLUMN active SET NOT NULL;
		ALTER TABLE users ALTER COLUMN token_version SET NOT NULL;
		ALTER TABLE users ALTER COLUMN created_at SET NOT NULL;
		ALTER TABLE users ALTER COLUMN updated_at SET NOT NULL;

		-- User-organisation membership
		CREATE TABLE IF NOT EXISTS user_organisations (
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			org_id  INTEGER NOT NULL REFERENCES organisations(id) ON DELETE CASCADE,
			PRIMARY KEY (user_id, org_id)
		);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_user_organisations_user_org_unique
			ON user_organisations(user_id, org_id);

		-- Roles within an organisation for a user
		CREATE TABLE IF NOT EXISTS user_organisation_roles (
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			org_id  INTEGER NOT NULL REFERENCES organisations(id) ON DELETE CASCADE,
			role_id INTEGER NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
			PRIMARY KEY (user_id, org_id, role_id)
		);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_user_organisation_roles_user_org_role_unique
			ON user_organisation_roles(user_id, org_id, role_id);

		-- Organisation role limits (max persons per role per org)
		CREATE TABLE IF NOT EXISTS organisation_role_limits (
			org_id  INTEGER NOT NULL REFERENCES organisations(id) ON DELETE CASCADE,
			role_id INTEGER NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
			maximum INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (org_id, role_id)
		);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_organisation_role_limits_org_role_unique
			ON organisation_role_limits(org_id, role_id);

		-- Migrate permissions.role from lowercase to canonical case, and add FK.
		-- Wrapped in a check so it only runs once.
		DO $$ BEGIN
			IF NOT EXISTS (
				SELECT 1 FROM information_schema.table_constraints
				WHERE constraint_name = 'fk_permissions_role'
				  AND table_name = 'permissions'
			) THEN
				UPDATE permissions SET role = 'Admin'      WHERE LOWER(role) = 'admin';
				UPDATE permissions SET role = 'Manager'    WHERE LOWER(role) = 'manager';
				UPDATE permissions SET role = 'Technician' WHERE LOWER(role) = 'technician';
				UPDATE permissions SET role = 'Operator'   WHERE LOWER(role) = 'operator';
				UPDATE permissions SET role = 'User'       WHERE LOWER(role) = 'user';
				ALTER TABLE permissions ADD CONSTRAINT fk_permissions_role
					FOREIGN KEY (role) REFERENCES roles(name);
			END IF;
		END $$;

		-- Seed default permissions with canonical role names
		INSERT INTO permissions (org_id, role, ui, server)
		SELECT o.id, r.role, r.ui, '{}'::jsonb
		FROM organisations o
		CROSS JOIN (VALUES
			('SystemAdmin', '{"dashboards-setup": {"read": true, "edit": true}, "dashboard-container": {"edit": true}, "widget-default": {"view": true, "configure": true}, "organisations": {"view": true, "change": true}, "permissions": {"manage": true}, "users": {"manage": true}, "nodes": {"read": true, "write": true}, "tags": {"read": true, "write": true}, "logs": {"read": true, "write": true}}'::jsonb),
			('Admin',       '{"dashboards-setup": {"read": true, "edit": true}, "dashboard-container": {"edit": true}, "widget-default": {"view": true, "configure": true}, "organisations": {"view": false, "change": false}, "permissions": {"manage": true}, "users": {"manage": true}, "nodes": {"read": true, "write": true}, "tags": {"read": true, "write": true}, "logs": {"read": true, "write": true}}'::jsonb),
			('Manager',     '{"dashboards-setup": {"read": true, "edit": true}, "dashboard-container": {"edit": true}, "widget-default": {"view": true, "configure": true}, "organisations": {"view": false, "change": false}, "permissions": {"manage": false}, "users": {"manage": false}, "nodes": {"read": true, "write": true}, "tags": {"read": true, "write": true}, "logs": {"read": true, "write": false}}'::jsonb),
			('Technician',  '{"dashboards-setup": {"read": true, "edit": false}, "dashboard-container": {"edit": true}, "widget-default": {"view": true, "configure": true}, "organisations": {"view": false, "change": false}, "permissions": {"manage": false}, "users": {"manage": false}, "nodes": {"read": true, "write": true}, "tags": {"read": true, "write": true}, "logs": {"read": true, "write": false}}'::jsonb),
			('Operator',    '{"dashboards-setup": {"read": true, "edit": false}, "dashboard-container": {"edit": false}, "widget-default": {"view": true, "configure": false}, "organisations": {"view": false, "change": false}, "permissions": {"manage": false}, "users": {"manage": false}, "nodes": {"read": true, "write": false}, "tags": {"read": true, "write": true}, "logs": {"read": false, "write": false}}'::jsonb),
			('User',        '{"dashboards-setup": {"read": true, "edit": false}, "dashboard-container": {"edit": false}, "widget-default": {"view": true, "configure": false}, "organisations": {"view": false, "change": false}, "permissions": {"manage": false}, "users": {"manage": false}, "nodes": {"read": true, "write": false}, "tags": {"read": true, "write": false}, "logs": {"read": false, "write": false}}'::jsonb)
		) AS r(role, ui)
		WHERE o.name = 'default'
		ON CONFLICT (org_id, role) DO NOTHING;

		UPDATE permissions
		SET ui = (ui - 'panel-container') || jsonb_build_object('dashboard-container', ui->'panel-container')
		WHERE ui ? 'panel-container' AND NOT (ui ? 'dashboard-container');

		-- Add nodes/tags/logs permissions to existing rows that predate this migration.
		-- NOT (ui ? 'nodes') guard makes these idempotent.
		UPDATE permissions SET ui = ui || '{"nodes":{"read":true,"write":true},"tags":{"read":true,"write":true},"logs":{"read":true}}'::jsonb
		WHERE role IN ('SystemAdmin','Admin','Manager','Technician') AND NOT (ui ? 'nodes')
		  AND org_id = (SELECT id FROM organisations WHERE name = 'default');

		-- Add reports permission to existing rows that predate this migration.
		UPDATE permissions SET ui = ui || '{"reports":{"manage":true}}'::jsonb
		WHERE role IN ('SystemAdmin','Admin','Manager') AND NOT (ui ? 'reports');
		UPDATE permissions SET ui = ui || '{"reports":{"manage":false}}'::jsonb
		WHERE role IN ('Technician','Operator','User') AND NOT (ui ? 'reports');

		-- Add scheduler permission to existing rows that predate this migration.
		UPDATE permissions SET ui = ui || '{"scheduler":{"manage":true}}'::jsonb
		WHERE role IN ('SystemAdmin','Admin','Manager') AND NOT (ui ? 'scheduler');
		UPDATE permissions SET ui = ui || '{"scheduler":{"manage":false}}'::jsonb
		WHERE role IN ('Technician','Operator','User') AND NOT (ui ? 'scheduler');

		-- Add organisations.view to existing rows. Preserve the previous
		-- behaviour by granting view automatically only where change was true.
		UPDATE permissions
		SET ui = jsonb_set(
			CASE WHEN ui ? 'organisations' THEN ui ELSE ui || '{"organisations":{}}'::jsonb END,
			'{organisations,view}',
			CASE WHEN (ui #>> '{organisations,change}') = 'true' THEN 'true'::jsonb ELSE 'false'::jsonb END,
			true
		)
		WHERE (ui #> '{organisations,view}') IS NULL;

		-- Add view actions to existing manage-only widget permissions.
		-- Preserve behaviour by granting view automatically where manage was true.
		UPDATE permissions
		SET ui = jsonb_set(
			CASE WHEN ui ? 'permissions' THEN ui ELSE ui || '{"permissions":{}}'::jsonb END,
			'{permissions,view}',
			CASE WHEN (ui #>> '{permissions,manage}') = 'true' THEN 'true'::jsonb ELSE 'false'::jsonb END,
			true
		)
		WHERE (ui #> '{permissions,view}') IS NULL;

		UPDATE permissions
		SET ui = jsonb_set(
			CASE WHEN ui ? 'users' THEN ui ELSE ui || '{"users":{}}'::jsonb END,
			'{users,view}',
			CASE WHEN (ui #>> '{users,manage}') = 'true' THEN 'true'::jsonb ELSE 'false'::jsonb END,
			true
		)
		WHERE (ui #> '{users,view}') IS NULL;

		UPDATE permissions
		SET ui = jsonb_set(
			CASE WHEN ui ? 'reports' THEN ui ELSE ui || '{"reports":{}}'::jsonb END,
			'{reports,view}',
			CASE WHEN (ui #>> '{reports,manage}') = 'true' THEN 'true'::jsonb ELSE 'false'::jsonb END,
			true
		)
		WHERE (ui #> '{reports,view}') IS NULL;

		UPDATE permissions
		SET ui = jsonb_set(
			CASE WHEN ui ? 'notifications' THEN ui ELSE ui || '{"notifications":{}}'::jsonb END,
			'{notifications,view}',
			CASE WHEN (ui #>> '{notifications,manage}') = 'true' THEN 'true'::jsonb ELSE 'false'::jsonb END,
			true
		)
		WHERE (ui #> '{notifications,view}') IS NULL;

		UPDATE permissions
		SET ui = jsonb_set(
			CASE WHEN ui ? 'scheduler' THEN ui ELSE ui || '{"scheduler":{}}'::jsonb END,
			'{scheduler,view}',
			CASE WHEN (ui #>> '{scheduler,manage}') = 'true' THEN 'true'::jsonb ELSE 'false'::jsonb END,
			true
		)
		WHERE (ui #> '{scheduler,view}') IS NULL;

		UPDATE permissions
		SET ui = jsonb_set(
			CASE WHEN ui ? 'tagcalcs' THEN ui ELSE ui || '{"tagcalcs":{}}'::jsonb END,
			'{tagcalcs,view}',
			CASE WHEN (ui #>> '{tagcalcs,manage}') = 'true' THEN 'true'::jsonb ELSE 'false'::jsonb END,
			true
		)
		WHERE (ui #> '{tagcalcs,view}') IS NULL;

		UPDATE permissions SET ui = ui || '{"nodes":{"read":true,"write":false},"tags":{"read":true,"write":true},"logs":{"read":false,"write":false}}'::jsonb
		WHERE role = 'Operator' AND NOT (ui ? 'nodes')
		  AND org_id = (SELECT id FROM organisations WHERE name = 'default');

		UPDATE permissions SET ui = ui || '{"nodes":{"read":true,"write":false},"tags":{"read":true,"write":false},"logs":{"read":false,"write":false}}'::jsonb
		WHERE role = 'User' AND NOT (ui ? 'nodes')
		  AND org_id = (SELECT id FROM organisations WHERE name = 'default');

		-- Split audit-log read and write permissions. Existing installs that
		-- only had logs.read keep their read posture, while only elevated roles
		-- receive logs.write.
		UPDATE permissions
		SET ui = jsonb_set(
			CASE WHEN ui ? 'logs' THEN ui ELSE ui || '{"logs":{}}'::jsonb END,
			'{logs,write}',
			CASE WHEN role IN ('SystemAdmin','Admin') THEN 'true'::jsonb ELSE 'false'::jsonb END,
			true
		)
		WHERE (ui #> '{logs,write}') IS NULL;

		-- Restrict organisations.change for Admin to false (SystemAdmin-only default).
		-- Guarded so it only fires when the value is still the original seeded 'true'.
		UPDATE permissions
		SET ui = jsonb_set(ui, '{organisations,change}', 'false')
		WHERE role = 'Admin'
		  AND (ui #>> '{organisations,change}') = 'true'
		  AND NOT (ui ? 'nodes')
		  AND org_id = (SELECT id FROM organisations WHERE name = 'default');

		-- Backfill permissions for any organisation that has no rows yet.
		-- This covers orgs created before the per-org seeding was added to CreateOrganisation.
		-- ON CONFLICT DO NOTHING makes this safe for orgs that already have rows.
		INSERT INTO permissions (org_id, role, ui, server)
		SELECT o.id, r.role, r.ui, '{}'::jsonb
		FROM organisations o
		CROSS JOIN (VALUES
			('SystemAdmin', '{"dashboards-setup":{"read":true,"edit":true},"dashboard-container":{"edit":true},"widget-default":{"view":true,"configure":true},"organisations":{"view":true,"change":true},"permissions":{"manage":true},"users":{"manage":true},"nodes":{"read":true,"write":true},"tags":{"read":true,"write":true},"logs":{"read":true,"write":true}}'::jsonb),
			('Admin',       '{"dashboards-setup":{"read":true,"edit":true},"dashboard-container":{"edit":true},"widget-default":{"view":true,"configure":true},"organisations":{"view":false,"change":false},"permissions":{"manage":true},"users":{"manage":true},"nodes":{"read":true,"write":true},"tags":{"read":true,"write":true},"logs":{"read":true,"write":true}}'::jsonb),
			('Manager',     '{"dashboards-setup":{"read":true,"edit":true},"dashboard-container":{"edit":true},"widget-default":{"view":true,"configure":true},"organisations":{"view":false,"change":false},"permissions":{"manage":false},"users":{"manage":false},"nodes":{"read":true,"write":true},"tags":{"read":true,"write":true},"logs":{"read":true,"write":false}}'::jsonb),
			('Technician',  '{"dashboards-setup":{"read":true,"edit":false},"dashboard-container":{"edit":true},"widget-default":{"view":true,"configure":true},"organisations":{"view":false,"change":false},"permissions":{"manage":false},"users":{"manage":false},"nodes":{"read":true,"write":true},"tags":{"read":true,"write":true},"logs":{"read":true,"write":false}}'::jsonb),
			('Operator',    '{"dashboards-setup":{"read":true,"edit":false},"dashboard-container":{"edit":false},"widget-default":{"view":true,"configure":false},"organisations":{"view":false,"change":false},"permissions":{"manage":false},"users":{"manage":false},"nodes":{"read":true,"write":false},"tags":{"read":true,"write":false},"logs":{"read":false}}'::jsonb),
			('User',        '{"dashboards-setup":{"read":true,"edit":false},"dashboard-container":{"edit":false},"widget-default":{"view":true,"configure":false},"organisations":{"view":false,"change":false},"permissions":{"manage":false},"users":{"manage":false},"nodes":{"read":true,"write":false},"tags":{"read":true,"write":false},"logs":{"read":false}}'::jsonb)
		) AS r(role, ui)
		WHERE NOT EXISTS (
			SELECT 1 FROM permissions p WHERE p.org_id = o.id
		)
		ON CONFLICT (org_id, role) DO NOTHING;

		UPDATE permissions
		SET ui = jsonb_set(
			CASE WHEN ui ? 'logs' THEN ui ELSE ui || '{"logs":{}}'::jsonb END,
			'{logs,write}',
			CASE WHEN role IN ('SystemAdmin','Admin') THEN 'true'::jsonb ELSE 'false'::jsonb END,
			true
		)
		WHERE (ui #> '{logs,write}') IS NULL;

		-- Also ensure the admin user (login_name = 'admin') is a SystemAdmin in every org.
		-- Uses ON CONFLICT DO NOTHING so it is safe to run repeatedly.
		INSERT INTO user_organisations (user_id, org_id)
		SELECT u.id, o.id
		FROM users u
		CROSS JOIN organisations o
		WHERE u.login_name = 'admin'
		ON CONFLICT DO NOTHING;

		INSERT INTO user_organisation_roles (user_id, org_id, role_id)
		SELECT u.id, o.id, r.id
		FROM users u
		CROSS JOIN organisations o
		JOIN roles r ON r.name = 'SystemAdmin'
		WHERE u.login_name = 'admin'
		ON CONFLICT DO NOTHING;

		-- API keys for device data ingestion (up to 5 per org)
		CREATE TABLE IF NOT EXISTS org_api_keys (
			id         SERIAL PRIMARY KEY,
			org_id     INTEGER NOT NULL REFERENCES organisations(id) ON DELETE CASCADE,
			name       TEXT NOT NULL,
			key        TEXT NOT NULL UNIQUE,
			key_hash   TEXT,
			key_prefix TEXT NOT NULL DEFAULT '',
			key_last4  TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		ALTER TABLE org_api_keys ADD COLUMN IF NOT EXISTS key_hash TEXT;
		ALTER TABLE org_api_keys ADD COLUMN IF NOT EXISTS key_prefix TEXT NOT NULL DEFAULT '';
		ALTER TABLE org_api_keys ADD COLUMN IF NOT EXISTS key_last4 TEXT NOT NULL DEFAULT '';
		ALTER TABLE org_api_keys ALTER COLUMN key_prefix SET DEFAULT '';
		ALTER TABLE org_api_keys ALTER COLUMN key_last4 SET DEFAULT '';
		ALTER TABLE org_api_keys ALTER COLUMN created_at SET DEFAULT NOW();
		UPDATE org_api_keys SET key_prefix = '' WHERE key_prefix IS NULL;
		UPDATE org_api_keys SET key_last4 = '' WHERE key_last4 IS NULL;
		UPDATE org_api_keys SET created_at = NOW() WHERE created_at IS NULL;
		ALTER TABLE org_api_keys ALTER COLUMN key_prefix SET NOT NULL;
		ALTER TABLE org_api_keys ALTER COLUMN key_last4 SET NOT NULL;
		ALTER TABLE org_api_keys ALTER COLUMN created_at SET NOT NULL;
		CREATE UNIQUE INDEX IF NOT EXISTS idx_org_api_keys_key_hash
			ON org_api_keys(key_hash)
			WHERE key_hash IS NOT NULL AND key_hash <> '';

		-- Notification profiles
		CREATE TABLE IF NOT EXISTS notification_profiles (
			id           SERIAL PRIMARY KEY,
			org_name     TEXT NOT NULL REFERENCES organisations(name) ON DELETE CASCADE,
			name         TEXT NOT NULL,
			description  TEXT NOT NULL DEFAULT '',
			roles        JSONB NOT NULL DEFAULT '[]',
			users        JSONB NOT NULL DEFAULT '[]',
			ack_required BOOLEAN NOT NULL DEFAULT false,
			created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(org_name, name)
		);
		ALTER TABLE notification_profiles ALTER COLUMN description SET DEFAULT '';
		ALTER TABLE notification_profiles ALTER COLUMN roles SET DEFAULT '[]';
		ALTER TABLE notification_profiles ALTER COLUMN users SET DEFAULT '[]';
		ALTER TABLE notification_profiles ALTER COLUMN ack_required SET DEFAULT FALSE;
		ALTER TABLE notification_profiles ALTER COLUMN created_at SET DEFAULT NOW();
		ALTER TABLE notification_profiles ALTER COLUMN updated_at SET DEFAULT NOW();
		UPDATE notification_profiles SET description = '' WHERE description IS NULL;
		UPDATE notification_profiles SET roles = '[]' WHERE roles IS NULL;
		UPDATE notification_profiles SET users = '[]' WHERE users IS NULL;
		UPDATE notification_profiles SET ack_required = FALSE WHERE ack_required IS NULL;
		UPDATE notification_profiles SET created_at = NOW() WHERE created_at IS NULL;
		UPDATE notification_profiles SET updated_at = NOW() WHERE updated_at IS NULL;
		ALTER TABLE notification_profiles ALTER COLUMN description SET NOT NULL;
		ALTER TABLE notification_profiles ALTER COLUMN roles SET NOT NULL;
		ALTER TABLE notification_profiles ALTER COLUMN users SET NOT NULL;
		ALTER TABLE notification_profiles ALTER COLUMN ack_required SET NOT NULL;
		ALTER TABLE notification_profiles ALTER COLUMN created_at SET NOT NULL;
		ALTER TABLE notification_profiles ALTER COLUMN updated_at SET NOT NULL;
		CREATE UNIQUE INDEX IF NOT EXISTS idx_notification_profiles_org_name_unique
			ON notification_profiles(org_name, name);

		-- Seed predefined notification profiles
		INSERT INTO notification_profiles (org_name, name, description, roles)
		SELECT 'default', v.name, v.description, v.roles
		FROM (VALUES
			('SysAdmin',   'Server issues',    '["SystemAdmin"]'::jsonb),
			('Manager',    'Operational issues','["Manager"]'::jsonb),
			('Technician', 'Technical issues',  '["Technician"]'::jsonb)
		) AS v(name, description, roles)
		WHERE NOT EXISTS (
			SELECT 1 FROM notification_profiles np WHERE np.org_name = 'default' AND np.name = v.name
		);

		-- Add notifications permission to existing rows that predate this migration.
		UPDATE permissions SET ui = ui || '{"notifications":{"manage":true}}'::jsonb
		WHERE role IN ('SystemAdmin','Admin') AND NOT (ui ? 'notifications');
		UPDATE permissions SET ui = ui || '{"notifications":{"manage":false}}'::jsonb
		WHERE role IN ('Manager','Technician','Operator','User') AND NOT (ui ? 'notifications');

		-- PDF report templates
		CREATE TABLE IF NOT EXISTS pdf_templates (
			id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			org_name      TEXT NOT NULL REFERENCES organisations(name) ON DELETE CASCADE,
			name          TEXT NOT NULL,
			description   TEXT NOT NULL DEFAULT '',
			template_json JSONB NOT NULL DEFAULT '{}',
			variables     JSONB NOT NULL DEFAULT '[]',
			created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(org_name, name)
		);
		ALTER TABLE pdf_templates ALTER COLUMN description SET DEFAULT '';
		ALTER TABLE pdf_templates ALTER COLUMN template_json SET DEFAULT '{}';
		ALTER TABLE pdf_templates ALTER COLUMN variables SET DEFAULT '[]';
		ALTER TABLE pdf_templates ALTER COLUMN created_at SET DEFAULT NOW();
		ALTER TABLE pdf_templates ALTER COLUMN updated_at SET DEFAULT NOW();
		UPDATE pdf_templates SET description = '' WHERE description IS NULL;
		UPDATE pdf_templates SET template_json = '{}' WHERE template_json IS NULL;
		UPDATE pdf_templates SET variables = '[]' WHERE variables IS NULL;
		UPDATE pdf_templates SET created_at = NOW() WHERE created_at IS NULL;
		UPDATE pdf_templates SET updated_at = NOW() WHERE updated_at IS NULL;
		ALTER TABLE pdf_templates ALTER COLUMN description SET NOT NULL;
		ALTER TABLE pdf_templates ALTER COLUMN template_json SET NOT NULL;
		ALTER TABLE pdf_templates ALTER COLUMN variables SET NOT NULL;
		ALTER TABLE pdf_templates ALTER COLUMN created_at SET NOT NULL;
		ALTER TABLE pdf_templates ALTER COLUMN updated_at SET NOT NULL;

		CREATE TABLE IF NOT EXISTS tag_calcs (
			id               SERIAL PRIMARY KEY,
			org_name         TEXT NOT NULL REFERENCES organisations(name) ON DELETE CASCADE,
			name             TEXT NOT NULL,
			description      TEXT NOT NULL DEFAULT '',
			output_tag       TEXT NOT NULL,
			expression       TEXT NOT NULL,
			interval_seconds INT NOT NULL DEFAULT 60,
			enabled          BOOLEAN NOT NULL DEFAULT true,
			created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(org_name, name)
		);
		ALTER TABLE tag_calcs ALTER COLUMN description SET DEFAULT '';
		ALTER TABLE tag_calcs ALTER COLUMN interval_seconds SET DEFAULT 60;
		ALTER TABLE tag_calcs ALTER COLUMN enabled SET DEFAULT TRUE;
		ALTER TABLE tag_calcs ALTER COLUMN created_at SET DEFAULT NOW();
		ALTER TABLE tag_calcs ALTER COLUMN updated_at SET DEFAULT NOW();
		UPDATE tag_calcs SET description = '' WHERE description IS NULL;
		UPDATE tag_calcs SET interval_seconds = 60 WHERE interval_seconds IS NULL;
		UPDATE tag_calcs SET enabled = TRUE WHERE enabled IS NULL;
		UPDATE tag_calcs SET created_at = NOW() WHERE created_at IS NULL;
		UPDATE tag_calcs SET updated_at = NOW() WHERE updated_at IS NULL;
		ALTER TABLE tag_calcs ALTER COLUMN description SET NOT NULL;
		ALTER TABLE tag_calcs ALTER COLUMN interval_seconds SET NOT NULL;
		ALTER TABLE tag_calcs ALTER COLUMN enabled SET NOT NULL;
		ALTER TABLE tag_calcs ALTER COLUMN created_at SET NOT NULL;
		ALTER TABLE tag_calcs ALTER COLUMN updated_at SET NOT NULL;
	`

	if _, err := db.pool.Exec(ctx, migration); err != nil {
		return fmt.Errorf("running migration: %w", err)
	}

	if err := db.seedAdminUser(ctx); err != nil {
		return fmt.Errorf("seeding admin user: %w", err)
	}
	if err := db.seedStarterDashboardsForAllOrgs(ctx); err != nil {
		return fmt.Errorf("seeding starter dashboards: %w", err)
	}
	if err := db.migrateAPIKeyHashes(ctx); err != nil {
		return fmt.Errorf("migrating api key hashes: %w", err)
	}

	metricsMigration := `
		CREATE TABLE IF NOT EXISTS metric_devices (
			id      INTEGER GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
			org_id  INTEGER NOT NULL REFERENCES organisations(id),
			name    TEXT NOT NULL,
			UNIQUE (org_id, name)
		);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_metric_devices_org_name_unique
			ON metric_devices(org_id, name);

		CREATE TABLE IF NOT EXISTS metric_definitions (
			id        INTEGER GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
			device_id INTEGER NOT NULL REFERENCES metric_devices(id),
			name      TEXT NOT NULL,
			UNIQUE (device_id, name)
		);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_metric_definitions_device_name_unique
			ON metric_definitions(device_id, name);

		CREATE TABLE IF NOT EXISTS device_metrics (
			time      TIMESTAMPTZ NOT NULL,
			org_id    INTEGER NOT NULL REFERENCES organisations(id),
			device_id INTEGER NOT NULL REFERENCES metric_devices(id),
			metric_id INTEGER NOT NULL REFERENCES metric_definitions(id),
			value     REAL NOT NULL
		);

		CREATE INDEX IF NOT EXISTS idx_device_metrics_lookup
			ON device_metrics (org_id, device_id, metric_id, time DESC);

		DO $$
		BEGIN
			IF EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'timescaledb') THEN
				IF NOT EXISTS (
					SELECT 1 FROM timescaledb_information.hypertables
					WHERE hypertable_name = 'device_metrics'
				) THEN
					PERFORM create_hypertable('device_metrics', 'time',
						chunk_time_interval => INTERVAL '7 days',
						migrate_data => TRUE);
					ALTER TABLE device_metrics SET (
						timescaledb.compress,
						timescaledb.compress_segmentby = 'org_id, device_id, metric_id',
						timescaledb.compress_orderby = 'time DESC'
					);
				END IF;
				IF NOT EXISTS (
					SELECT 1 FROM timescaledb_information.jobs
					WHERE application_name LIKE '%Compression%'
					  AND hypertable_name = 'device_metrics'
				) THEN
					PERFORM add_compression_policy('device_metrics', INTERVAL '7 days');
				END IF;
			END IF;
		END $$;
	`

	if _, err := db.pool.Exec(ctx, metricsMigration); err != nil {
		return fmt.Errorf("running metrics migration: %w", err)
	}

	schedulerMigration := `
		CREATE TABLE IF NOT EXISTS scheduled_tasks (
			id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			org_name         TEXT NOT NULL REFERENCES organisations(name) ON DELETE CASCADE,
			name             TEXT NOT NULL,
			description      TEXT NOT NULL DEFAULT '',
			task_type        TEXT NOT NULL,
			task_config      JSONB NOT NULL DEFAULT '{}',
			schedule         TEXT NOT NULL,
			enabled          BOOLEAN NOT NULL DEFAULT TRUE,
			last_run_at      TIMESTAMPTZ,
			last_run_status  TEXT NOT NULL DEFAULT '',
			last_run_message TEXT NOT NULL DEFAULT '',
			created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(org_name, name)
		);
		ALTER TABLE scheduled_tasks ALTER COLUMN description SET DEFAULT '';
		ALTER TABLE scheduled_tasks ALTER COLUMN task_config SET DEFAULT '{}';
		ALTER TABLE scheduled_tasks ALTER COLUMN enabled SET DEFAULT TRUE;
		ALTER TABLE scheduled_tasks ALTER COLUMN last_run_status SET DEFAULT '';
		ALTER TABLE scheduled_tasks ALTER COLUMN last_run_message SET DEFAULT '';
		ALTER TABLE scheduled_tasks ALTER COLUMN created_at SET DEFAULT NOW();
		ALTER TABLE scheduled_tasks ALTER COLUMN updated_at SET DEFAULT NOW();
		UPDATE scheduled_tasks SET description = '' WHERE description IS NULL;
		UPDATE scheduled_tasks SET task_config = '{}' WHERE task_config IS NULL;
		UPDATE scheduled_tasks SET enabled = TRUE WHERE enabled IS NULL;
		UPDATE scheduled_tasks SET last_run_status = '' WHERE last_run_status IS NULL;
		UPDATE scheduled_tasks SET last_run_message = '' WHERE last_run_message IS NULL;
		UPDATE scheduled_tasks SET created_at = NOW() WHERE created_at IS NULL;
		UPDATE scheduled_tasks SET updated_at = NOW() WHERE updated_at IS NULL;
		ALTER TABLE scheduled_tasks ALTER COLUMN description SET NOT NULL;
		ALTER TABLE scheduled_tasks ALTER COLUMN task_config SET NOT NULL;
		ALTER TABLE scheduled_tasks ALTER COLUMN enabled SET NOT NULL;
		ALTER TABLE scheduled_tasks ALTER COLUMN last_run_status SET NOT NULL;
		ALTER TABLE scheduled_tasks ALTER COLUMN last_run_message SET NOT NULL;
		ALTER TABLE scheduled_tasks ALTER COLUMN created_at SET NOT NULL;
		ALTER TABLE scheduled_tasks ALTER COLUMN updated_at SET NOT NULL;
		CREATE UNIQUE INDEX IF NOT EXISTS idx_scheduled_tasks_org_name_unique
			ON scheduled_tasks(org_name, name);

		CREATE TABLE IF NOT EXISTS schedule_run_log (
			id           SERIAL PRIMARY KEY,
			schedule_id  UUID NOT NULL REFERENCES scheduled_tasks(id) ON DELETE CASCADE,
			org_name     TEXT NOT NULL,
			fired_at     TIMESTAMPTZ NOT NULL,
			completed_at TIMESTAMPTZ,
			status       TEXT NOT NULL DEFAULT '',
			message      TEXT NOT NULL DEFAULT '',
			output_path  TEXT NOT NULL DEFAULT ''
		);
		ALTER TABLE schedule_run_log ALTER COLUMN status SET DEFAULT '';
		ALTER TABLE schedule_run_log ALTER COLUMN message SET DEFAULT '';
		ALTER TABLE schedule_run_log ALTER COLUMN output_path SET DEFAULT '';
		UPDATE schedule_run_log SET status = '' WHERE status IS NULL;
		UPDATE schedule_run_log SET message = '' WHERE message IS NULL;
		UPDATE schedule_run_log SET output_path = '' WHERE output_path IS NULL;
		ALTER TABLE schedule_run_log ALTER COLUMN status SET NOT NULL;
		ALTER TABLE schedule_run_log ALTER COLUMN message SET NOT NULL;
		ALTER TABLE schedule_run_log ALTER COLUMN output_path SET NOT NULL;
	`

	if _, err := db.pool.Exec(ctx, schedulerMigration); err != nil {
		return fmt.Errorf("running scheduler migration: %w", err)
	}

	return nil
}

func (db *PostgresDB) seedStarterDashboardsForAllOrgs(ctx context.Context) error {
	rows, err := db.pool.Query(ctx, "SELECT id FROM organisations")
	if err != nil {
		return err
	}
	defer rows.Close()

	var orgIDs []int
	for rows.Next() {
		var orgID int
		if err := rows.Scan(&orgID); err != nil {
			return err
		}
		orgIDs = append(orgIDs, orgID)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, orgID := range orgIDs {
		if err := db.seedStarterDashboards(ctx, orgID); err != nil {
			return err
		}
	}
	return nil
}

func (db *PostgresDB) seedStarterDashboards(ctx context.Context, orgID int) error {
	var count int
	if err := db.pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM dashboards WHERE org_id = $1",
		orgID,
	).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	tx, err := db.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		INSERT INTO dashboards (org_id, name, description, icon, variation, device_type, permission, is_category, parent_id, sort_order, widgets)
		VALUES ($1, $2, 'XACT help manual', 'mdi:view-dashboard', '', '', '', FALSE, NULL, 0, $3::jsonb)
	`, orgID, sqldb.StarterDashboardName, sqldb.StarterDashboardWidgetsJSON); err != nil {
		return fmt.Errorf("inserting starter dashboard: %w", err)
	}

	var monitoringID int
	if err := tx.QueryRow(ctx, `
		INSERT INTO dashboards (org_id, name, description, icon, variation, device_type, permission, is_category, parent_id, sort_order, widgets)
		VALUES ($1, $2, 'Monitoring tools', 'mdi:monitor-dashboard', '', '', '', TRUE, NULL, 1, '[]'::jsonb)
		RETURNING id
	`, orgID, sqldb.StarterMonitoringCategory).Scan(&monitoringID); err != nil {
		return fmt.Errorf("inserting starter monitoring category: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO dashboards (org_id, name, description, icon, variation, device_type, permission, is_category, parent_id, sort_order, widgets)
		VALUES ($1, $2, 'Browse and monitor tags', 'mdi:tag-multiple', '', '', '', FALSE, $3, 2, $4::jsonb)
	`, orgID, sqldb.StarterTagViewName, monitoringID, sqldb.StarterTagViewWidgetsJSON); err != nil {
		return fmt.Errorf("inserting starter tag view: %w", err)
	}

	return tx.Commit(ctx)
}

// Close releases the connection pool.
func (db *PostgresDB) Close() {
	if db.pool != nil {
		db.pool.Close()
	}
}

// RawDB returns a *sql.DB backed by the pgxpool, for use with backup adapters.
func (db *PostgresDB) RawDB() *sql.DB {
	if db.rawPool == nil {
		return nil
	}
	return stdlib.OpenDBFromPool(db.rawPool)
}

// BackupAdapter returns a backups.Adapter backed by the PostgreSQL connection.
func (db *PostgresDB) BackupAdapter() backups.Adapter {
	return &backups.PostgresAdapter{DB: db.RawDB()}
}

// resolveOrgID looks up the organisation ID by name.
func (db *PostgresDB) resolveOrgID(ctx context.Context, org string) (int, error) {
	var orgID int
	err := db.pool.QueryRow(ctx, "SELECT id FROM organisations WHERE name = $1", org).Scan(&orgID)
	if err != nil {
		return 0, fmt.Errorf("organisation %q not found: %w", org, err)
	}
	return orgID, nil
}

// ListDashboards returns dashboard metadata (without widgets) for an organisation.
func (db *PostgresDB) ListDashboards(ctx context.Context, org string) ([]sqldb.DashboardMeta, error) {
	rows, err := db.pool.Query(ctx, `
		SELECT p.id, p.name, p.description, p.icon, p.variation,
		       p.device_type, p.permission, p.is_category, p.parent_id, p.sort_order
		FROM dashboards p
		JOIN organisations o ON o.id = p.org_id
		WHERE o.name = $1
		ORDER BY p.sort_order, p.name
	`, org)
	if err != nil {
		return nil, fmt.Errorf("listing dashboards: %w", err)
	}
	defer rows.Close()

	var dashboards []sqldb.DashboardMeta
	for rows.Next() {
		var p sqldb.DashboardMeta
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.Icon,
			&p.Variation, &p.DeviceType, &p.Permission, &p.IsCategory, &p.ParentID, &p.SortOrder); err != nil {
			return nil, fmt.Errorf("scanning dashboard row: %w", err)
		}
		dashboards = append(dashboards, p)
	}
	return dashboards, rows.Err()
}

// GetDashboard returns a single dashboard with full data including widgets.
func (db *PostgresDB) GetDashboard(ctx context.Context, org string, id int) (*sqldb.Dashboard, error) {
	var p sqldb.Dashboard
	err := db.pool.QueryRow(ctx, `
		SELECT p.id, p.name, p.description, p.icon, p.variation,
		       p.device_type, p.permission, p.is_category, p.parent_id, p.sort_order, p.widgets
		FROM dashboards p
		JOIN organisations o ON o.id = p.org_id
		WHERE o.name = $1 AND p.id = $2
	`, org, id).Scan(&p.ID, &p.Name, &p.Description, &p.Icon,
		&p.Variation, &p.DeviceType, &p.Permission, &p.IsCategory, &p.ParentID, &p.SortOrder, &p.Widgets)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting dashboard %d: %w", id, err)
	}
	return &p, nil
}

// CreateDashboard creates a new dashboard for the given organisation.
func (db *PostgresDB) CreateDashboard(ctx context.Context, org string, dashboard *sqldb.Dashboard) error {
	orgID, err := db.resolveOrgID(ctx, org)
	if err != nil {
		return err
	}

	widgets := dashboard.Widgets
	if widgets == nil {
		widgets = json.RawMessage("[]")
	}

	err = db.pool.QueryRow(ctx, `
		INSERT INTO dashboards (org_id, name, description, icon, variation, device_type, permission, is_category, parent_id, sort_order, widgets)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id
	`, orgID, dashboard.Name, dashboard.Description, dashboard.Icon,
		dashboard.Variation, dashboard.DeviceType, dashboard.Permission, dashboard.IsCategory, dashboard.ParentID, dashboard.SortOrder, widgets).Scan(&dashboard.ID)
	if err != nil {
		return fmt.Errorf("creating dashboard %q: %w", dashboard.Name, err)
	}
	return nil
}

// UpdateDashboard updates an existing dashboard identified by organisation and id.
func (db *PostgresDB) UpdateDashboard(ctx context.Context, org string, id int, dashboard *sqldb.Dashboard) error {
	orgID, err := db.resolveOrgID(ctx, org)
	if err != nil {
		return err
	}

	widgets := dashboard.Widgets
	if widgets == nil {
		widgets = json.RawMessage("[]")
	}

	tag, err := db.pool.Exec(ctx, `
		UPDATE dashboards SET
			name = $3, description = $4, icon = $5, variation = $6,
			device_type = $7, permission = $8, is_category = $9, parent_id = $10, sort_order = $11, widgets = $12, updated_at = NOW()
		WHERE org_id = $1 AND id = $2
	`, orgID, id, dashboard.Name, dashboard.Description, dashboard.Icon,
		dashboard.Variation, dashboard.DeviceType, dashboard.Permission, dashboard.IsCategory, dashboard.ParentID, dashboard.SortOrder, widgets)
	if err != nil {
		return fmt.Errorf("updating dashboard %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("dashboard %d not found", id)
	}
	return nil
}

// ListPermissions returns all role permission records for an organisation.
func (db *PostgresDB) ListPermissions(ctx context.Context, org string) ([]sqldb.RolePermissions, error) {
	rows, err := db.pool.Query(ctx, `
		SELECT p.role, p.ui, p.server
		FROM permissions p
		JOIN organisations o ON o.id = p.org_id
		WHERE o.name = $1
		ORDER BY p.role
	`, org)
	if err != nil {
		return nil, fmt.Errorf("listing permissions: %w", err)
	}
	defer rows.Close()

	var perms []sqldb.RolePermissions
	for rows.Next() {
		var rp sqldb.RolePermissions
		if err := rows.Scan(&rp.Role, &rp.UI, &rp.Server); err != nil {
			return nil, fmt.Errorf("scanning permission row: %w", err)
		}
		perms = append(perms, rp)
	}
	return perms, rows.Err()
}

// GetPermissions returns the permission record for a specific role in an organisation.
func (db *PostgresDB) GetPermissions(ctx context.Context, org string, role string) (*sqldb.RolePermissions, error) {
	var rp sqldb.RolePermissions
	err := db.pool.QueryRow(ctx, `
		SELECT p.role, p.ui, p.server
		FROM permissions p
		JOIN organisations o ON o.id = p.org_id
		WHERE o.name = $1 AND p.role = $2
	`, org, role).Scan(&rp.Role, &rp.UI, &rp.Server)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting permissions for role %q: %w", role, err)
	}
	return &rp, nil
}

// UpdatePermissions updates the permission record for a specific role.
func (db *PostgresDB) UpdatePermissions(ctx context.Context, org string, role string, perms *sqldb.RolePermissions) error {
	orgID, err := db.resolveOrgID(ctx, org)
	if err != nil {
		return err
	}

	ui := perms.UI
	if ui == nil {
		ui = json.RawMessage("{}")
	}
	server := perms.Server
	if server == nil {
		server = json.RawMessage("{}")
	}

	tag, err := db.pool.Exec(ctx, `
		UPDATE permissions SET ui = $3, server = $4, updated_at = NOW()
		WHERE org_id = $1 AND role = $2
	`, orgID, role, ui, server)
	if err != nil {
		return fmt.Errorf("updating permissions for role %q: %w", role, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("role %q not found", role)
	}
	return nil
}

// SaveConfig upserts a named config blob for an organisation.
// It increments the version number on each save.
func (db *PostgresDB) SaveConfig(ctx context.Context, org string, configName string, config json.RawMessage) error {
	orgID, err := db.resolveOrgID(ctx, org)
	if err != nil {
		return err
	}

	_, err = db.pool.Exec(ctx, `
		INSERT INTO system_config (org_id, config_name, version, config, updated_at)
		VALUES ($1, $2, 1, $3, NOW())
		ON CONFLICT (org_id, config_name, version) DO UPDATE
			SET config = $3, updated_at = NOW()
	`, orgID, configName, config)
	if err != nil {
		return fmt.Errorf("saving config %q: %w", configName, err)
	}
	return nil
}

// LoadConfig retrieves the latest config blob by org and name. Returns nil if not found.
func (db *PostgresDB) LoadConfig(ctx context.Context, org string, configName string) (json.RawMessage, error) {
	var config json.RawMessage
	err := db.pool.QueryRow(ctx, `
		SELECT sc.config
		FROM system_config sc
		JOIN organisations o ON o.id = sc.org_id
		WHERE o.name = $1 AND sc.config_name = $2
		ORDER BY sc.version DESC
		LIMIT 1
	`, org, configName).Scan(&config)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("loading config %q: %w", configName, err)
	}
	return config, nil
}

// DeleteDashboard removes a dashboard identified by organisation and id.
func (db *PostgresDB) DeleteDashboard(ctx context.Context, org string, id int) error {
	orgID, err := db.resolveOrgID(ctx, org)
	if err != nil {
		return err
	}

	tag, err := db.pool.Exec(ctx, `
		DELETE FROM dashboards WHERE org_id = $1 AND id = $2
	`, orgID, id)
	if err != nil {
		return fmt.Errorf("deleting dashboard %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("dashboard %d not found", id)
	}
	return nil
}

// ── Scheduler ─────────────────────────────────────────────────────────────────

func (db *PostgresDB) ListScheduledTasks(ctx context.Context, org string) ([]sqldb.ScheduledTask, error) {
	rows, err := db.pool.Query(ctx, `
		SELECT id, org_name, name, description, task_type, task_config, schedule,
		       enabled, last_run_at, last_run_status, last_run_message, created_at, updated_at
		FROM scheduled_tasks WHERE org_name = $1 ORDER BY name
	`, org)
	if err != nil {
		return nil, fmt.Errorf("listing scheduled tasks: %w", err)
	}
	defer rows.Close()

	var tasks []sqldb.ScheduledTask
	for rows.Next() {
		var t sqldb.ScheduledTask
		var taskConfig []byte
		if err := rows.Scan(&t.ID, &t.OrgName, &t.Name, &t.Description, &t.TaskType,
			&taskConfig, &t.Schedule, &t.Enabled, &t.LastRunAt,
			&t.LastRunStatus, &t.LastRunMessage, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		t.TaskConfig = json.RawMessage(taskConfig)
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func (db *PostgresDB) GetScheduledTask(ctx context.Context, org string, id string) (*sqldb.ScheduledTask, error) {
	var t sqldb.ScheduledTask
	var taskConfig []byte
	err := db.pool.QueryRow(ctx, `
		SELECT id, org_name, name, description, task_type, task_config, schedule,
		       enabled, last_run_at, last_run_status, last_run_message, created_at, updated_at
		FROM scheduled_tasks WHERE org_name = $1 AND id = $2
	`, org, id).Scan(&t.ID, &t.OrgName, &t.Name, &t.Description, &t.TaskType,
		&taskConfig, &t.Schedule, &t.Enabled, &t.LastRunAt,
		&t.LastRunStatus, &t.LastRunMessage, &t.CreatedAt, &t.UpdatedAt)
	t.TaskConfig = json.RawMessage(taskConfig)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting scheduled task: %w", err)
	}
	return &t, nil
}

func (db *PostgresDB) CreateScheduledTask(ctx context.Context, org string, t *sqldb.ScheduledTask) error {
	return db.pool.QueryRow(ctx, `
		INSERT INTO scheduled_tasks (org_name, name, description, task_type, task_config, schedule, enabled)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at, updated_at
	`, org, t.Name, t.Description, t.TaskType, t.TaskConfig, t.Schedule, t.Enabled).
		Scan(&t.ID, &t.CreatedAt, &t.UpdatedAt)
}

func (db *PostgresDB) UpdateScheduledTask(ctx context.Context, org string, id string, t *sqldb.ScheduledTask) error {
	tag, err := db.pool.Exec(ctx, `
		UPDATE scheduled_tasks
		SET name = $1, description = $2, task_type = $3, task_config = $4,
		    schedule = $5, enabled = $6, updated_at = NOW()
		WHERE org_name = $7 AND id = $8
	`, t.Name, t.Description, t.TaskType, t.TaskConfig, t.Schedule, t.Enabled, org, id)
	if err != nil {
		return fmt.Errorf("updating scheduled task: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("scheduled task %q not found", id)
	}
	return nil
}

func (db *PostgresDB) DeleteScheduledTask(ctx context.Context, org string, id string) error {
	tag, err := db.pool.Exec(ctx, `
		DELETE FROM scheduled_tasks WHERE org_name = $1 AND id = $2
	`, org, id)
	if err != nil {
		return fmt.Errorf("deleting scheduled task: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("scheduled task %q not found", id)
	}
	return nil
}

func (db *PostgresDB) UpdateScheduledTaskStatus(ctx context.Context, id string, status string, message string, runAt time.Time) error {
	_, err := db.pool.Exec(ctx, `
		UPDATE scheduled_tasks
		SET last_run_at = $1, last_run_status = $2, last_run_message = $3, updated_at = NOW()
		WHERE id = $4
	`, runAt, status, message, id)
	return err
}

func (db *PostgresDB) AppendScheduleRunLog(ctx context.Context, entry *sqldb.ScheduleRunLog) error {
	return db.pool.QueryRow(ctx, `
		INSERT INTO schedule_run_log (schedule_id, org_name, fired_at, status, message, output_path)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id
	`, entry.ScheduleID, entry.OrgName, entry.FiredAt, entry.Status, entry.Message, entry.OutputPath).
		Scan(&entry.ID)
}

func (db *PostgresDB) UpdateScheduleRunLog(ctx context.Context, id int, completedAt time.Time, status, message, outputPath string) error {
	_, err := db.pool.Exec(ctx, `
		UPDATE schedule_run_log
		SET completed_at = $1, status = $2, message = $3, output_path = $4
		WHERE id = $5
	`, completedAt, status, message, outputPath, id)
	return err
}

func (db *PostgresDB) ListScheduleRunLog(ctx context.Context, scheduleID string, limit int) ([]sqldb.ScheduleRunLog, error) {
	rows, err := db.pool.Query(ctx, `
		SELECT id, schedule_id, org_name, fired_at, completed_at, status, message, output_path
		FROM schedule_run_log WHERE schedule_id = $1
		ORDER BY fired_at DESC LIMIT $2
	`, scheduleID, limit)
	if err != nil {
		return nil, fmt.Errorf("listing run log: %w", err)
	}
	defer rows.Close()

	var entries []sqldb.ScheduleRunLog
	for rows.Next() {
		var e sqldb.ScheduleRunLog
		if err := rows.Scan(&e.ID, &e.ScheduleID, &e.OrgName, &e.FiredAt, &e.CompletedAt,
			&e.Status, &e.Message, &e.OutputPath); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
