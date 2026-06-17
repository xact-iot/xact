package psql

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

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
			('SystemAdmin', '{"dashboards-setup":{"read":true,"edit":true},"dashboard-container":{"edit":true},"widget-default":{"view":true,"configure":true},"organisations":{"view":true,"change":true},"permissions":{"manage":true},"users":{"manage":true},"nodes":{"read":true,"write":true},"tags":{"read":true,"write":true},"logs":{"read":true,"write":true},"profile":{"change":true}}'::jsonb),
			('Admin',       '{"dashboards-setup":{"read":true,"edit":true},"dashboard-container":{"edit":true},"widget-default":{"view":true,"configure":true},"organisations":{"view":false,"change":false},"permissions":{"manage":true},"users":{"manage":true},"nodes":{"read":true,"write":true},"tags":{"read":true,"write":true},"logs":{"read":true,"write":true},"profile":{"change":true}}'::jsonb),
			('Manager',     '{"dashboards-setup":{"read":true,"edit":true},"dashboard-container":{"edit":true},"widget-default":{"view":true,"configure":true},"organisations":{"view":false,"change":false},"permissions":{"manage":false},"users":{"manage":false},"nodes":{"read":true,"write":true},"tags":{"read":true,"write":true},"logs":{"read":true,"write":false},"profile":{"change":true}}'::jsonb),
			('Technician',  '{"dashboards-setup":{"read":true,"edit":false},"dashboard-container":{"edit":true},"widget-default":{"view":true,"configure":true},"organisations":{"view":false,"change":false},"permissions":{"manage":false},"users":{"manage":false},"nodes":{"read":true,"write":true},"tags":{"read":true,"write":true},"logs":{"read":true,"write":false},"profile":{"change":true}}'::jsonb),
			('Operator',    '{"dashboards-setup":{"read":true,"edit":false},"dashboard-container":{"edit":false},"widget-default":{"view":true,"configure":false},"organisations":{"view":false,"change":false},"permissions":{"manage":false},"users":{"manage":false},"nodes":{"read":true,"write":false},"tags":{"read":true,"write":false},"logs":{"read":false,"write":false},"profile":{"change":true}}'::jsonb),
			('User',        '{"dashboards-setup":{"read":true,"edit":false},"dashboard-container":{"edit":false},"widget-default":{"view":true,"configure":false},"organisations":{"view":false,"change":false},"permissions":{"manage":false},"users":{"manage":false},"nodes":{"read":true,"write":false},"tags":{"read":true,"write":false},"logs":{"read":false,"write":false},"profile":{"change":false}}'::jsonb)
		) AS r(role, ui)
		ON CONFLICT (org_id, role) DO NOTHING
	`, orgID)
	if err != nil {
		return err
	}
	_, err = db.pool.Exec(ctx, `
		UPDATE permissions
		SET ui = jsonb_set(
			jsonb_set(
				jsonb_set(
					CASE WHEN ui ? 'agentkeys' THEN ui ELSE ui || '{"agentkeys":{}}'::jsonb END,
					'{agentkeys,manage}',
					to_jsonb(role IN ('SystemAdmin','Admin')),
					true
				),
				'{agentkeys,personal}',
				to_jsonb(role IN ('SystemAdmin','Admin','Manager','Technician','Operator')),
				true
			),
			'{agentkeys,access}',
			to_jsonb(role IN ('SystemAdmin','Admin','Manager','Technician','Operator')),
			true
		)
		WHERE org_id = $1
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

// ListAgentTokens returns agent bearer tokens for an organisation.
func (db *PostgresDB) ListAgentTokens(ctx context.Context, orgName string, userID int, includeAll bool) ([]sqldb.AgentToken, error) {
	query := `
		SELECT k.id, o.name, k.user_id, COALESCE(u.login_name, ''),
		       COALESCE(NULLIF(TRIM(u.first_name || ' ' || u.last_name), ''), u.login_name, ''),
		       k.name, k.token_prefix, k.token_last4, k.roles, k.created_at, k.expires_at, k.last_used_at
		FROM org_agent_tokens k
		JOIN organisations o ON o.id = k.org_id
		LEFT JOIN users u ON u.id = k.user_id
		WHERE o.name = $1`
	args := []any{orgName}
	if !includeAll {
		query += ` AND k.user_id = $2`
		args = append(args, userID)
	}
	query += ` ORDER BY k.created_at`
	rows, err := db.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing agent tokens for %q: %w", orgName, err)
	}
	defer rows.Close()

	var tokens []sqldb.AgentToken
	for rows.Next() {
		tok, err := scanAgentToken(rows)
		if err != nil {
			return nil, err
		}
		tok.Token = sqldb.MaskAPIKey(tok.TokenPrefix, tok.TokenLast4)
		tokens = append(tokens, tok)
	}
	return tokens, rows.Err()
}

