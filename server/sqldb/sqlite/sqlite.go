// Package sqlite implements the sqldb.DB interface using an embedded SQLite database.
package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/xact-iot/xact/backups"
	"github.com/xact-iot/xact/sqldb"
	_ "modernc.org/sqlite"
)

// SQLiteDB implements the sqldb.DB interface using modernc.org/sqlite.
type SQLiteDB struct {
	db                  *sql.DB
	bootstrapAdminFile  string
	metricOrgIDs        sync.Map
	metricDeviceIDs     sync.Map
	metricDefinitionIDs sync.Map
}

// NewSQLiteDB opens an SQLite database at path and returns a ready-to-use instance.
// Call Migrate() separately to initialise the schema.
func NewSQLiteDB(ctx context.Context, path string) (sqldb.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite database: %w", err)
	}
	// SQLite allows only one writer at a time; one connection prevents contention.
	db.SetMaxOpenConns(1)
	// Enable foreign key enforcement on the single connection.
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enabling foreign keys: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("pinging sqlite database: %w", err)
	}
	return &SQLiteDB{db: db, bootstrapAdminFile: bootstrapAdminPasswordFile(path)}, nil
}

// Close releases the database connection.
func (db *SQLiteDB) Close() {
	db.db.Close()
}

// RawDB returns the underlying *sql.DB, for use with backup adapters.
func (db *SQLiteDB) RawDB() *sql.DB {
	return db.db
}

// BackupAdapter returns a backups.Adapter backed by the SQLite connection.
func (db *SQLiteDB) BackupAdapter() backups.Adapter {
	return &backups.SQLiteAdapter{DB: db.db}
}

