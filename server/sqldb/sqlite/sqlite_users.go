package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/xact-iot/xact/sqldb"
)

// HashPassword returns a bcrypt hash of the given plaintext password.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hashing password: %w", err)
	}
	return string(hash), nil
}

// CheckPassword verifies plaintext against a bcrypt hash.
func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// GenerateRandomPassword creates a cryptographically random 12-character password.
func GenerateRandomPassword() (string, error) {
	b := make([]byte, 9)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating random password: %w", err)
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// ListRoles returns all defined roles.
func (db *SQLiteDB) ListRoles(ctx context.Context) ([]sqldb.Role, error) {
	rows, err := db.db.QueryContext(ctx, "SELECT id, name, description FROM roles ORDER BY id")
	if err != nil {
		return nil, fmt.Errorf("listing roles: %w", err)
	}
	defer rows.Close()

	var roles []sqldb.Role
	for rows.Next() {
		var r sqldb.Role
		if err := rows.Scan(&r.ID, &r.Name, &r.Description); err != nil {
			return nil, fmt.Errorf("scanning role row: %w", err)
		}
		roles = append(roles, r)
	}
	return roles, rows.Err()
}

// ListUsers returns all users without password hashes, including their org memberships.
func (db *SQLiteDB) ListUsers(ctx context.Context) ([]sqldb.User, error) {
	rows, err := db.db.QueryContext(ctx, `
		SELECT id, first_name, last_name, login_name, email,
		       notification_options, active, last_login, token_version, created_at
		FROM users
		ORDER BY login_name
	`)
	if err != nil {
		return nil, fmt.Errorf("listing users: %w", err)
	}
	defer rows.Close()

	var users []sqldb.User
	for rows.Next() {
		u, err := scanUserRow(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, *u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range users {
		orgs, err := db.GetUserOrgs(ctx, users[i].ID)
		if err != nil {
			return nil, err
		}
		users[i].Orgs = orgs
	}
	return users, nil
}

// GetUserByID returns a single user by ID with org memberships. Returns nil if not found.
func (db *SQLiteDB) GetUserByID(ctx context.Context, id int) (*sqldb.User, error) {
	rows, err := db.db.QueryContext(ctx, `
		SELECT id, first_name, last_name, login_name, email,
		       notification_options, active, last_login, token_version, created_at
		FROM users WHERE id = ?
	`, id)
	if err != nil {
		return nil, fmt.Errorf("getting user %d: %w", id, err)
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, rows.Err()
	}
	u, err := scanUserRow(rows)
	if err != nil {
		return nil, fmt.Errorf("getting user %d: %w", id, err)
	}
	rows.Close()

	u.Orgs, err = db.GetUserOrgs(ctx, u.ID)
	return u, err
}

// GetUserByLogin finds a user by login_name or email.
// Returns (user, passwordHash, error). Returns nil user if not found.
func (db *SQLiteDB) GetUserByLogin(ctx context.Context, login string) (*sqldb.User, string, error) {
	var u sqldb.User
	var hash, notifOpts, createdAtStr string
	var active sqliteBool
	var lastLoginStr *string
	err := db.db.QueryRowContext(ctx, `
		SELECT id, first_name, last_name, login_name, email,
		       notification_options, active, last_login, token_version, created_at, password_hash
		FROM users
		WHERE (login_name = ? OR email = ?) AND (active = 1 OR lower(CAST(active AS TEXT)) IN ('true', 't', 'yes', 'y', 'on'))
	`, login, login).Scan(
		&u.ID, &u.FirstName, &u.LastName, &u.LoginName, &u.Email,
		&notifOpts, &active, &lastLoginStr, &u.TokenVersion, &createdAtStr, &hash,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", fmt.Errorf("getting user by login %q: %w", login, err)
	}
	u.Active = active.Bool
	u.NotificationOptions = []byte(notifOpts)
	u.CreatedAt = parseTimestamp(createdAtStr)
	if lastLoginStr != nil {
		t := parseTimestamp(*lastLoginStr)
		u.LastLogin = &t
	}

	orgs, err := db.GetUserOrgs(ctx, u.ID)
	if err != nil {
		return nil, "", err
	}
	u.Orgs = orgs
	return &u, hash, nil
}

// CreateUser creates a new user record and sets user.ID and user.CreatedAt on success.
func (db *SQLiteDB) CreateUser(ctx context.Context, user *sqldb.User, passwordHash string) error {
	opts := user.NotificationOptions
	if opts == nil {
		opts = []byte("{}")
	}
	now := formatTimestamp(time.Now())
	active := 0
	if user.Active {
		active = 1
	}
	result, err := db.db.ExecContext(ctx, `
		INSERT INTO users (first_name, last_name, login_name, password_hash, email,
		                   notification_options, active, token_version, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
	`, user.FirstName, user.LastName, user.LoginName, passwordHash, user.Email,
		string(opts), active, now, now)
	if err != nil {
		return err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	user.ID = int(id)
	user.TokenVersion = 1
	user.CreatedAt = parseTimestamp(now)
	return nil
}

// UpdateUser updates mutable user fields (not password, not login_name).
func (db *SQLiteDB) UpdateUser(ctx context.Context, user *sqldb.User) error {
	opts := user.NotificationOptions
	if opts == nil {
		opts = []byte("{}")
	}
	active := 0
	if user.Active {
		active = 1
	}
	now := formatTimestamp(time.Now())
	result, err := db.db.ExecContext(ctx, `
		UPDATE users SET
			first_name = ?, last_name = ?, email = ?,
			notification_options = ?, active = ?, updated_at = ?
		WHERE id = ?
	`, user.FirstName, user.LastName, user.Email, string(opts), active, now, user.ID)
	if err != nil {
		return fmt.Errorf("updating user %d: %w", user.ID, err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("user %d not found", user.ID)
	}
	return nil
}

// SetUserPassword updates a user's password hash.
func (db *SQLiteDB) SetUserPassword(ctx context.Context, id int, passwordHash string) error {
	now := formatTimestamp(time.Now())
	result, err := db.db.ExecContext(ctx,
		"UPDATE users SET password_hash = ?, token_version = token_version + 1, updated_at = ? WHERE id = ?",
		passwordHash, now, id)
	if err != nil {
		return fmt.Errorf("setting password for user %d: %w", id, err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("user %d not found", id)
	}
	return nil
}

// GetUserAuthState returns the active flag and token version for JWT validation.
func (db *SQLiteDB) GetUserAuthState(ctx context.Context, id int) (bool, int, error) {
	var active sqliteBool
	var tokenVersion int
	err := db.db.QueryRowContext(ctx,
		"SELECT active, token_version FROM users WHERE id = ?", id,
	).Scan(&active, &tokenVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return false, 0, nil
	}
	if err != nil {
		return false, 0, fmt.Errorf("getting auth state for user %d: %w", id, err)
	}
	return active.Bool, tokenVersion, nil
}

// BumpUserTokenVersion invalidates existing JWTs for a user.
func (db *SQLiteDB) BumpUserTokenVersion(ctx context.Context, id int) error {
	now := formatTimestamp(time.Now())
	result, err := db.db.ExecContext(ctx,
		"UPDATE users SET token_version = token_version + 1, updated_at = ? WHERE id = ?",
		now, id)
	if err != nil {
		return fmt.Errorf("bumping token version for user %d: %w", id, err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("user %d not found", id)
	}
	return nil
}

// UpdateLastLogin records now as last_login for the user.
func (db *SQLiteDB) UpdateLastLogin(ctx context.Context, id int) error {
	_, err := db.db.ExecContext(ctx,
		"UPDATE users SET last_login = ? WHERE id = ?",
		formatTimestamp(time.Now()), id)
	return err
}

// GetUserOrgs returns all organisations a user belongs to, with their role names.
func (db *SQLiteDB) GetUserOrgs(ctx context.Context, userID int) ([]sqldb.UserOrg, error) {
	rows, err := db.db.QueryContext(ctx, `
		SELECT o.id, o.name
		FROM user_organisations uo
		JOIN organisations o ON o.id = uo.org_id
		WHERE uo.user_id = ?
		ORDER BY o.name
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("getting user orgs: %w", err)
	}
	defer rows.Close()

	var orgs []sqldb.UserOrg
	for rows.Next() {
		var uo sqldb.UserOrg
		if err := rows.Scan(&uo.OrgID, &uo.OrgName); err != nil {
			return nil, err
		}
		orgs = append(orgs, uo)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i, uo := range orgs {
		roleRows, err := db.db.QueryContext(ctx, `
			SELECT r.name
			FROM user_organisation_roles uor
			JOIN roles r ON r.id = uor.role_id
			WHERE uor.user_id = ? AND uor.org_id = ?
			ORDER BY r.name
		`, userID, uo.OrgID)
		if err != nil {
			return nil, fmt.Errorf("getting roles for user %d org %d: %w", userID, uo.OrgID, err)
		}
		var roleNames []string
		for roleRows.Next() {
			var name string
			if err := roleRows.Scan(&name); err != nil {
				roleRows.Close()
				return nil, err
			}
			roleNames = append(roleNames, name)
		}
		roleRows.Close()
		if err := roleRows.Err(); err != nil {
			return nil, err
		}
		orgs[i].Roles = roleNames
	}
	return orgs, nil
}

// AddUserToOrg adds a user to an organisation (membership only, no roles).
func (db *SQLiteDB) AddUserToOrg(ctx context.Context, userID, orgID int) error {
	_, err := db.db.ExecContext(ctx,
		"INSERT OR IGNORE INTO user_organisations (user_id, org_id) VALUES (?, ?)",
		userID, orgID)
	if err != nil {
		return err
	}
	return db.BumpUserTokenVersion(ctx, userID)
}

// AssignRoleToUser assigns a named role to a user within an organisation.
func (db *SQLiteDB) AssignRoleToUser(ctx context.Context, userID, orgID int, roleName string) error {
	_, err := db.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO user_organisation_roles (user_id, org_id, role_id)
		SELECT ?, ?, id FROM roles WHERE name = ?
	`, userID, orgID, roleName)
	if err != nil {
		return fmt.Errorf("assigning role %q to user %d in org %d: %w", roleName, userID, orgID, err)
	}
	return db.BumpUserTokenVersion(ctx, userID)
}

// RemoveRoleFromUser removes a named role from a user within an organisation.
func (db *SQLiteDB) RemoveRoleFromUser(ctx context.Context, userID, orgID int, roleName string) error {
	_, err := db.db.ExecContext(ctx, `
		DELETE FROM user_organisation_roles
		WHERE user_id = ? AND org_id = ?
		  AND role_id = (SELECT id FROM roles WHERE name = ?)
	`, userID, orgID, roleName)
	if err != nil {
		return err
	}
	return db.BumpUserTokenVersion(ctx, userID)
}

// SetUserOrgRoles replaces all roles for a user in a named organisation.
func (db *SQLiteDB) SetUserOrgRoles(ctx context.Context, userID int, orgName string, roleNames []string) error {
	var orgID int
	if err := db.db.QueryRowContext(ctx,
		"SELECT id FROM organisations WHERE name = ?", orgName,
	).Scan(&orgID); err != nil {
		return fmt.Errorf("org %q not found: %w", orgName, err)
	}

	if _, err := db.db.ExecContext(ctx,
		"INSERT OR IGNORE INTO user_organisations (user_id, org_id) VALUES (?, ?)",
		userID, orgID,
	); err != nil {
		return fmt.Errorf("adding user to org: %w", err)
	}

	if _, err := db.db.ExecContext(ctx,
		"DELETE FROM user_organisation_roles WHERE user_id = ? AND org_id = ?",
		userID, orgID,
	); err != nil {
		return fmt.Errorf("clearing roles for user %d in org %d: %w", userID, orgID, err)
	}

	for _, roleName := range roleNames {
		if _, err := db.db.ExecContext(ctx, `
			INSERT OR IGNORE INTO user_organisation_roles (user_id, org_id, role_id)
			SELECT ?, ?, id FROM roles WHERE name = ?
		`, userID, orgID, roleName); err != nil {
			return fmt.Errorf("assigning role %q to user %d: %w", roleName, userID, err)
		}
	}
	return db.BumpUserTokenVersion(ctx, userID)
}

// AssignUserToOrg adds a user to a named organisation and grants the given role names.
func (db *SQLiteDB) AssignUserToOrg(ctx context.Context, userID int, orgName string, roleNames []string) error {
	if err := db.AssignUserToOrgWithoutTokenBump(ctx, userID, orgName, roleNames); err != nil {
		return err
	}
	return db.BumpUserTokenVersion(ctx, userID)
}

// AssignUserToOrgWithoutTokenBump adds a user to an organisation without
// invalidating the current session. It is intended for org-create auto-assign.
func (db *SQLiteDB) AssignUserToOrgWithoutTokenBump(ctx context.Context, userID int, orgName string, roleNames []string) error {
	var orgID int
	if err := db.db.QueryRowContext(ctx,
		"SELECT id FROM organisations WHERE name = ?", orgName,
	).Scan(&orgID); err != nil {
		return fmt.Errorf("org %q not found: %w", orgName, err)
	}

	if _, err := db.db.ExecContext(ctx,
		"INSERT OR IGNORE INTO user_organisations (user_id, org_id) VALUES (?, ?)",
		userID, orgID,
	); err != nil {
		return fmt.Errorf("adding user to org: %w", err)
	}

	for _, roleName := range roleNames {
		if _, err := db.db.ExecContext(ctx, `
			INSERT OR IGNORE INTO user_organisation_roles (user_id, org_id, role_id)
			SELECT ?, ?, id FROM roles WHERE name = ?
		`, userID, orgID, roleName); err != nil {
			return fmt.Errorf("assigning role %q: %w", roleName, err)
		}
	}
	return nil
}

// scanUserRow scans a user row (without password_hash column).
func scanUserRow(rows *sql.Rows) (*sqldb.User, error) {
	var u sqldb.User
	var notifOpts, createdAtStr string
	var active sqliteBool
	var lastLoginStr *string
	if err := rows.Scan(
		&u.ID, &u.FirstName, &u.LastName, &u.LoginName, &u.Email,
		&notifOpts, &active, &lastLoginStr, &u.TokenVersion, &createdAtStr,
	); err != nil {
		return nil, fmt.Errorf("scanning user row: %w", err)
	}
	u.Active = active.Bool
	u.NotificationOptions = []byte(notifOpts)
	u.CreatedAt = parseTimestamp(createdAtStr)
	if lastLoginStr != nil {
		t := parseTimestamp(*lastLoginStr)
		u.LastLogin = &t
	}
	return &u, nil
}