// CreateAgentToken generates and stores a new bearer token for an organisation.
func (db *PostgresDB) CreateAgentToken(ctx context.Context, orgName string, userID int, name string, roles []string, expiresAt *time.Time) (*sqldb.AgentToken, error) {
	var count int
	err := db.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM org_agent_tokens k
		JOIN organisations o ON o.id = k.org_id
		WHERE o.name = $1 AND k.user_id = $2`, orgName, userID).Scan(&count)
	if err != nil {
		return nil, fmt.Errorf("counting agent tokens: %w", err)
	}
	if count >= 10 {
		return nil, fmt.Errorf("user already has 10 agent tokens in organisation %q (maximum)", orgName)
	}

	tokenValue, err := sqldb.NewRawAgentToken()
	if err != nil {
		return nil, fmt.Errorf("generating agent token: %w", err)
	}
	tokenHash := sqldb.HashAgentToken(tokenValue)
	tokenSecret, err := sqldb.EncryptAgentToken(tokenValue)
	if err != nil {
		return nil, fmt.Errorf("encrypting agent token: %w", err)
	}
	tokenPrefix := sqldb.APIKeyPrefix(tokenValue)
	tokenLast4 := sqldb.APIKeyLast4(tokenValue)
	roles = normalizeAgentRoles(roles)
	rolesJSON, _ := json.Marshal(roles)

	var tok sqldb.AgentToken
	err = db.pool.QueryRow(ctx, `
		INSERT INTO org_agent_tokens (org_id, user_id, name, token_secret, token_hash, token_prefix, token_last4, roles, expires_at)
		SELECT o.id, u.id, $3, $4, $5, $6, $7, $8::jsonb, $9
		FROM organisations o
		JOIN user_organisations uo ON uo.org_id = o.id
		JOIN users u ON u.id = uo.user_id
		WHERE o.name = $1 AND u.id = $2 AND u.active = TRUE
		RETURNING id, $1, $2, COALESCE((SELECT login_name FROM users WHERE id = $2), ''),
		          COALESCE((SELECT NULLIF(TRIM(first_name || ' ' || last_name), '') FROM users WHERE id = $2), (SELECT login_name FROM users WHERE id = $2), ''),
		          $3, created_at, expires_at`,
		orgName, userID, name, tokenSecret, tokenHash, tokenPrefix, tokenLast4, string(rolesJSON), expiresAt,
	).Scan(&tok.ID, &tok.OrgName, &tok.UserID, &tok.UserLoginName, &tok.UserDisplayName, &tok.Name, &tok.CreatedAt, &tok.ExpiresAt)
	if err != nil {
		return nil, fmt.Errorf("creating agent token: %w", err)
	}
	tok.Token = tokenValue
	tok.TokenPrefix = tokenPrefix
	tok.TokenLast4 = tokenLast4
	tok.Roles = roles
	return &tok, nil
}

// GetAgentToken returns a decryptable agent token when the caller is allowed to see it.
func (db *PostgresDB) GetAgentToken(ctx context.Context, orgName string, id int, userID int, includeAll bool) (*sqldb.AgentToken, error) {
	query := `
		SELECT k.id, o.name, k.user_id, COALESCE(u.login_name, ''),
		       COALESCE(NULLIF(TRIM(u.first_name || ' ' || u.last_name), ''), u.login_name, ''),
		       k.name, k.token_prefix, k.token_last4, k.roles, k.created_at, k.expires_at, k.last_used_at, k.token_secret
		FROM org_agent_tokens k
		JOIN organisations o ON o.id = k.org_id
		LEFT JOIN users u ON u.id = k.user_id
		WHERE o.name = $1 AND k.id = $2`
	args := []any{orgName, id}
	if !includeAll {
		query += ` AND k.user_id = $3`
		args = append(args, userID)
	}
	var secret string
	tok, err := scanAgentTokenWithSecret(db.pool.QueryRow(ctx, query, args...), &secret)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting agent token: %w", err)
	}
	raw, err := sqldb.DecryptAgentToken(secret)
	if err != nil {
		return nil, err
	}
	tok.Token = raw
	return &tok, nil
}

// DeleteAgentToken removes an agent token by ID within an organisation.
func (db *PostgresDB) DeleteAgentToken(ctx context.Context, orgName string, id int, userID int, includeAll bool) error {
	query := `
		DELETE FROM org_agent_tokens k
		USING organisations o
		WHERE k.org_id = o.id AND o.name = $1 AND k.id = $2`
	args := []any{orgName, id}
	if !includeAll {
		query += ` AND k.user_id = $3`
		args = append(args, userID)
	}
	tag, err := db.pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("deleting agent token %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("agent token %d not found in organisation %q", id, orgName)
	}
	return nil
}

// ResolveAgentToken returns the token record for a raw bearer token.
func (db *PostgresDB) ResolveAgentToken(ctx context.Context, raw string) (*sqldb.AgentToken, error) {
	tokenHash := sqldb.HashAgentToken(raw)
	row := db.pool.QueryRow(ctx, `
		SELECT k.id, o.name, k.user_id, COALESCE(u.login_name, ''),
		       COALESCE(NULLIF(TRIM(u.first_name || ' ' || u.last_name), ''), u.login_name, ''),
		       k.name, k.token_prefix, k.token_last4, k.roles, k.created_at, k.expires_at, k.last_used_at
		FROM org_agent_tokens k
		JOIN organisations o ON o.id = k.org_id
		LEFT JOIN users u ON u.id = k.user_id
		WHERE k.token_hash = $1
		  AND (k.expires_at IS NULL OR k.expires_at > NOW())
	`, tokenHash)
	tok, err := scanAgentToken(row)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("looking up agent token: %w", err)
	}
	return &tok, nil
}

// TouchAgentToken records recent use for display/audit ergonomics.
func (db *PostgresDB) TouchAgentToken(ctx context.Context, id int) error {
	_, err := db.pool.Exec(ctx, "UPDATE org_agent_tokens SET last_used_at = NOW() WHERE id = $1", id)
	return err
}

type agentTokenScanner interface {
	Scan(dest ...any) error
}

func scanAgentToken(row agentTokenScanner) (sqldb.AgentToken, error) {
	var tok sqldb.AgentToken
	var rolesJSON []byte
	if err := row.Scan(&tok.ID, &tok.OrgName, &tok.UserID, &tok.UserLoginName, &tok.UserDisplayName, &tok.Name, &tok.TokenPrefix, &tok.TokenLast4, &rolesJSON, &tok.CreatedAt, &tok.ExpiresAt, &tok.LastUsedAt); err != nil {
		return tok, err
	}
	_ = json.Unmarshal(rolesJSON, &tok.Roles)
	tok.Roles = normalizeAgentRoles(tok.Roles)
	return tok, nil
}

func scanAgentTokenWithSecret(row agentTokenScanner, secret *string) (sqldb.AgentToken, error) {
	var tok sqldb.AgentToken
	var rolesJSON []byte
	if err := row.Scan(&tok.ID, &tok.OrgName, &tok.UserID, &tok.UserLoginName, &tok.UserDisplayName, &tok.Name, &tok.TokenPrefix, &tok.TokenLast4, &rolesJSON, &tok.CreatedAt, &tok.ExpiresAt, &tok.LastUsedAt, secret); err != nil {
		return tok, err
	}
	_ = json.Unmarshal(rolesJSON, &tok.Roles)
	tok.Roles = normalizeAgentRoles(tok.Roles)
	return tok, nil
}

func normalizeAgentRoles(roles []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, role := range roles {
		role = strings.TrimSpace(role)
		if role == "" {
			continue
		}
		key := strings.ToLower(role)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, role)
	}
	return out
}