// Migrate creates tables and seeds initial data. Safe to call on every startup.
func (db *SQLiteDB) Migrate(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS organisations (
			id           INTEGER PRIMARY KEY,
			name         TEXT NOT NULL UNIQUE,
			display_name TEXT NOT NULL DEFAULT '',
			active       INTEGER NOT NULL DEFAULT 1,
			logo         TEXT NOT NULL DEFAULT '',
			favicon      TEXT NOT NULL DEFAULT '',
			area         TEXT
		)`,
		`ALTER TABLE organisations ADD COLUMN logo TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE organisations ADD COLUMN favicon TEXT NOT NULL DEFAULT ''`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_organisations_name_unique ON organisations(name)`,
		`INSERT OR IGNORE INTO organisations (name, display_name, active) VALUES ('default', '', 1)`,

		`CREATE TABLE IF NOT EXISTS roles (
			id          INTEGER PRIMARY KEY,
			name        TEXT NOT NULL UNIQUE,
			description TEXT NOT NULL DEFAULT ''
		)`,
		`INSERT INTO roles (name, description)
		 SELECT 'SystemAdmin', 'Has unrestricted access to all features and organisations'
		 WHERE NOT EXISTS (SELECT 1 FROM roles WHERE name = 'SystemAdmin')`,
		`INSERT INTO roles (name, description)
		 SELECT 'Admin', 'Has administrative rights within an organisation'
		 WHERE NOT EXISTS (SELECT 1 FROM roles WHERE name = 'Admin')`,
		`INSERT INTO roles (name, description)
		 SELECT 'Manager', 'Ops manager, KPI dashboards, reports'
		 WHERE NOT EXISTS (SELECT 1 FROM roles WHERE name = 'Manager')`,
		`INSERT INTO roles (name, description)
		 SELECT 'Technician', 'System infrastructure status and maintenance information'
		 WHERE NOT EXISTS (SELECT 1 FROM roles WHERE name = 'Technician')`,
		`INSERT INTO roles (name, description)
		 SELECT 'Operator', 'Operations information and control'
		 WHERE NOT EXISTS (SELECT 1 FROM roles WHERE name = 'Operator')`,
		`INSERT INTO roles (name, description)
		 SELECT 'User', 'Readonly access to specified information'
		 WHERE NOT EXISTS (SELECT 1 FROM roles WHERE name = 'User')`,

		`CREATE TABLE IF NOT EXISTS users (
			id                   INTEGER PRIMARY KEY,
			first_name           TEXT NOT NULL DEFAULT '',
			last_name            TEXT NOT NULL DEFAULT '',
			login_name           TEXT NOT NULL UNIQUE,
			password_hash        TEXT NOT NULL,
			email                TEXT NOT NULL UNIQUE,
			notification_options TEXT NOT NULL DEFAULT '{}',
			active               INTEGER NOT NULL DEFAULT 1,
			last_login           TEXT,
			token_version        INTEGER NOT NULL DEFAULT 1,
			created_at           TEXT NOT NULL,
			updated_at           TEXT NOT NULL
		)`,
		`ALTER TABLE users ADD COLUMN token_version INTEGER NOT NULL DEFAULT 1`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_login_name_unique ON users(login_name)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email_unique ON users(email)`,

		`CREATE TABLE IF NOT EXISTS dashboards (
			id          INTEGER PRIMARY KEY,
			org_id      INTEGER NOT NULL REFERENCES organisations(id) ON DELETE CASCADE,
			name        TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			icon        TEXT NOT NULL DEFAULT '',
			variation   TEXT NOT NULL DEFAULT '',
			device_type TEXT NOT NULL DEFAULT '',
			is_category INTEGER NOT NULL DEFAULT 0,
			parent_id   INTEGER REFERENCES dashboards(id) ON DELETE CASCADE,
			sort_order  INTEGER NOT NULL DEFAULT 0,
			permission  TEXT NOT NULL DEFAULT '',
			widgets     TEXT NOT NULL DEFAULT '[]',
			created_at  TEXT NOT NULL,
			updated_at  TEXT NOT NULL
		)`,
		`ALTER TABLE dashboards ADD COLUMN is_category INTEGER NOT NULL DEFAULT 0`,
		`DROP INDEX IF EXISTS idx_panels_org_name`,
		`DROP INDEX IF EXISTS idx_dashboards_org_name`,
		`CREATE INDEX IF NOT EXISTS idx_dashboards_org_name ON dashboards(org_id, name)`,

		`CREATE TABLE IF NOT EXISTS permissions (
			id         INTEGER PRIMARY KEY,
			org_id     INTEGER NOT NULL REFERENCES organisations(id) ON DELETE CASCADE,
			role       TEXT NOT NULL REFERENCES roles(name),
			ui         TEXT NOT NULL DEFAULT '{}',
			server     TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(org_id, role)
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_permissions_org_role_unique ON permissions(org_id, role)`,

		`CREATE TABLE IF NOT EXISTS system_config (
			id          INTEGER PRIMARY KEY,
			org_id      INTEGER NOT NULL REFERENCES organisations(id) ON DELETE CASCADE,
			version     INTEGER NOT NULL DEFAULT 1,
			config_name TEXT NOT NULL,
			config      TEXT NOT NULL DEFAULT '{}',
			created_at  TEXT NOT NULL,
			updated_at  TEXT NOT NULL,
			UNIQUE(org_id, config_name, version)
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_system_config_org_name_version_unique ON system_config(org_id, config_name, version)`,

		`CREATE TABLE IF NOT EXISTS events (
			id              INTEGER PRIMARY KEY,
			timestamp       TEXT NOT NULL,
			server          TEXT NOT NULL DEFAULT '',
			org_name        TEXT NOT NULL DEFAULT '',
			user_id         INTEGER REFERENCES users(id),
			severity        TEXT NOT NULL,
			notification_id INTEGER NOT NULL DEFAULT 0,
			device          TEXT NOT NULL DEFAULT '',
			message         TEXT NOT NULL DEFAULT '',
			params          TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_events_severity ON events (severity, timestamp)`,
		`CREATE INDEX IF NOT EXISTS idx_events_device   ON events (device, timestamp)`,
		`CREATE INDEX IF NOT EXISTS idx_events_org      ON events (org_name, timestamp)`,
		`CREATE INDEX IF NOT EXISTS idx_events_user     ON events (user_id, timestamp)`,
		`CREATE INDEX IF NOT EXISTS idx_events_notif    ON events (notification_id, timestamp)`,

		`CREATE TABLE IF NOT EXISTS user_organisations (
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			org_id  INTEGER NOT NULL REFERENCES organisations(id) ON DELETE CASCADE,
			PRIMARY KEY (user_id, org_id)
		)`,

		`CREATE TABLE IF NOT EXISTS user_organisation_roles (
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			org_id  INTEGER NOT NULL REFERENCES organisations(id) ON DELETE CASCADE,
			role_id INTEGER NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
			PRIMARY KEY (user_id, org_id, role_id)
		)`,
		`INSERT OR IGNORE INTO user_organisation_roles (user_id, org_id, role_id)
		 SELECT uor.user_id, uor.org_id, MIN(canonical.id)
		 FROM user_organisation_roles uor
		 JOIN roles duplicate ON duplicate.id = uor.role_id
		 JOIN roles canonical ON canonical.name = duplicate.name
		 WHERE uor.role_id IN (
			SELECT id FROM roles
			WHERE id NOT IN (SELECT MIN(id) FROM roles GROUP BY name)
		 )
		 GROUP BY uor.user_id, uor.org_id, duplicate.name`,
		`DELETE FROM user_organisation_roles
		 WHERE role_id IN (
			SELECT id FROM roles
			WHERE id NOT IN (SELECT MIN(id) FROM roles GROUP BY name)
		 )`,
		`DELETE FROM roles
		 WHERE id NOT IN (SELECT MIN(id) FROM roles GROUP BY name)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_roles_name_unique ON roles(name)`,

		`CREATE TABLE IF NOT EXISTS organisation_role_limits (
			org_id  INTEGER NOT NULL REFERENCES organisations(id) ON DELETE CASCADE,
			role_id INTEGER NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
			maximum INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (org_id, role_id)
		)`,

		`CREATE TABLE IF NOT EXISTS org_api_keys (
			id         INTEGER PRIMARY KEY,
			org_id     INTEGER NOT NULL REFERENCES organisations(id) ON DELETE CASCADE,
			name       TEXT NOT NULL,
			key        TEXT NOT NULL UNIQUE,
			key_hash   TEXT,
			key_prefix TEXT NOT NULL DEFAULT '',
			key_last4  TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		)`,
		`ALTER TABLE org_api_keys ADD COLUMN key_hash TEXT`,
		`ALTER TABLE org_api_keys ADD COLUMN key_prefix TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE org_api_keys ADD COLUMN key_last4 TEXT NOT NULL DEFAULT ''`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_org_api_keys_key_unique ON org_api_keys(key)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_org_api_keys_key_hash ON org_api_keys(key_hash) WHERE key_hash IS NOT NULL AND key_hash <> ''`,

		`CREATE TABLE IF NOT EXISTS notification_profiles (
			id           INTEGER PRIMARY KEY,
			org_name     TEXT NOT NULL REFERENCES organisations(name) ON DELETE CASCADE,
			name         TEXT NOT NULL,
			description  TEXT NOT NULL DEFAULT '',
			roles        TEXT NOT NULL DEFAULT '[]',
			users        TEXT NOT NULL DEFAULT '[]',
			ack_required INTEGER NOT NULL DEFAULT 0,
			created_at   TEXT NOT NULL,
			updated_at   TEXT NOT NULL,
			UNIQUE(org_name, name)
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_notification_profiles_org_name_unique ON notification_profiles(org_name, name)`,

		`CREATE TABLE IF NOT EXISTS pdf_templates (
			id            TEXT PRIMARY KEY,
			org_name      TEXT NOT NULL REFERENCES organisations(name) ON DELETE CASCADE,
			name          TEXT NOT NULL,
			description   TEXT NOT NULL DEFAULT '',
			template_json TEXT NOT NULL DEFAULT '{}',
			variables     TEXT NOT NULL DEFAULT '[]',
			created_at    TEXT NOT NULL,
			updated_at    TEXT NOT NULL,
			UNIQUE(org_name, name)
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_pdf_templates_org_name_unique ON pdf_templates(org_name, name)`,

		`CREATE TABLE IF NOT EXISTS tag_calcs (
			id               INTEGER PRIMARY KEY,
			org_name         TEXT NOT NULL REFERENCES organisations(name) ON DELETE CASCADE,
			name             TEXT NOT NULL,
			description      TEXT NOT NULL DEFAULT '',
			output_tag       TEXT NOT NULL,
			expression       TEXT NOT NULL,
			interval_seconds INTEGER NOT NULL DEFAULT 60,
			enabled          INTEGER NOT NULL DEFAULT 1,
			created_at       TEXT NOT NULL,
			updated_at       TEXT NOT NULL,
			UNIQUE(org_name, name)
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_tag_calcs_org_name_unique ON tag_calcs(org_name, name)`,

		`CREATE TABLE IF NOT EXISTS metric_devices (
			id     INTEGER PRIMARY KEY,
			org_id INTEGER NOT NULL REFERENCES organisations(id) ON DELETE CASCADE,
			name   TEXT NOT NULL,
			UNIQUE (org_id, name)
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_metric_devices_org_name_unique ON metric_devices(org_id, name)`,

		`CREATE TABLE IF NOT EXISTS metric_definitions (
			id        INTEGER PRIMARY KEY,
			device_id INTEGER NOT NULL REFERENCES metric_devices(id),
			name      TEXT NOT NULL,
			UNIQUE (device_id, name)
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_metric_definitions_device_name_unique ON metric_definitions(device_id, name)`,

		`CREATE TABLE IF NOT EXISTS device_metrics (
			time      TEXT NOT NULL,
			org_id    INTEGER NOT NULL REFERENCES organisations(id) ON DELETE CASCADE,
			device_id INTEGER NOT NULL REFERENCES metric_devices(id),
			metric_id INTEGER NOT NULL REFERENCES metric_definitions(id),
			value     REAL NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_device_metrics_lookup ON device_metrics (org_id, device_id, metric_id, time DESC)`,

		`CREATE TABLE IF NOT EXISTS scheduled_tasks (
			id               TEXT PRIMARY KEY,
			org_name         TEXT NOT NULL REFERENCES organisations(name) ON DELETE CASCADE,
			name             TEXT NOT NULL,
			description      TEXT NOT NULL DEFAULT '',
			task_type        TEXT NOT NULL,
			task_config      TEXT NOT NULL DEFAULT '{}',
			schedule         TEXT NOT NULL,
			enabled          INTEGER NOT NULL DEFAULT 1,
			last_run_at      TEXT,
			last_run_status  TEXT NOT NULL DEFAULT '',
			last_run_message TEXT NOT NULL DEFAULT '',
			created_at       TEXT NOT NULL,
			updated_at       TEXT NOT NULL,
			UNIQUE(org_name, name)
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_scheduled_tasks_org_name_unique ON scheduled_tasks(org_name, name)`,

		`CREATE TABLE IF NOT EXISTS schedule_run_log (
			id           INTEGER PRIMARY KEY,
			schedule_id  TEXT NOT NULL REFERENCES scheduled_tasks(id) ON DELETE CASCADE,
			org_name     TEXT NOT NULL,
			fired_at     TEXT NOT NULL,
			completed_at TEXT,
			status       TEXT NOT NULL DEFAULT '',
			message      TEXT NOT NULL DEFAULT '',
			output_path  TEXT NOT NULL DEFAULT ''
		)`,
	}

	if err := db.renameLegacyDashboardTable(ctx); err != nil {
		return err
	}

	for _, stmt := range stmts {
		announce := shouldLogSQLiteMigration(stmt)
		if announce {
			log.Printf("SQLite migration running: %s", sqliteMigrationPreview(stmt))
		}
		start := time.Now()
		if _, err := db.db.ExecContext(ctx, stmt); err != nil {
			if strings.Contains(stmt, "ADD COLUMN") && strings.Contains(err.Error(), "duplicate column") {
				continue
			}
			preview := stmt
			if len(preview) > 60 {
				preview = preview[:60] + "..."
			}
			return fmt.Errorf("migration statement failed (%s): %w", preview, err)
		}
		if announce {
			log.Printf("SQLite migration finished in %s: %s", time.Since(start).Round(time.Millisecond), sqliteMigrationPreview(stmt))
		}
	}

	if err := db.seedAdminUser(ctx); err != nil {
		return fmt.Errorf("seeding admin user: %w", err)
	}

	var orgID int
	if err := db.db.QueryRowContext(ctx,
		"SELECT id FROM organisations WHERE name = 'default'",
	).Scan(&orgID); err != nil {
		return fmt.Errorf("finding default org: %w", err)
	}
	if err := db.seedOrgPermissions(ctx, orgID); err != nil {
		return fmt.Errorf("seeding default permissions: %w", err)
	}
	if err := db.seedStarterDashboards(ctx, orgID); err != nil {
		return fmt.Errorf("seeding starter dashboards: %w", err)
	}
	if err := db.ensureViewPermissions(ctx); err != nil {
		return fmt.Errorf("ensuring view permissions: %w", err)
	}
	if err := db.seedNotificationProfiles(ctx, "default"); err != nil {
		return fmt.Errorf("seeding notification profiles: %w", err)
	}
	if err := db.migrateAPIKeyHashes(ctx); err != nil {
		return fmt.Errorf("migrating api key hashes: %w", err)
	}

	// Ensure admin user is a SystemAdmin in every org.
	if err := db.ensureAdminOrgRoles(ctx); err != nil {
		return fmt.Errorf("ensuring admin org roles: %w", err)
	}

	return nil
}

func shouldLogSQLiteMigration(stmt string) bool {
	upper := strings.ToUpper(strings.TrimSpace(stmt))
	return strings.HasPrefix(upper, "CREATE INDEX") || strings.HasPrefix(upper, "CREATE UNIQUE INDEX")
}

func sqliteMigrationPreview(stmt string) string {
	preview := strings.Join(strings.Fields(stmt), " ")
	if len(preview) > 120 {
		return preview[:120] + "..."
	}
	return preview
}

func (db *SQLiteDB) renameLegacyDashboardTable(ctx context.Context) error {
	var legacyCount int
	if err := db.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'panels'`,
	).Scan(&legacyCount); err != nil {
		return err
	}
	if legacyCount == 0 {
		return nil
	}
	var dashboardCount int
	if err := db.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'dashboards'`,
	).Scan(&dashboardCount); err != nil {
		return err
	}
	if dashboardCount > 0 {
		return nil
	}
	if _, err := db.db.ExecContext(ctx, `ALTER TABLE panels RENAME TO dashboards`); err != nil {
		return fmt.Errorf("renaming panels table to dashboards: %w", err)
	}
	return nil
}

func (db *SQLiteDB) seedStarterDashboards(ctx context.Context, orgID int) error {
	var count int
	if err := db.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM dashboards WHERE org_id = ?",
		orgID,
	).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	tx, err := db.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := formatTimestamp(time.Now())
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO dashboards (org_id, name, description, icon, variation, device_type, permission, is_category, parent_id, sort_order, widgets, created_at, updated_at)
		VALUES (?, ?, 'XACT help manual', 'mdi:view-dashboard', '', '', '', 0, NULL, 0, ?, ?, ?)
	`, orgID, sqldb.StarterDashboardName, sqldb.StarterDashboardWidgetsJSON, now, now); err != nil {
		return fmt.Errorf("inserting starter dashboard: %w", err)
	}

	result, err := tx.ExecContext(ctx, `
		INSERT INTO dashboards (org_id, name, description, icon, variation, device_type, permission, is_category, parent_id, sort_order, widgets, created_at, updated_at)
		VALUES (?, ?, 'Monitoring tools', 'mdi:monitor-dashboard', '', '', '', 1, NULL, 1, '[]', ?, ?)
	`, orgID, sqldb.StarterMonitoringCategory, now, now)
	if err != nil {
		return fmt.Errorf("inserting starter monitoring category: %w", err)
	}
	monitoringID, err := result.LastInsertId()
	if err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO dashboards (org_id, name, description, icon, variation, device_type, permission, is_category, parent_id, sort_order, widgets, created_at, updated_at)
		VALUES (?, ?, 'Browse and monitor tags', 'mdi:tag-multiple', '', '', '', 0, ?, 2, ?, ?, ?)
	`, orgID, sqldb.StarterTagViewName, monitoringID, sqldb.StarterTagViewWidgetsJSON, now, now); err != nil {
		return fmt.Errorf("inserting starter tag view: %w", err)
	}

	return tx.Commit()
}

