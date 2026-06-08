package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/xact-iot/xact/sqldb"
)

// ListNotificationProfiles returns all notification profiles for an organisation.
func (db *SQLiteDB) ListNotificationProfiles(ctx context.Context, org string) ([]sqldb.NotificationProfile, error) {
	rows, err := db.db.QueryContext(ctx, `
		SELECT id, org_name, name, description, roles, users, ack_required, created_at, updated_at
		FROM notification_profiles
		WHERE org_name = ?
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
func (db *SQLiteDB) GetNotificationProfile(ctx context.Context, org string, id int) (*sqldb.NotificationProfile, error) {
	rows, err := db.db.QueryContext(ctx, `
		SELECT id, org_name, name, description, roles, users, ack_required, created_at, updated_at
		FROM notification_profiles
		WHERE org_name = ? AND id = ?
	`, org, id)
	if err != nil {
		return nil, fmt.Errorf("getting notification profile: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, rows.Err()
	}
	p, err := scanProfile(rows)
	if err != nil {
		return nil, fmt.Errorf("scanning notification profile: %w", err)
	}
	return &p, nil
}

// GetNotificationProfileByName returns a profile by name within an organisation.
func (db *SQLiteDB) GetNotificationProfileByName(ctx context.Context, org string, name string) (*sqldb.NotificationProfile, error) {
	rows, err := db.db.QueryContext(ctx, `
		SELECT id, org_name, name, description, roles, users, ack_required, created_at, updated_at
		FROM notification_profiles
		WHERE org_name = ? AND name = ?
	`, org, name)
	if err != nil {
		return nil, fmt.Errorf("getting notification profile by name: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, rows.Err()
	}
	p, err := scanProfile(rows)
	if err != nil {
		return nil, fmt.Errorf("scanning notification profile: %w", err)
	}
	return &p, nil
}

// ResolveNotificationID returns the ID for a notification profile by its canonical name
// within an organisation. Returns 0 if not found.
func (db *SQLiteDB) ResolveNotificationID(ctx context.Context, org, name string) (int, error) {
	row := db.db.QueryRowContext(ctx, `
		SELECT id FROM notification_profiles WHERE org_name = ? AND name = ?
	`, org, name)
	var id int
	if err := row.Scan(&id); err == sql.ErrNoRows {
		fmt.Printf("sqlite_notifications:90 No rows %v %v\n", org, name)
		return 0, nil
	} else if err != nil {
		fmt.Printf("sqlite_notifications:92 Error %v %v, %v\n", org, name, err)
		return 0, fmt.Errorf("resolve notification profile ID: %w", err)
	}
	fmt.Printf("sqlite_notifications:94 ResolveNotificationId %v, %v, %d\n", org, name, id)

	return id, nil
}

// CreateNotificationProfile inserts a new notification profile.
func (db *SQLiteDB) CreateNotificationProfile(ctx context.Context, org string, p *sqldb.NotificationProfile) error {
	rolesJSON, _ := json.Marshal(p.Roles)
	usersJSON, _ := json.Marshal(p.Users)
	if p.Roles == nil {
		rolesJSON = []byte("[]")
	}
	if p.Users == nil {
		usersJSON = []byte("[]")
	}
	ackRequired := 0
	if p.AckRequired {
		ackRequired = 1
	}
	now := formatTimestamp(time.Now())
	result, err := db.db.ExecContext(ctx, `
		INSERT INTO notification_profiles (org_name, name, description, roles, users, ack_required, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, org, p.Name, p.Description, string(rolesJSON), string(usersJSON), ackRequired, now, now)
	if err != nil {
		return fmt.Errorf("creating notification profile: %w", err)
	}
	id, _ := result.LastInsertId()
	p.ID = int(id)
	p.OrgName = org
	p.CreatedAt = parseTimestamp(now)
	p.UpdatedAt = parseTimestamp(now)
	return nil
}

// UpdateNotificationProfile updates an existing notification profile.
func (db *SQLiteDB) UpdateNotificationProfile(ctx context.Context, org string, id int, p *sqldb.NotificationProfile) error {
	rolesJSON, _ := json.Marshal(p.Roles)
	usersJSON, _ := json.Marshal(p.Users)
	if p.Roles == nil {
		rolesJSON = []byte("[]")
	}
	if p.Users == nil {
		usersJSON = []byte("[]")
	}
	ackRequired := 0
	if p.AckRequired {
		ackRequired = 1
	}
	now := formatTimestamp(time.Now())
	result, err := db.db.ExecContext(ctx, `
		UPDATE notification_profiles
		SET name = ?, description = ?, roles = ?, users = ?, ack_required = ?, updated_at = ?
		WHERE org_name = ? AND id = ?
	`, p.Name, p.Description, string(rolesJSON), string(usersJSON), ackRequired, now, org, id)
	if err != nil {
		return fmt.Errorf("updating notification profile: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("notification profile not found")
	}
	return nil
}

// DeleteNotificationProfile removes a notification profile by ID.
func (db *SQLiteDB) DeleteNotificationProfile(ctx context.Context, org string, id int) error {
	result, err := db.db.ExecContext(ctx,
		"DELETE FROM notification_profiles WHERE org_name = ? AND id = ?", org, id)
	if err != nil {
		return fmt.Errorf("deleting notification profile: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("notification profile not found")
	}
	return nil
}

// GetNotificationRecipients returns the deduplicated list of users matching
// a profile's roles and explicit user list.
func (db *SQLiteDB) GetNotificationRecipients(ctx context.Context, org string, profileID int) ([]sqldb.NotificationRecipient, error) {
	profile, err := db.GetNotificationProfile(ctx, org, profileID)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, fmt.Errorf("notification profile %d not found", profileID)
	}

	var whereParts []string
	var args []any

	if len(profile.Roles) > 0 {
		sub := `u.id IN (
			SELECT uor.user_id
			FROM user_organisation_roles uor
			JOIN organisations o ON o.id = uor.org_id
			JOIN roles r ON r.id = uor.role_id
			WHERE o.name = ? AND r.name IN ` + inClause(len(profile.Roles)) + `
		)`
		whereParts = append(whereParts, sub)
		args = append(args, org)
		for _, r := range profile.Roles {
			args = append(args, r)
		}
	}

	if len(profile.Users) > 0 {
		whereParts = append(whereParts, "u.id IN "+inClause(len(profile.Users)))
		for _, uid := range profile.Users {
			args = append(args, uid)
		}
	}

	if len(whereParts) == 0 {
		return nil, nil
	}

	query := `SELECT DISTINCT u.id, u.first_name, u.last_name, u.email, u.notification_options
		FROM users u
		WHERE u.active = 1 AND (` + strings.Join(whereParts, " OR ") + `)
		ORDER BY u.id`

	rows, err := db.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("getting notification recipients: %w", err)
	}
	defer rows.Close()

	var recipients []sqldb.NotificationRecipient
	for rows.Next() {
		var r sqldb.NotificationRecipient
		var notifOpts sql.NullString
		if err := rows.Scan(&r.ID, &r.FirstName, &r.LastName, &r.Email, &notifOpts); err != nil {
			return nil, fmt.Errorf("scanning notification recipient: %w", err)
		}
		if notifOpts.Valid {
			r.NotificationOptions = []byte(notifOpts.String)
		}
		recipients = append(recipients, r)
	}
	return recipients, rows.Err()
}

// scanProfile scans a notification profile from rows.
func scanProfile(rows *sql.Rows) (sqldb.NotificationProfile, error) {
	var p sqldb.NotificationProfile
	var rolesStr, usersStr, createdAtStr, updatedAtStr string
	var ackRequiredInt int
	if err := rows.Scan(&p.ID, &p.OrgName, &p.Name, &p.Description,
		&rolesStr, &usersStr, &ackRequiredInt, &createdAtStr, &updatedAtStr); err != nil {
		return p, fmt.Errorf("scanning notification profile: %w", err)
	}
	p.AckRequired = ackRequiredInt != 0
	p.CreatedAt = parseTimestamp(createdAtStr)
	p.UpdatedAt = parseTimestamp(updatedAtStr)
	_ = json.Unmarshal([]byte(rolesStr), &p.Roles)
	_ = json.Unmarshal([]byte(usersStr), &p.Users)
	if p.Roles == nil {
		p.Roles = []string{}
	}
	if p.Users == nil {
		p.Users = []int{}
	}
	return p, nil
}
