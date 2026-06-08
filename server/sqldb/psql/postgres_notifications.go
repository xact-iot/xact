package psql

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/xact-iot/xact/sqldb"
)

// ListNotificationProfiles returns all notification profiles for an organisation.
func (db *PostgresDB) ListNotificationProfiles(ctx context.Context, org string) ([]sqldb.NotificationProfile, error) {
	rows, err := db.pool.Query(ctx, `
		SELECT id, org_name, name, description, roles, users, ack_required, created_at, updated_at
		FROM notification_profiles
		WHERE org_name = $1
		ORDER BY name
	`, org)
	if err != nil {
		return nil, fmt.Errorf("listing notification profiles: %w", err)
	}
	defer rows.Close()

	var profiles []sqldb.NotificationProfile
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, p)
	}
	return profiles, rows.Err()
}

// GetNotificationProfile returns a single notification profile by ID.
func (db *PostgresDB) GetNotificationProfile(ctx context.Context, org string, id int) (*sqldb.NotificationProfile, error) {
	row := db.pool.QueryRow(ctx, `
		SELECT id, org_name, name, description, roles, users, ack_required, created_at, updated_at
		FROM notification_profiles
		WHERE org_name = $1 AND id = $2
	`, org, id)

	p, err := scanProfileRow(row)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting notification profile: %w", err)
	}
	return &p, nil
}

// GetNotificationProfileByName returns a profile by name within an organisation.
func (db *PostgresDB) GetNotificationProfileByName(ctx context.Context, org string, name string) (*sqldb.NotificationProfile, error) {
	row := db.pool.QueryRow(ctx, `
		SELECT id, org_name, name, description, roles, users, ack_required, created_at, updated_at
		FROM notification_profiles
		WHERE org_name = $1 AND name = $2
	`, org, name)

	p, err := scanProfileRow(row)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting notification profile by name: %w", err)
	}
	return &p, nil
}

// ResolveNotificationID returns the ID for a notification profile by its canonical name
// within an organisation. Returns 0 if not found.
func (db *PostgresDB) ResolveNotificationID(ctx context.Context, org, name string) (int, error) {
	row := db.pool.QueryRow(ctx, `
		SELECT id FROM notification_profiles WHERE org_name = $1 AND name = $2
	`, org, name)
	var id int
	if err := row.Scan(&id); err == pgx.ErrNoRows {
		return 0, nil
	} else if err != nil {
		return 0, fmt.Errorf("resolve notification profile ID: %w", err)
	}
	return id, nil
}

// CreateNotificationProfile inserts a new notification profile.
func (db *PostgresDB) CreateNotificationProfile(ctx context.Context, org string, p *sqldb.NotificationProfile) error {
	rolesJSON, _ := json.Marshal(p.Roles)
	usersJSON, _ := json.Marshal(p.Users)
	if p.Roles == nil {
		rolesJSON = []byte("[]")
	}
	if p.Users == nil {
		usersJSON = []byte("[]")
	}

	err := db.pool.QueryRow(ctx, `
		INSERT INTO notification_profiles (org_name, name, description, roles, users, ack_required)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, created_at, updated_at
	`, org, p.Name, p.Description, rolesJSON, usersJSON, p.AckRequired).Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return fmt.Errorf("creating notification profile: %w", err)
	}
	p.OrgName = org
	return nil
}