// seedAdminUser creates the bootstrap admin user if it does not exist.
func (db *SQLiteDB) seedAdminUser(ctx context.Context) error {
	var count int
	if err := db.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM users WHERE login_name = 'admin'",
	).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	cred, err := sqldb.ResolveBootstrapAdminPassword(db.bootstrapAdminFile)
	if err != nil {
		return err
	}
	hash := sqldb.UnsetBootstrapAdminHash
	if cred.Set {
		var err error
		hash, err = HashPassword(cred.Password)
		if err != nil {
			return err
		}
	}

	now := formatTimestamp(time.Now())
	result, err := db.db.ExecContext(ctx, `
		INSERT INTO users (first_name, last_name, login_name, password_hash, email, notification_options, active, created_at, updated_at)
		VALUES ('System', 'Admin', 'admin', ?, 'admin@localhost', '{}', 1, ?, ?)
	`, hash, now, now)
	if err != nil {
		return fmt.Errorf("inserting admin user: %w", err)
	}

	userID, err := result.LastInsertId()
	if err != nil {
		return err
	}

	var orgID, roleID int
	if err := db.db.QueryRowContext(ctx,
		"SELECT id FROM organisations WHERE name = 'default'",
	).Scan(&orgID); err != nil {
		return fmt.Errorf("finding default org: %w", err)
	}
	if err := db.db.QueryRowContext(ctx,
		"SELECT id FROM roles WHERE name = 'SystemAdmin'",
	).Scan(&roleID); err != nil {
		return fmt.Errorf("finding SystemAdmin role: %w", err)
	}

	if _, err := db.db.ExecContext(ctx,
		"INSERT INTO user_organisations (user_id, org_id) VALUES (?, ?)",
		userID, orgID,
	); err != nil {
		return fmt.Errorf("adding admin to default org: %w", err)
	}

	if _, err := db.db.ExecContext(ctx,
		"INSERT INTO user_organisation_roles (user_id, org_id, role_id) VALUES (?, ?, ?)",
		userID, orgID, roleID,
	); err != nil {
		return fmt.Errorf("assigning SystemAdmin to admin: %w", err)
	}

	logBootstrapAdminCredential(cred)
	return nil
}

