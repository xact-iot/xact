package psql

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/xact-iot/xact/sqldb"
)

// parseBOX parses a PostgreSQL BOX text representation into an OrgArea.
// PostgreSQL normalises BOX output to "(max_x,max_y),(min_x,min_y)",
// which maps to (east,north),(west,south) for geographic coordinates.
func parseBOX(s string) (*sqldb.OrgArea, error) {
	var x1, y1, x2, y2 float64
	if _, err := fmt.Sscanf(s, "(%f,%f),(%f,%f)", &x1, &y1, &x2, &y2); err != nil {
		return nil, fmt.Errorf("parsing box %q: %w", s, err)
	}
	return &sqldb.OrgArea{East: x1, North: y1, West: x2, South: y2}, nil
}

// formatBOX formats an OrgArea as a PostgreSQL BOX literal string suitable
// for use with a $n::box parameter cast.
func formatBOX(a *sqldb.OrgArea) string {
	return fmt.Sprintf("((%f,%f),(%f,%f))", a.East, a.North, a.West, a.South)
}

// ListOrganisations returns all organisations ordered by name.
func (db *PostgresDB) ListOrganisations(ctx context.Context) ([]sqldb.Organisation, error) {
	rows, err := db.pool.Query(ctx,
		`SELECT id, name, display_name, active, logo_data, favicon, area::text FROM organisations ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("listing organisations: %w", err)
	}
	defer rows.Close()

	var orgs []sqldb.Organisation
	for rows.Next() {
		var o sqldb.Organisation
		var areaText *string
		if err := rows.Scan(&o.ID, &o.Name, &o.DisplayName, &o.Active, &o.Logo, &o.Favicon, &areaText); err != nil {
			return nil, fmt.Errorf("scanning organisation: %w", err)
		}
		if areaText != nil {
			o.Area, _ = parseBOX(*areaText)
		}
		orgs = append(orgs, o)
	}
	return orgs, rows.Err()
}

// GetOrganisation returns a single organisation by name, or nil if not found.
func (db *PostgresDB) GetOrganisation(ctx context.Context, name string) (*sqldb.Organisation, error) {
	var o sqldb.Organisation
	var areaText *string
	err := db.pool.QueryRow(ctx,
		`SELECT id, name, display_name, active, logo_data, favicon, area::text FROM organisations WHERE name = $1`, name,
	).Scan(&o.ID, &o.Name, &o.DisplayName, &o.Active, &o.Logo, &o.Favicon, &areaText)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting organisation %q: %w", name, err)
	}
	if areaText != nil {
		o.Area, _ = parseBOX(*areaText)
	}
	return &o, nil
}

// CreateOrganisation inserts a new organisation, setting org.ID on success.
// It also seeds the default role permissions for the new org so that users
// with existing roles can access it without a manual permissions setup.
func (db *PostgresDB) CreateOrganisation(ctx context.Context, org *sqldb.Organisation) error {
	var areaParam any
	if org.Area != nil {
		areaParam = formatBOX(org.Area)
	}
	if err := db.pool.QueryRow(ctx,
		`INSERT INTO organisations (name, display_name, active, logo_data, favicon, area)
		 VALUES ($1, $2, $3, $4, $5, $6::box) RETURNING id`,
		org.Name, org.DisplayName, org.Active, org.Logo, org.Favicon, areaParam,
	).Scan(&org.ID); err != nil {
		return err
	}
	if err := db.seedNotificationProfiles(ctx, org.Name); err != nil {
		return err
	}
	if err := db.seedOrgPermissions(ctx, org.ID); err != nil {
		return err
	}
	return db.seedStarterDashboards(ctx, org.ID)
}

// seedOrgPermissions inserts the default role permission rows for an organisation.
// Mirrors the defaults seeded for the "default" org in the migration.
// Uses ON CONFLICT DO NOTHING so it is safe to call on an org that already has rows.
func (db *PostgresDB) seedOrgPermissions(ctx context.Context, orgID int) error {
	_, err := db.pool.Exec(ctx, `
		INSERT INTO permissions (org_id, role, ui, server)
		SELECT $1, r.role, r.ui, '{}'::jsonb
		FROM (VALUES
			('SystemAdmin', '{"dashboards-setup":{"read":true,"edit":true},"dashboard-container":{"edit":true},"widget-default":{"view":true,"configure":true},"organisations":{"view":true,"change":true},"permissions":{"manage":true},"users":{"manage":true},"nodes":{"read":true,"write":true},"tags":{"read":true,"write":true},"logs":{"read":true,"write":true}}'::jsonb),
			('Admin',       '{"dashboards-setup":{"read":true,"edit":true},"dashboard-container":{"edit":true},"widget-default":{"view":true,"configure":true},"organisations":{"view":false,"change":false},"permissions":{"manage":true},"users":{"manage":true},"nodes":{"read":true,"write":true},"tags":{"read":true,"write":true},"logs":{"read":true,"write":true}}'::jsonb),
			('Manager',     '{"dashboards-setup":{"read":true,"edit":true},"dashboard-container":{"edit":true},"widget-default":{"view":true,"configure":true},"organisations":{"view":false,"change":false},"permissions":{"manage":false},"users":{"manage":false},"nodes":{"read":true,"write":true},"tags":{"read":true,"write":true},"logs":{"read":true,"write":false}}'::jsonb),
			('Technician',  '{"dashboards-setup":{"read":true,"edit":false},"dashboard-container":{"edit":true},"widget-default":{"view":true,"configure":true},"organisations":{"view":false,"change":false},"permissions":{"manage":false},"users":{"manage":false},"nodes":{"read":true,"write":true},"tags":{"read":true,"write":true},"logs":{"read":true,"write":false}}'::jsonb),
			('Operator',    '{"dashboards-setup":{"read":true,"edit":false},"dashboard-container":{"edit":false},"widget-default":{"view":true,"configure":false},"organisations":{"view":false,"change":false},"permissions":{"manage":false},"users":{"manage":false},"nodes":{"read":true,"write":false},"tags":{"read":true,"write":false},"logs":{"read":false,"write":false}}'::jsonb),
			('User',        '{"dashboards-setup":{"read":true,"edit":false},"dashboard-container":{"edit":false},"widget-default":{"view":true,"configure":false},"organisations":{"view":false,"change":false},"permissions":{"manage":false},"users":{"manage":false},"nodes":{"read":true,"write":false},"tags":{"read":true,"write":false},"logs":{"read":false,"write":false}}'::jsonb)
		) AS r(role, ui)
		ON CONFLICT (org_id, role) DO NOTHING
	`, orgID)
	return err
}

// seedNotificationProfiles inserts the default notification profiles for an organisation.
func (db *PostgresDB) seedNotificationProfiles(ctx context.Context, orgName string) error {
	_, err := db.pool.Exec(ctx, `
		INSERT INTO notification_profiles (org_name, name, description, roles)
		SELECT $1, v.name, v.description, v.roles
		FROM (VALUES
			('SysAdmin',   'Server issues',      '["SystemAdmin"]'::jsonb),
			('Manager',    'Operational issues', '["Manager"]'::jsonb),
			('Technician', 'Technical issues',    '["Technician"]'::jsonb)
		) AS v(name, description, roles)
		WHERE NOT EXISTS (
			SELECT 1 FROM notification_profiles np WHERE np.org_name = $1 AND np.name = v.name
		)
		ON CONFLICT (org_name, name) DO NOTHING
	`, orgName)
	return err
}

// UpdateOrganisation updates the organisation identified by name.
// Name is immutable; only display_name, active, and area are written.
func (db *PostgresDB) UpdateOrganisation(ctx context.Context, name string, org *sqldb.Organisation) error {
	var areaParam any
	if org.Area != nil {
		areaParam = formatBOX(org.Area)
	}
	tag, err := db.pool.Exec(ctx,
		`UPDATE organisations SET display_name = $2, active = $3, logo_data = $4, favicon = $5, area = $6::box WHERE name = $1`,
		name, org.DisplayName, org.Active, org.Logo, org.Favicon, areaParam,
	)
	if err != nil {
		return fmt.Errorf("updating organisation %q: %w", name, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("organisation %q not found", name)
	}
	return nil
}

// DeleteOrganisation removes an organisation. The "default" org cannot be deleted.
func (db *PostgresDB) DeleteOrganisation(ctx context.Context, name string) error {
	if name == "default" {
		return fmt.Errorf("cannot delete the default organisation")
	}
	tag, err := db.pool.Exec(ctx, `DELETE FROM organisations WHERE name = $1`, name)
	if err != nil {
		return fmt.Errorf("deleting organisation %q: %w", name, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("organisation %q not found", name)
	}
	return nil
}

// ── API keys ──────────────────────────────────────────────────────────────────

// ListAPIKeys returns all API keys for an organisation.
func (db *PostgresDB) ListAPIKeys(ctx context.Context, orgName string) ([]sqldb.APIKey, error) {
	rows, err := db.pool.Query(ctx, `
		SELECT k.id, o.name, k.name, k.key_prefix, k.key_last4, k.created_at
		FROM org_api_keys k
		JOIN organisations o ON o.id = k.org_id
		WHERE o.name = $1
		ORDER BY k.created_at`, orgName)
	if err != nil {
		return nil, fmt.Errorf("listing api keys for %q: %w", orgName, err)
	}
	defer rows.Close()

	var keys []sqldb.APIKey
	for rows.Next() {
		var k sqldb.APIKey
		if err := rows.Scan(&k.ID, &k.OrgName, &k.Name, &k.KeyPrefix, &k.KeyLast4, &k.CreatedAt); err != nil {
			return nil, err
		}
		k.Key = sqldb.MaskAPIKey(k.KeyPrefix, k.KeyLast4)
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// CreateAPIKey generates a new random API key and stores it for the given org.
// Returns an error if the org already has 5 keys.
func (db *PostgresDB) CreateAPIKey(ctx context.Context, orgName, name string) (*sqldb.APIKey, error) {
	// Enforce limit of 5 keys per org.
	var count int
	err := db.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM org_api_keys k
		JOIN organisations o ON o.id = k.org_id
		WHERE o.name = $1`, orgName).Scan(&count)
	if err != nil {
		return nil, fmt.Errorf("counting api keys: %w", err)
	}
	if count >= 5 {
		return nil, fmt.Errorf("organisation %q already has 5 API keys (maximum)", orgName)
	}

	keyValue, err := sqldb.NewRawAPIKey()
	if err != nil {
		return nil, fmt.Errorf("generating api key: %w", err)
	}
	keyHash := sqldb.HashAPIKey(keyValue)
	keyPrefix := sqldb.APIKeyPrefix(keyValue)
	keyLast4 := sqldb.APIKeyLast4(keyValue)

	var k sqldb.APIKey
	err = db.pool.QueryRow(ctx, `
		INSERT INTO org_api_keys (org_id, name, key, key_hash, key_prefix, key_last4)
		SELECT id, $2, $3, $4, $5, $6 FROM organisations WHERE name = $1
		RETURNING id, $1, $2, created_at`,
		orgName, name, sqldb.APIKeyPlaceholder(0, keyHash), keyHash, keyPrefix, keyLast4,
	).Scan(&k.ID, &k.OrgName, &k.Name, &k.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("creating api key: %w", err)
	}
	_, _ = db.pool.Exec(ctx,
		"UPDATE org_api_keys SET key = $2 WHERE id = $1",
		k.ID, sqldb.APIKeyPlaceholder(k.ID, keyHash))
	k.Key = keyValue
	k.KeyPrefix = keyPrefix
	k.KeyLast4 = keyLast4
	return &k, nil
}