// UpdateNotificationProfile updates an existing notification profile.
func (db *PostgresDB) UpdateNotificationProfile(ctx context.Context, org string, id int, p *sqldb.NotificationProfile) error {
	rolesJSON, _ := json.Marshal(p.Roles)
	usersJSON, _ := json.Marshal(p.Users)
	if p.Roles == nil {
		rolesJSON = []byte("[]")
	}
	if p.Users == nil {
		usersJSON = []byte("[]")
	}

	tag, err := db.pool.Exec(ctx, `
		UPDATE notification_profiles
		SET name = $3, description = $4, roles = $5, users = $6, ack_required = $7, updated_at = NOW()
		WHERE org_name = $1 AND id = $2
	`, org, id, p.Name, p.Description, rolesJSON, usersJSON, p.AckRequired)
	if err != nil {
		return fmt.Errorf("updating notification profile: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("notification profile not found")
	}
	return nil
}

// DeleteNotificationProfile removes a notification profile by ID.
func (db *PostgresDB) DeleteNotificationProfile(ctx context.Context, org string, id int) error {
	tag, err := db.pool.Exec(ctx, `
		DELETE FROM notification_profiles WHERE org_name = $1 AND id = $2
	`, org, id)
	if err != nil {
		return fmt.Errorf("deleting notification profile: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("notification profile not found")
	}
	return nil
}

// GetNotificationRecipients returns the deduplicated list of users matching
// a profile's roles and explicit user list.
func (db *PostgresDB) GetNotificationRecipients(ctx context.Context, org string, profileID int) ([]sqldb.NotificationRecipient, error) {
	// Get the profile first.
	profile, err := db.GetNotificationProfile(ctx, org, profileID)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, fmt.Errorf("notification profile %d not found", profileID)
	}

	// Build the deduplicated user set: users with matching roles + explicit user IDs.
	rows, err := db.pool.Query(ctx, `
		SELECT DISTINCT u.id, u.first_name, u.last_name, u.email, u.notification_options
		FROM users u
		WHERE u.active = true AND (
			u.id IN (
				SELECT uor.user_id
				FROM user_organisation_roles uor
				JOIN organisations o ON o.id = uor.org_id
				JOIN roles r ON r.id = uor.role_id
				WHERE o.name = $1 AND r.name = ANY($2)
			)
			OR u.id = ANY($3)
		)
		ORDER BY u.id
	`, org, profile.Roles, profile.Users)
	if err != nil {
		return nil, fmt.Errorf("getting notification recipients: %w", err)
	}
	defer rows.Close()

	var recipients []sqldb.NotificationRecipient
	for rows.Next() {
		var r sqldb.NotificationRecipient
		if err := rows.Scan(&r.ID, &r.FirstName, &r.LastName, &r.Email, &r.NotificationOptions); err != nil {
			return nil, fmt.Errorf("scanning notification recipient: %w", err)
		}
		recipients = append(recipients, r)
	}
	return recipients, rows.Err()
}

// scanProfile scans a notification profile from a row set.
func scanProfile(rows pgx.Rows) (sqldb.NotificationProfile, error) {
	var p sqldb.NotificationProfile
	var rolesJSON, usersJSON json.RawMessage
	if err := rows.Scan(&p.ID, &p.OrgName, &p.Name, &p.Description, &rolesJSON, &usersJSON, &p.AckRequired, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return p, fmt.Errorf("scanning notification profile: %w", err)
	}
	_ = json.Unmarshal(rolesJSON, &p.Roles)
	_ = json.Unmarshal(usersJSON, &p.Users)
	if p.Roles == nil {
		p.Roles = []string{}
	}
	if p.Users == nil {
		p.Users = []int{}
	}
	return p, nil
}

// scanProfileRow scans a single notification profile row.
func scanProfileRow(row pgx.Row) (sqldb.NotificationProfile, error) {
	var p sqldb.NotificationProfile
	var rolesJSON, usersJSON json.RawMessage
	if err := row.Scan(&p.ID, &p.OrgName, &p.Name, &p.Description, &rolesJSON, &usersJSON, &p.AckRequired, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return p, err
	}
	_ = json.Unmarshal(rolesJSON, &p.Roles)
	_ = json.Unmarshal(usersJSON, &p.Users)
	if p.Roles == nil {
		p.Roles = []string{}
	}
	if p.Users == nil {
		p.Users = []int{}
	}
	return p, nil
}