func bootstrapAdminPasswordFile(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || path == ":memory:" || strings.HasPrefix(path, "file:") {
		return ""
	}
	return filepath.Join(filepath.Dir(path), "bootstrap-admin-password.txt")
}

func logBootstrapAdminCredential(cred sqldb.AdminBootstrapCredential) {
	if cred.Set {
		log.Printf("Created bootstrap admin user 'admin' using password from %s", cred.Source)
		return
	}
	log.Printf("Created bootstrap admin user 'admin' with password unset; first browser login must set it")
}

// ensureAdminOrgRoles ensures the admin user has SystemAdmin role in all organisations.
func (db *SQLiteDB) ensureAdminOrgRoles(ctx context.Context) error {
	var adminID int
	err := db.db.QueryRowContext(ctx,
		"SELECT id FROM users WHERE login_name = 'admin'",
	).Scan(&adminID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}

	var roleID int
	if err := db.db.QueryRowContext(ctx,
		"SELECT id FROM roles WHERE name = 'SystemAdmin'",
	).Scan(&roleID); err != nil {
		return err
	}

	rows, err := db.db.QueryContext(ctx, "SELECT id FROM organisations")
	if err != nil {
		return err
	}
	var orgIDs []int
	for rows.Next() {
		var id int
		rows.Scan(&id)
		orgIDs = append(orgIDs, id)
	}
	rows.Close()

	for _, orgID := range orgIDs {
		if _, err := db.db.ExecContext(ctx,
			"INSERT OR IGNORE INTO user_organisations (user_id, org_id) VALUES (?, ?)",
			adminID, orgID,
		); err != nil {
			return err
		}
		if _, err := db.db.ExecContext(ctx,
			"INSERT OR IGNORE INTO user_organisation_roles (user_id, org_id, role_id) VALUES (?, ?, ?)",
			adminID, orgID, roleID,
		); err != nil {
			return err
		}
	}
	return nil
}

