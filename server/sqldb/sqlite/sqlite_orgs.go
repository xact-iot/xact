package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/xact-iot/xact/sqldb"
)

// ListOrganisations returns all organisations ordered by name.
func (db *SQLiteDB) ListOrganisations(ctx context.Context) ([]sqldb.Organisation, error) {
	rows, err := db.db.QueryContext(ctx,
		"SELECT id, name, display_name, active, logo, favicon, area FROM organisations ORDER BY name")
	if err != nil {
		return nil, fmt.Errorf("listing organisations: %w", err)
	}
	defer rows.Close()

	var orgs []sqldb.Organisation
	for rows.Next() {
		o, err := scanOrganisation(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning organisation: %w", err)
		}
		orgs = append(orgs, o)
	}
	return orgs, rows.Err()
}

// GetOrganisation returns a single organisation by name, or nil if not found.
func (db *SQLiteDB) GetOrganisation(ctx context.Context, name string) (*sqldb.Organisation, error) {
	o, err := scanOrganisation(db.db.QueryRowContext(ctx,
		"SELECT id, name, display_name, active, logo, favicon, area FROM organisations WHERE name = ?", name,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting organisation %q: %w", name, err)
	}
	return &o, nil
}

type sqlScanner interface {
	Scan(dest ...any) error
}

func scanOrganisation(scanner sqlScanner) (sqldb.Organisation, error) {
	var o sqldb.Organisation
	var active sqliteBool
	var displayName, logo, favicon, areaJSON sql.NullString
	if err := scanner.Scan(&o.ID, &o.Name, &displayName, &active, &logo, &favicon, &areaJSON); err != nil {
		return o, err
	}
	o.DisplayName = displayName.String
	o.Active = active.Bool
	o.Logo = logo.String
	o.Favicon = favicon.String
	if areaJSON.Valid {
		o.Area = parseArea(areaJSON.String)
	}
	return o, nil
}

// CreateOrganisation inserts a new organisation, setting org.ID on success.
func (db *SQLiteDB) CreateOrganisation(ctx context.Context, org *sqldb.Organisation) error {
	active := 0
	if org.Active {
		active = 1
	}
	var areaParam any
	if org.Area != nil {
		areaParam = formatArea(org.Area)
	}
	result, err := db.db.ExecContext(ctx,
		"INSERT INTO organisations (name, display_name, active, logo, favicon, area) VALUES (?, ?, ?, ?, ?, ?)",
		org.Name, org.DisplayName, active, org.Logo, org.Favicon, areaParam)
	if err != nil {
		return err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	org.ID = int(id)
	if err := db.seedNotificationProfiles(ctx, org.Name); err != nil {
		return err
	}
	if err := db.seedOrgPermissions(ctx, org.ID); err != nil {
		return err
	}
	return db.seedStarterDashboards(ctx, org.ID)
}

// UpdateOrganisation updates the organisation identified by name.
func (db *SQLiteDB) UpdateOrganisation(ctx context.Context, name string, org *sqldb.Organisation) error {
	active := 0
	if org.Active {
		active = 1
	}
	var areaParam any
	if org.Area != nil {
		areaParam = formatArea(org.Area)
	}
	result, err := db.db.ExecContext(ctx,
		"UPDATE organisations SET display_name = ?, active = ?, logo = ?, favicon = ?, area = ? WHERE name = ?",
		org.DisplayName, active, org.Logo, org.Favicon, areaParam, name)
	if err != nil {
		return fmt.Errorf("updating organisation %q: %w", name, err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("organisation %q not found", name)
	}
	return nil
}

// DeleteOrganisation removes an organisation. The "default" org cannot be deleted.
func (db *SQLiteDB) DeleteOrganisation(ctx context.Context, name string) error {
	if name == "default" {
		return fmt.Errorf("cannot delete the default organisation")
	}
	result, err := db.db.ExecContext(ctx, "DELETE FROM organisations WHERE name = ?", name)
	if err != nil {
		return fmt.Errorf("deleting organisation %q: %w", name, err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("organisation %q not found", name)
	}
	return nil
}

// ---- API keys ----

// ListAPIKeys returns all API keys for an organisation.
func (db *SQLiteDB) ListAPIKeys(ctx context.Context, orgName string) ([]sqldb.APIKey, error) {
	rows, err := db.db.QueryContext(ctx, `
		SELECT k.id, o.name, k.name, k.key_prefix, k.key_last4, k.created_at
		FROM org_api_keys k
		JOIN organisations o ON o.id = k.org_id
		WHERE o.name = ?
		ORDER BY k.created_at
	`, orgName)
	if err != nil {
		return nil, fmt.Errorf("listing api keys for %q: %w", orgName, err)
	}
	defer rows.Close()

	var keys []sqldb.APIKey
	for rows.Next() {
		var k sqldb.APIKey
		var createdAtStr string
		if err := rows.Scan(&k.ID, &k.OrgName, &k.Name, &k.KeyPrefix, &k.KeyLast4, &createdAtStr); err != nil {
			return nil, err
		}
		k.Key = sqldb.MaskAPIKey(k.KeyPrefix, k.KeyLast4)
		k.CreatedAt = parseTimestamp(createdAtStr)
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// CreateAPIKey generates a new random API key and stores it for the given org.
func (db *SQLiteDB) CreateAPIKey(ctx context.Context, orgName, name string) (*sqldb.APIKey, error) {
	var count int
	err := db.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM org_api_keys k
		JOIN organisations o ON o.id = k.org_id
		WHERE o.name = ?
	`, orgName).Scan(&count)
	if err != nil {
		return nil, fmt.Errorf("counting api keys: %w", err)
	}
	if count >= 5 {
		return nil, fmt.Errorf("organisation %q already has 5 API keys (maximum)", orgName)
	}

	var orgID int
	if err := db.db.QueryRowContext(ctx,
		"SELECT id FROM organisations WHERE name = ?", orgName,
	).Scan(&orgID); err != nil {
		return nil, fmt.Errorf("organisation %q not found: %w", orgName, err)
	}

	keyValue, err := sqldb.NewRawAPIKey()
	if err != nil {
		return nil, fmt.Errorf("generating api key: %w", err)
	}
	keyHash := sqldb.HashAPIKey(keyValue)
	keyPrefix := sqldb.APIKeyPrefix(keyValue)
	keyLast4 := sqldb.APIKeyLast4(keyValue)
	now := formatTimestamp(time.Now())

	result, err := db.db.ExecContext(ctx,
		`INSERT INTO org_api_keys (org_id, name, key, key_hash, key_prefix, key_last4, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		orgID, name, sqldb.APIKeyPlaceholder(0, keyHash), keyHash, keyPrefix, keyLast4, now)
	if err != nil {
		return nil, fmt.Errorf("creating api key: %w", err)
	}
	id, _ := result.LastInsertId()
	_, _ = db.db.ExecContext(ctx,
		"UPDATE org_api_keys SET key = ? WHERE id = ?",
		sqldb.APIKeyPlaceholder(int(id), keyHash), id)

	return &sqldb.APIKey{
		ID:        int(id),
		OrgName:   orgName,
		Name:      name,
		Key:       keyValue,
		KeyPrefix: keyPrefix,
		KeyLast4:  keyLast4,
		CreatedAt: parseTimestamp(now),
	}, nil
}

// DeleteAPIKey removes an API key by ID within an organisation.
func (db *SQLiteDB) DeleteAPIKey(ctx context.Context, orgName string, id int) error {
	result, err := db.db.ExecContext(ctx, `
		DELETE FROM org_api_keys
		WHERE id = ? AND org_id = (SELECT id FROM organisations WHERE name = ?)
	`, id, orgName)
	if err != nil {
		return fmt.Errorf("deleting api key %d: %w", id, err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("api key %d not found in organisation %q", id, orgName)
	}
	return nil
}

// GetAPIKeyOrg returns the org name that owns the given raw key value.
// Returns ("", nil) when the key does not exist.
func (db *SQLiteDB) GetAPIKeyOrg(ctx context.Context, key string) (string, error) {
	var orgName string
	keyHash := sqldb.HashAPIKey(key)
	err := db.db.QueryRowContext(ctx, `
		SELECT o.name FROM org_api_keys k
		JOIN organisations o ON o.id = k.org_id
		WHERE k.key_hash = ?
	`, keyHash).Scan(&orgName)
	if errors.Is(err, sql.ErrNoRows) {
		return db.getLegacyAPIKeyOrg(ctx, key, keyHash)
	}
	if err != nil {
		return "", fmt.Errorf("looking up api key: %w", err)
	}
	return orgName, nil
}

func (db *SQLiteDB) getLegacyAPIKeyOrg(ctx context.Context, key, keyHash string) (string, error) {
	var id int
	var orgName string
	err := db.db.QueryRowContext(ctx, `
		SELECT k.id, o.name FROM org_api_keys k
		JOIN organisations o ON o.id = k.org_id
		WHERE k.key = ?
	`, key).Scan(&id, &orgName)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("looking up legacy api key: %w", err)
	}
	_, _ = db.db.ExecContext(ctx, `
		UPDATE org_api_keys
		SET key = ?, key_hash = ?, key_prefix = ?, key_last4 = ?
		WHERE id = ?
	`, sqldb.APIKeyPlaceholder(id, keyHash), keyHash, sqldb.APIKeyPrefix(key), sqldb.APIKeyLast4(key), id)
	return orgName, nil
}

func (db *SQLiteDB) migrateAPIKeyHashes(ctx context.Context) error {
	rows, err := db.db.QueryContext(ctx, `
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
		if _, err := db.db.ExecContext(ctx, `
			UPDATE org_api_keys
			SET key = ?, key_hash = ?, key_prefix = ?, key_last4 = ?
			WHERE id = ?
		`, sqldb.APIKeyPlaceholder(k.id, keyHash), keyHash, sqldb.APIKeyPrefix(k.key), sqldb.APIKeyLast4(k.key), k.id); err != nil {
			return err
		}
	}
	return nil
}

// formatArea serialises an OrgArea to a JSON string for storage.
func formatArea(a *sqldb.OrgArea) string {
	b, _ := json.Marshal(a)
	return string(b)
}

// parseArea deserialises an OrgArea from a JSON string.
func parseArea(s string) *sqldb.OrgArea {
	var a sqldb.OrgArea
	if err := json.Unmarshal([]byte(s), &a); err != nil {
		return nil
	}
	return &a
}