// DeleteAPIKey removes an API key by ID within an organisation.
func (db *PostgresDB) DeleteAPIKey(ctx context.Context, orgName string, id int) error {
	tag, err := db.pool.Exec(ctx, `
		DELETE FROM org_api_keys k
		USING organisations o
		WHERE k.org_id = o.id AND o.name = $1 AND k.id = $2`,
		orgName, id)
	if err != nil {
		return fmt.Errorf("deleting api key %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("api key %d not found in organisation %q", id, orgName)
	}
	return nil
}

// GetAPIKeyOrg returns the org name that owns the given raw key value.
// Returns ("", nil) when the key does not exist.
func (db *PostgresDB) GetAPIKeyOrg(ctx context.Context, key string) (string, error) {
	var orgName string
	keyHash := sqldb.HashAPIKey(key)
	err := db.pool.QueryRow(ctx, `
		SELECT o.name FROM org_api_keys k
		JOIN organisations o ON o.id = k.org_id
		WHERE k.key_hash = $1`, keyHash).Scan(&orgName)
	if err == pgx.ErrNoRows {
		return db.getLegacyAPIKeyOrg(ctx, key, keyHash)
	}
	if err != nil {
		return "", fmt.Errorf("looking up api key: %w", err)
	}
	return orgName, nil
}

func (db *PostgresDB) getLegacyAPIKeyOrg(ctx context.Context, key, keyHash string) (string, error) {
	var id int
	var orgName string
	err := db.pool.QueryRow(ctx, `
		SELECT k.id, o.name FROM org_api_keys k
		JOIN organisations o ON o.id = k.org_id
		WHERE k.key = $1`, key).Scan(&id, &orgName)
	if err == pgx.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("looking up legacy api key: %w", err)
	}
	_, _ = db.pool.Exec(ctx, `
		UPDATE org_api_keys
		SET key = $2, key_hash = $3, key_prefix = $4, key_last4 = $5
		WHERE id = $1
	`, id, sqldb.APIKeyPlaceholder(id, keyHash), keyHash, sqldb.APIKeyPrefix(key), sqldb.APIKeyLast4(key))
	return orgName, nil
}

func (db *PostgresDB) migrateAPIKeyHashes(ctx context.Context) error {
	rows, err := db.pool.Query(ctx, `
		SELECT id, key FROM org_api_keys
		WHERE key_hash IS NULL OR key_hash = ''
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type legacyKey struct {
		id  int
		key string
	}
	var legacy []legacyKey
	for rows.Next() {
		var k legacyKey
		if err := rows.Scan(&k.id, &k.key); err != nil {
			return err
		}
		legacy = append(legacy, k)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, k := range legacy {
		keyHash := sqldb.HashAPIKey(k.key)
		if _, err := db.pool.Exec(ctx, `
			UPDATE org_api_keys
			SET key = $2, key_hash = $3, key_prefix = $4, key_last4 = $5
			WHERE id = $1
		`, k.id, sqldb.APIKeyPlaceholder(k.id, keyHash), keyHash, sqldb.APIKeyPrefix(k.key), sqldb.APIKeyLast4(k.key)); err != nil {
			return err
		}
	}
	return nil
}