// seedOrgPermissions inserts the default role permissions for an organisation.
func (db *SQLiteDB) seedOrgPermissions(ctx context.Context, orgID int) error {
	type seed struct {
		role string
		ui   string
	}
	seeds := []seed{
		{"SystemAdmin", `{"dashboards-setup":{"read":true,"edit":true},"dashboard-container":{"edit":true},"widget-default":{"view":true,"configure":true},"organisations":{"view":true,"change":true},"permissions":{"view":true,"manage":true},"users":{"view":true,"manage":true},"nodes":{"read":true,"write":true},"tags":{"read":true,"write":true},"logs":{"read":true,"write":true},"reports":{"view":true,"manage":true},"notifications":{"view":true,"manage":true},"scheduler":{"view":true,"manage":true},"tagcalcs":{"view":true,"manage":true}}`},
		{"Admin", `{"dashboards-setup":{"read":true,"edit":true},"dashboard-container":{"edit":true},"widget-default":{"view":true,"configure":true},"organisations":{"view":false,"change":false},"permissions":{"view":true,"manage":true},"users":{"view":true,"manage":true},"nodes":{"read":true,"write":true},"tags":{"read":true,"write":true},"logs":{"read":true,"write":true},"reports":{"view":true,"manage":true},"notifications":{"view":true,"manage":true},"scheduler":{"view":true,"manage":true},"tagcalcs":{"view":true,"manage":true}}`},
		{"Manager", `{"dashboards-setup":{"read":true,"edit":true},"dashboard-container":{"edit":true},"widget-default":{"view":true,"configure":true},"organisations":{"view":false,"change":false},"permissions":{"view":false,"manage":false},"users":{"view":false,"manage":false},"nodes":{"read":true,"write":true},"tags":{"read":true,"write":true},"logs":{"read":true,"write":false},"reports":{"view":true,"manage":true},"notifications":{"view":false,"manage":false},"scheduler":{"view":true,"manage":true},"tagcalcs":{"view":false,"manage":false}}`},
		{"Technician", `{"dashboards-setup":{"read":true,"edit":false},"dashboard-container":{"edit":true},"widget-default":{"view":true,"configure":true},"organisations":{"view":false,"change":false},"permissions":{"view":false,"manage":false},"users":{"view":false,"manage":false},"nodes":{"read":true,"write":true},"tags":{"read":true,"write":true},"logs":{"read":true,"write":false},"reports":{"view":false,"manage":false},"notifications":{"view":false,"manage":false},"scheduler":{"view":false,"manage":false},"tagcalcs":{"view":false,"manage":false}}`},
		{"Operator", `{"dashboards-setup":{"read":true,"edit":false},"dashboard-container":{"edit":false},"widget-default":{"view":true,"configure":false},"organisations":{"view":false,"change":false},"permissions":{"view":false,"manage":false},"users":{"view":false,"manage":false},"nodes":{"read":true,"write":false},"tags":{"read":true,"write":false},"logs":{"read":false,"write":false},"reports":{"view":false,"manage":false},"notifications":{"view":false,"manage":false},"scheduler":{"view":false,"manage":false},"tagcalcs":{"view":false,"manage":false}}`},
		{"User", `{"dashboards-setup":{"read":true,"edit":false},"dashboard-container":{"edit":false},"widget-default":{"view":true,"configure":false},"organisations":{"view":false,"change":false},"permissions":{"view":false,"manage":false},"users":{"view":false,"manage":false},"nodes":{"read":true,"write":false},"tags":{"read":true,"write":false},"logs":{"read":false,"write":false},"reports":{"view":false,"manage":false},"notifications":{"view":false,"manage":false},"scheduler":{"view":false,"manage":false},"tagcalcs":{"view":false,"manage":false}}`},
	}
	now := formatTimestamp(time.Now())
	for _, s := range seeds {
		if _, err := db.db.ExecContext(ctx,
			`INSERT OR IGNORE INTO permissions (org_id, role, ui, server, created_at, updated_at) VALUES (?, ?, ?, '{}', ?, ?)`,
			orgID, s.role, s.ui, now, now,
		); err != nil {
			return fmt.Errorf("seeding permission for %s: %w", s.role, err)
		}
	}
	return nil
}

func (db *SQLiteDB) ensureViewPermissions(ctx context.Context) error {
	rows, err := db.db.QueryContext(ctx, `SELECT id, role, ui FROM permissions`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type update struct {
		id int
		ui string
	}
	var updates []update
	for rows.Next() {
		var id int
		var role, raw string
		if err := rows.Scan(&id, &role, &raw); err != nil {
			return err
		}
		var ui map[string]map[string]bool
		if err := json.Unmarshal([]byte(raw), &ui); err != nil {
			continue
		}
		changed := false
		if legacy, ok := ui["panel-container"]; ok {
			if _, exists := ui["dashboard-container"]; !exists {
				ui["dashboard-container"] = legacy
			}
			delete(ui, "panel-container")
			changed = true
		}
		viewSources := map[string]string{
			"organisations": "change",
			"permissions":   "manage",
			"users":         "manage",
			"reports":       "manage",
			"notifications": "manage",
			"scheduler":     "manage",
			"tagcalcs":      "manage",
		}
		for resource, sourceAction := range viewSources {
			perms, ok := ui[resource]
			if !ok {
				perms = map[string]bool{}
				ui[resource] = perms
			}
			if _, ok := perms["view"]; ok {
				continue
			}
			perms["view"] = perms[sourceAction]
			changed = true
		}
		logs, ok := ui["logs"]
		if !ok {
			logs = map[string]bool{}
			ui["logs"] = logs
			changed = true
		}
		if _, ok := logs["write"]; !ok {
			logs["write"] = role == "SystemAdmin" || role == "Admin"
			changed = true
		}
		if !changed {
			continue
		}
		encoded, err := json.Marshal(ui)
		if err != nil {
			return err
		}
		updates = append(updates, update{id: id, ui: string(encoded)})
	}
	if err := rows.Err(); err != nil {
		return err
	}

	now := formatTimestamp(time.Now())
	for _, u := range updates {
		if _, err := db.db.ExecContext(ctx, `UPDATE permissions SET ui = ?, updated_at = ? WHERE id = ?`, u.ui, now, u.id); err != nil {
			return err
		}
	}
	return nil
}

// seedNotificationProfiles inserts the default notification profiles for an organisation.
func (db *SQLiteDB) seedNotificationProfiles(ctx context.Context, orgName string) error {
	now := formatTimestamp(time.Now())
	type seed struct{ name, description, roles string }
	seeds := []seed{
		{"SysAdmin", "Server issues", `["SystemAdmin"]`},
		{"Manager", "Operational issues", `["Manager"]`},
		{"Technician", "Technical issues", `["Technician"]`},
	}
	for _, s := range seeds {
		if _, err := db.db.ExecContext(ctx,
			`INSERT OR IGNORE INTO notification_profiles (org_name, name, description, roles, users, ack_required, created_at, updated_at)
			 VALUES (?, ?, ?, ?, '[]', 0, ?, ?)`,
			orgName, s.name, s.description, s.roles, now, now,
		); err != nil {
			return err
		}
	}
	return nil
}

// resolveOrgID looks up the organisation ID by name.
func (db *SQLiteDB) resolveOrgID(ctx context.Context, org string) (int, error) {
	var orgID int
	err := db.db.QueryRowContext(ctx, "SELECT id FROM organisations WHERE name = ?", org).Scan(&orgID)
	if err != nil {
		return 0, fmt.Errorf("organisation %q not found: %w", org, err)
	}
	return orgID, nil
}

// inClause returns an SQL IN placeholder string "(?,?,?)" for n elements.
func inClause(n int) string {
	if n == 0 {
		return "(NULL)"
	}
	b := make([]byte, 0, n*2+1)
	b = append(b, '(')
	for i := 0; i < n; i++ {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, '?')
	}
	b = append(b, ')')
	return string(b)
}

// newUUID generates a random UUID v4 string.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func newUUID() string {
	var b [16]byte
	rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// formatTimestamp formats a time.Time as RFC3339Nano for storage.
func formatTimestamp(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

// parseTimestamp parses an RFC3339Nano string from storage.
func parseTimestamp(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

// ---- Dashboard methods ----

// ListDashboards returns dashboard metadata (without widgets) for an organisation.
func (db *SQLiteDB) ListDashboards(ctx context.Context, org string) ([]sqldb.DashboardMeta, error) {
	rows, err := db.db.QueryContext(ctx, `
		SELECT p.id, p.name, p.description, p.icon, p.variation,
		       p.device_type, p.permission, p.is_category, p.parent_id, p.sort_order
		FROM dashboards p
		JOIN organisations o ON o.id = p.org_id
		WHERE o.name = ?
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
func (db *SQLiteDB) GetDashboard(ctx context.Context, org string, id int) (*sqldb.Dashboard, error) {
	var p sqldb.Dashboard
	var widgetsStr string
	err := db.db.QueryRowContext(ctx, `
		SELECT p.id, p.name, p.description, p.icon, p.variation,
		       p.device_type, p.permission, p.is_category, p.parent_id, p.sort_order, p.widgets
		FROM dashboards p
		JOIN organisations o ON o.id = p.org_id
		WHERE o.name = ? AND p.id = ?
	`, org, id).Scan(&p.ID, &p.Name, &p.Description, &p.Icon,
		&p.Variation, &p.DeviceType, &p.Permission, &p.IsCategory, &p.ParentID, &p.SortOrder, &widgetsStr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting dashboard %d: %w", id, err)
	}
	p.Widgets = json.RawMessage(widgetsStr)
	return &p, nil
}

// CreateDashboard creates a new dashboard for the given organisation.
func (db *SQLiteDB) CreateDashboard(ctx context.Context, org string, dashboard *sqldb.Dashboard) error {
	orgID, err := db.resolveOrgID(ctx, org)
	if err != nil {
		return err
	}
	widgets := dashboard.Widgets
	if widgets == nil {
		widgets = json.RawMessage("[]")
	}
	now := formatTimestamp(time.Now())
	result, err := db.db.ExecContext(ctx, `
		INSERT INTO dashboards (org_id, name, description, icon, variation, device_type, permission, is_category, parent_id, sort_order, widgets, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, orgID, dashboard.Name, dashboard.Description, dashboard.Icon,
		dashboard.Variation, dashboard.DeviceType, dashboard.Permission, dashboard.IsCategory, dashboard.ParentID, dashboard.SortOrder,
		string(widgets), now, now)
	if err != nil {
		return fmt.Errorf("creating dashboard %q: %w", dashboard.Name, err)
	}
	if id, err := result.LastInsertId(); err == nil {
		dashboard.ID = int(id)
	}
	return nil
}

// UpdateDashboard updates an existing dashboard identified by organisation and id.
func (db *SQLiteDB) UpdateDashboard(ctx context.Context, org string, id int, dashboard *sqldb.Dashboard) error {
	orgID, err := db.resolveOrgID(ctx, org)
	if err != nil {
		return err
	}
	widgets := dashboard.Widgets
	if widgets == nil {
		widgets = json.RawMessage("[]")
	}
	now := formatTimestamp(time.Now())
	result, err := db.db.ExecContext(ctx, `
		UPDATE dashboards SET
			name = ?, description = ?, icon = ?, variation = ?,
			device_type = ?, permission = ?, is_category = ?, parent_id = ?, sort_order = ?, widgets = ?, updated_at = ?
		WHERE org_id = ? AND id = ?
	`, dashboard.Name, dashboard.Description, dashboard.Icon,
		dashboard.Variation, dashboard.DeviceType, dashboard.Permission, dashboard.IsCategory, dashboard.ParentID, dashboard.SortOrder,
		string(widgets), now, orgID, id)
	if err != nil {
		return fmt.Errorf("updating dashboard %d: %w", id, err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("dashboard %d not found", id)
	}
	return nil
}

// DeleteDashboard removes a dashboard identified by organisation and id.
func (db *SQLiteDB) DeleteDashboard(ctx context.Context, org string, id int) error {
	orgID, err := db.resolveOrgID(ctx, org)
	if err != nil {
		return err
	}
	result, err := db.db.ExecContext(ctx,
		"DELETE FROM dashboards WHERE org_id = ? AND id = ?", orgID, id)
	if err != nil {
		return fmt.Errorf("deleting dashboard %d: %w", id, err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("dashboard %d not found", id)
	}
	return nil
}

// ---- Permission methods ----

// ListPermissions returns all role permission records for an organisation.
func (db *SQLiteDB) ListPermissions(ctx context.Context, org string) ([]sqldb.RolePermissions, error) {
	rows, err := db.db.QueryContext(ctx, `
		SELECT p.role, p.ui, p.server
		FROM permissions p
		JOIN organisations o ON o.id = p.org_id
		WHERE o.name = ?
		ORDER BY p.role
	`, org)
	if err != nil {
		return nil, fmt.Errorf("listing permissions: %w", err)
	}
	defer rows.Close()

	var perms []sqldb.RolePermissions
	for rows.Next() {
		var rp sqldb.RolePermissions
		var uiStr, serverStr string
		if err := rows.Scan(&rp.Role, &uiStr, &serverStr); err != nil {
			return nil, fmt.Errorf("scanning permission row: %w", err)
		}
		rp.UI = json.RawMessage(uiStr)
		rp.Server = json.RawMessage(serverStr)
		perms = append(perms, rp)
	}
	return perms, rows.Err()
}

// GetPermissions returns the permission record for a specific role in an organisation.
func (db *SQLiteDB) GetPermissions(ctx context.Context, org string, role string) (*sqldb.RolePermissions, error) {
	var rp sqldb.RolePermissions
	var uiStr, serverStr string
	err := db.db.QueryRowContext(ctx, `
		SELECT p.role, p.ui, p.server
		FROM permissions p
		JOIN organisations o ON o.id = p.org_id
		WHERE o.name = ? AND p.role = ?
	`, org, role).Scan(&rp.Role, &uiStr, &serverStr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting permissions for role %q: %w", role, err)
	}
	rp.UI = json.RawMessage(uiStr)
	rp.Server = json.RawMessage(serverStr)
	return &rp, nil
}

// UpdatePermissions updates the permission record for a specific role.
func (db *SQLiteDB) UpdatePermissions(ctx context.Context, org string, role string, perms *sqldb.RolePermissions) error {
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
	now := formatTimestamp(time.Now())
	result, err := db.db.ExecContext(ctx,
		"UPDATE permissions SET ui = ?, server = ?, updated_at = ? WHERE org_id = ? AND role = ?",
		string(ui), string(server), now, orgID, role)
	if err != nil {
		return fmt.Errorf("updating permissions for role %q: %w", role, err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("role %q not found", role)
	}
	return nil
}

// ---- Config methods ----

// SaveConfig upserts a named config blob for an organisation.
func (db *SQLiteDB) SaveConfig(ctx context.Context, org string, configName string, config json.RawMessage) error {
	orgID, err := db.resolveOrgID(ctx, org)
	if err != nil {
		return err
	}
	now := formatTimestamp(time.Now())
	_, err = db.db.ExecContext(ctx, `
		INSERT INTO system_config (org_id, config_name, version, config, created_at, updated_at)
		VALUES (?, ?, 1, ?, ?, ?)
		ON CONFLICT (org_id, config_name, version) DO UPDATE
			SET config = excluded.config, updated_at = excluded.updated_at
	`, orgID, configName, string(config), now, now)
	if err != nil {
		return fmt.Errorf("saving config %q: %w", configName, err)
	}
	return nil
}

// LoadConfig retrieves the latest config blob by org and name. Returns nil if not found.
func (db *SQLiteDB) LoadConfig(ctx context.Context, org string, configName string) (json.RawMessage, error) {
	var configStr string
	err := db.db.QueryRowContext(ctx, `
		SELECT sc.config
		FROM system_config sc
		JOIN organisations o ON o.id = sc.org_id
		WHERE o.name = ? AND sc.config_name = ?
		ORDER BY sc.version DESC
		LIMIT 1
	`, org, configName).Scan(&configStr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("loading config %q: %w", configName, err)
	}
	return json.RawMessage(configStr), nil
}

// ── Scheduler ─────────────────────────────────────────────────────────────────

func (db *SQLiteDB) ListScheduledTasks(ctx context.Context, org string) ([]sqldb.ScheduledTask, error) {
	rows, err := db.db.QueryContext(ctx, `
		SELECT id, org_name, name, description, task_type, task_config, schedule,
		       enabled, last_run_at, last_run_status, last_run_message, created_at, updated_at
		FROM scheduled_tasks WHERE org_name = ? ORDER BY name
	`, org)
	if err != nil {
		return nil, fmt.Errorf("listing scheduled tasks: %w", err)
	}
	defer rows.Close()

	var tasks []sqldb.ScheduledTask
	for rows.Next() {
		var t sqldb.ScheduledTask
		var taskConfig string
		var enabled int
		var createdAt, updatedAt string
		var lastRunAtPtr *string
		if err := rows.Scan(&t.ID, &t.OrgName, &t.Name, &t.Description, &t.TaskType,
			&taskConfig, &t.Schedule, &enabled, &lastRunAtPtr,
			&t.LastRunStatus, &t.LastRunMessage, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		t.TaskConfig = json.RawMessage(taskConfig)
		t.Enabled = enabled != 0
		t.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		t.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
		if lastRunAtPtr != nil {
			if ts, err := time.Parse(time.RFC3339Nano, *lastRunAtPtr); err == nil {
				t.LastRunAt = &ts
			}
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func (db *SQLiteDB) GetScheduledTask(ctx context.Context, org string, id string) (*sqldb.ScheduledTask, error) {
	var t sqldb.ScheduledTask
	var taskConfig string
	var enabled int
	var createdAt, updatedAt string
	var lastRunAtPtr *string
	err := db.db.QueryRowContext(ctx, `
		SELECT id, org_name, name, description, task_type, task_config, schedule,
		       enabled, last_run_at, last_run_status, last_run_message, created_at, updated_at
		FROM scheduled_tasks WHERE org_name = ? AND id = ?
	`, org, id).Scan(&t.ID, &t.OrgName, &t.Name, &t.Description, &t.TaskType,
		&taskConfig, &t.Schedule, &enabled, &lastRunAtPtr,
		&t.LastRunStatus, &t.LastRunMessage, &createdAt, &updatedAt)
	t.TaskConfig = json.RawMessage(taskConfig)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting scheduled task: %w", err)
	}
	t.Enabled = enabled != 0
	t.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	t.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	if lastRunAtPtr != nil {
		if ts, err2 := time.Parse(time.RFC3339Nano, *lastRunAtPtr); err2 == nil {
			t.LastRunAt = &ts
		}
	}
	return &t, nil
}

func (db *SQLiteDB) CreateScheduledTask(ctx context.Context, org string, t *sqldb.ScheduledTask) error {
	t.ID = newUUID()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	t.CreatedAt, _ = time.Parse(time.RFC3339Nano, now)
	t.UpdatedAt = t.CreatedAt
	_, err := db.db.ExecContext(ctx, `
		INSERT INTO scheduled_tasks (id, org_name, name, description, task_type, task_config, schedule, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, t.ID, org, t.Name, t.Description, t.TaskType, string(t.TaskConfig), t.Schedule,
		boolToInt(t.Enabled), now, now)
	return err
}

func (db *SQLiteDB) UpdateScheduledTask(ctx context.Context, org string, id string, t *sqldb.ScheduledTask) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := db.db.ExecContext(ctx, `
		UPDATE scheduled_tasks
		SET name = ?, description = ?, task_type = ?, task_config = ?,
		    schedule = ?, enabled = ?, updated_at = ?
		WHERE org_name = ? AND id = ?
	`, t.Name, t.Description, t.TaskType, string(t.TaskConfig), t.Schedule,
		boolToInt(t.Enabled), now, org, id)
	if err != nil {
		return fmt.Errorf("updating scheduled task: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("scheduled task %q not found", id)
	}
	return nil
}

func (db *SQLiteDB) DeleteScheduledTask(ctx context.Context, org string, id string) error {
	res, err := db.db.ExecContext(ctx, `DELETE FROM scheduled_tasks WHERE org_name = ? AND id = ?`, org, id)
	if err != nil {
		return fmt.Errorf("deleting scheduled task: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("scheduled task %q not found", id)
	}
	return nil
}

func (db *SQLiteDB) UpdateScheduledTaskStatus(ctx context.Context, id string, status string, message string, runAt time.Time) error {
	_, err := db.db.ExecContext(ctx, `
		UPDATE scheduled_tasks
		SET last_run_at = ?, last_run_status = ?, last_run_message = ?, updated_at = ?
		WHERE id = ?
	`, runAt.UTC().Format(time.RFC3339Nano), status, message, time.Now().UTC().Format(time.RFC3339Nano), id)
	return err
}

func (db *SQLiteDB) AppendScheduleRunLog(ctx context.Context, entry *sqldb.ScheduleRunLog) error {
	res, err := db.db.ExecContext(ctx, `
		INSERT INTO schedule_run_log (schedule_id, org_name, fired_at, status, message, output_path)
		VALUES (?, ?, ?, ?, ?, ?)
	`, entry.ScheduleID, entry.OrgName, entry.FiredAt.UTC().Format(time.RFC3339Nano),
		entry.Status, entry.Message, entry.OutputPath)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	entry.ID = int(id)
	return nil
}

func (db *SQLiteDB) UpdateScheduleRunLog(ctx context.Context, id int, completedAt time.Time, status, message, outputPath string) error {
	_, err := db.db.ExecContext(ctx, `
		UPDATE schedule_run_log
		SET completed_at = ?, status = ?, message = ?, output_path = ?
		WHERE id = ?
	`, completedAt.UTC().Format(time.RFC3339Nano), status, message, outputPath, id)
	return err
}

func (db *SQLiteDB) ListScheduleRunLog(ctx context.Context, scheduleID string, limit int) ([]sqldb.ScheduleRunLog, error) {
	rows, err := db.db.QueryContext(ctx, `
		SELECT id, schedule_id, org_name, fired_at, completed_at, status, message, output_path
		FROM schedule_run_log WHERE schedule_id = ?
		ORDER BY fired_at DESC LIMIT ?
	`, scheduleID, limit)
	if err != nil {
		return nil, fmt.Errorf("listing run log: %w", err)
	}
	defer rows.Close()

	var entries []sqldb.ScheduleRunLog
	for rows.Next() {
		var e sqldb.ScheduleRunLog
		var firedAt string
		var completedAt *string
		if err := rows.Scan(&e.ID, &e.ScheduleID, &e.OrgName, &firedAt, &completedAt,
			&e.Status, &e.Message, &e.OutputPath); err != nil {
			return nil, err
		}
		e.FiredAt, _ = time.Parse(time.RFC3339Nano, firedAt)
		if completedAt != nil {
			if ts, err2 := time.Parse(time.RFC3339Nano, *completedAt); err2 == nil {
				e.CompletedAt = &ts
			}
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
