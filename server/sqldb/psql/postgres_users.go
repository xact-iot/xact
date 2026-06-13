package psql

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
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
	b := make([]byte, 9) // 9 bytes → 12 base64 chars
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating random password: %w", err)
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// seedAdminUser creates the bootstrap admin user if it doesn't exist.
func (db *PostgresDB) seedAdminUser(ctx context.Context) error {
	var count int
	if err := db.pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM users WHERE login_name = 'admin'",
	).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	cred, err := sqldb.ResolveBootstrapAdminPassword("./data/bootstrap-admin-password.txt")
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

	var orgID int
	if err := db.pool.QueryRow(ctx,
		"SELECT id FROM organisations WHERE name = 'default'",
	).Scan(&orgID); err != nil {
		return fmt.Errorf("finding default org: %w", err)
	}

	var roleID int
	if err := db.pool.QueryRow(ctx,
		"SELECT id FROM roles WHERE name = 'SystemAdmin'",
	).Scan(&roleID); err != nil {
		return fmt.Errorf("finding SystemAdmin role: %w", err)
	}

	var userID int
	if err := db.pool.QueryRow(ctx, `
		INSERT INTO users (first_name, last_name, login_name, password_hash, email)
		VALUES ('System', 'Admin', 'admin', $1, 'admin@localhost')
		RETURNING id
	`, hash).Scan(&userID); err != nil {
		return fmt.Errorf("inserting admin user: %w", err)
	}

	if _, err := db.pool.Exec(ctx,
		"INSERT INTO user_organisations (user_id, org_id) VALUES ($1, $2)",
		userID, orgID,
	); err != nil {
		return fmt.Errorf("adding admin to default org: %w", err)
	}

	if _, err := db.pool.Exec(ctx,
		"INSERT INTO user_organisation_roles (user_id, org_id, role_id) VALUES ($1, $2, $3)",
		userID, orgID, roleID,
	); err != nil {
		return fmt.Errorf("assigning SystemAdmin role to admin: %w", err)
	}

	logBootstrapAdminCredential(cred)
	return nil
}

func logBootstrapAdminCredential(cred sqldb.AdminBootstrapCredential) {
	if cred.Set {
		log.Printf("Created bootstrap admin user 'admin' using password from %s", cred.Source)
		return
	}
	log.Printf("Created bootstrap admin user 'admin' with password unset; first browser login must set it")
}

// ListRoles returns all defined roles.
func (db *PostgresDB) ListRoles(ctx context.Context) ([]sqldb.Role, error) {
	rows, err := db.pool.Query(ctx,
		"SELECT id, name, description FROM roles ORDER BY id")
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
func (db *PostgresDB) ListUsers(ctx context.Context) ([]sqldb.User, error) {
	rows, err := db.pool.Query(ctx, `
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
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, *u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Attach org memberships
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
func (db *PostgresDB) GetUserByID(ctx context.Context, id int) (*sqldb.User, error) {
	row := db.pool.QueryRow(ctx, `
		SELECT id, first_name, last_name, login_name, email,
		       notification_options, active, last_login, token_version, created_at
		FROM users WHERE id = $1
	`, id)

	u, err := scanUser(row)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting user %d: %w", id, err)
	}

	u.Orgs, err = db.GetUserOrgs(ctx, u.ID)
	return u, err
}

// GetUserByLogin finds a user by login_name or email. Returns (user, passwordHash, error).
// Returns nil user if not found.
func (db *PostgresDB) GetUserByLogin(ctx context.Context, login string) (*sqldb.User, string, error) {
	var hash string
	row := db.pool.QueryRow(ctx, `
		SELECT id, first_name, last_name, login_name, email,
		       notification_options, active, last_login, token_version, created_at, password_hash
		FROM users
		WHERE (login_name = $1 OR email = $1) AND active = TRUE
	`, login)

	var u sqldb.User
	var lastLogin *time.Time
	if err := row.Scan(
		&u.ID, &u.FirstName, &u.LastName, &u.LoginName, &u.Email,
		&u.NotificationOptions, &u.Active, &lastLogin, &u.TokenVersion, &u.CreatedAt, &hash,
	); err == pgx.ErrNoRows {
		return nil, "", nil
	} else if err != nil {
		return nil, "", fmt.Errorf("getting user by login %q: %w", login, err)
	}
	u.LastLogin = lastLogin

	orgs, err := db.GetUserOrgs(ctx, u.ID)
	if err != nil {
		return nil, "", err
	}
	u.Orgs = orgs

	return &u, hash, nil
}

// CreateUser creates a new user record and returns the assigned ID via user.ID.
func (db *PostgresDB) CreateUser(ctx context.Context, user *sqldb.User, passwordHash string) error {
	opts := user.NotificationOptions
	if opts == nil {
		opts = []byte("{}")
	}
	return db.pool.QueryRow(ctx, `
		INSERT INTO users (first_name, last_name, login_name, password_hash, email,
		                   notification_options, active, token_version)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 1)
		RETURNING id, token_version, created_at
	`, user.FirstName, user.LastName, user.LoginName, passwordHash, user.Email,
		opts, user.Active,
	).Scan(&user.ID, &user.TokenVersion, &user.CreatedAt)
}

// UpdateUser updates mutable user fields (not password, not login_name).
func (db *PostgresDB) UpdateUser(ctx context.Context, user *sqldb.User) error {
	opts := user.NotificationOptions
	if opts == nil {
		opts = []byte("{}")
	}
	tag, err := db.pool.Exec(ctx, `
		UPDATE users SET
			first_name = $2, last_name = $3, email = $4,
			notification_options = $5, active = $6, updated_at = NOW()
		WHERE id = $1
	`, user.ID, user.FirstName, user.LastName, user.Email, opts, user.Active)
	if err != nil {
		return fmt.Errorf("updating user %d: %w", user.ID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("user %d not found", user.ID)
	}
	return nil
}

// SetUserPassword updates a user's password hash.
func (db *PostgresDB) SetUserPassword(ctx context.Context, id int, passwordHash string) error {
	tag, err := db.pool.Exec(ctx,
		"UPDATE users SET password_hash = $2, token_version = token_version + 1, updated_at = NOW() WHERE id = $1",
		id, passwordHash,
	)
	if err != nil {
		return fmt.Errorf("setting password for user %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("user %d not found", id)
	}
	return nil
}

// GetUserAuthState returns the active flag and token version for JWT validation.
func (db *PostgresDB) GetUserAuthState(ctx context.Context, id int) (bool, int, error) {
	var active bool
	var tokenVersion int
	err := db.pool.QueryRow(ctx,
		"SELECT active, token_version FROM users WHERE id = $1", id,
	).Scan(&active, &tokenVersion)
	if err == pgx.ErrNoRows {
		return false, 0, nil
	}
	if err != nil {
		return false, 0, fmt.Errorf("getting auth state for user %d: %w", id, err)
	}
	return active, tokenVersion, nil
}

// BumpUserTokenVersion invalidates existing JWTs for a user.
func (db *PostgresDB) BumpUserTokenVersion(ctx context.Context, id int) error {
	tag, err := db.pool.Exec(ctx,
		"UPDATE users SET token_version = token_version + 1, updated_at = NOW() WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("bumping token version for user %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("user %d not found", id)
	}
	return nil
}

// UpdateLastLogin records now as last_login for the user.
func (db *PostgresDB) UpdateLastLogin(ctx context.Context, id int) error {
	_, err := db.pool.Exec(ctx,
		"UPDATE users SET last_login = NOW() WHERE id = $1", id)
	return err
}

// GetUserOrgs returns all organisations a user belongs to, with their role names.
func (db *PostgresDB) GetUserOrgs(ctx context.Context, userID int) ([]sqldb.UserOrg, error) {
	rows, err := db.pool.Query(ctx, `
		SELECT o.id, o.name
		FROM user_organisations uo
		JOIN organisations o ON o.id = uo.org_id
		WHERE uo.user_id = $1
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

	// Fetch roles for each org
	for i, uo := range orgs {
		roleRows, err := db.pool.Query(ctx, `
			SELECT r.name
			FROM user_organisation_roles uor
			JOIN roles r ON r.id = uor.role_id
			WHERE uor.user_id = $1 AND uor.org_id = $2
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
func (db *PostgresDB) AddUserToOrg(ctx context.Context, userID, orgID int) error {
	_, err := db.pool.Exec(ctx,
		"INSERT INTO user_organisations (user_id, org_id) VALUES ($1, $2) ON CONFLICT DO NOTHING",
		userID, orgID,
	)
	if err != nil {
		return err
	}
	return db.BumpUserTokenVersion(ctx, userID)
}

// AssignRoleToUser assigns a named role to a user within an organisation.
func (db *PostgresDB) AssignRoleToUser(ctx context.Context, userID, orgID int, roleName string) error {
	_, err := db.pool.Exec(ctx, `
		INSERT INTO user_organisation_roles (user_id, org_id, role_id)
		SELECT $1, $2, id FROM roles WHERE name = $3
		ON CONFLICT DO NOTHING
	`, userID, orgID, roleName)
	if err != nil {
		return fmt.Errorf("assigning role %q to user %d in org %d: %w", roleName, userID, orgID, err)
	}
	return db.BumpUserTokenVersion(ctx, userID)
}

// RemoveRoleFromUser removes a named role from a user within an organisation.
func (db *PostgresDB) RemoveRoleFromUser(ctx context.Context, userID, orgID int, roleName string) error {
	_, err := db.pool.Exec(ctx, `
		DELETE FROM user_organisation_roles
		WHERE user_id = $1 AND org_id = $2
		  AND role_id = (SELECT id FROM roles WHERE name = $3)
	`, userID, orgID, roleName)
	if err != nil {
		return err
	}
	return db.BumpUserTokenVersion(ctx, userID)
}

// SetUserOrgRoles replaces all roles for a user in a named organisation.
// Any existing roles are removed first so the result is exactly roleNames.
func (db *PostgresDB) SetUserOrgRoles(ctx context.Context, userID int, orgName string, roleNames []string) error {
	var orgID int
	if err := db.pool.QueryRow(ctx,
		"SELECT id FROM organisations WHERE name = $1", orgName,
	).Scan(&orgID); err != nil {
		return fmt.Errorf("org %q not found: %w", orgName, err)
	}

	// Ensure the user is a member of the org.
	if _, err := db.pool.Exec(ctx,
		"INSERT INTO user_organisations (user_id, org_id) VALUES ($1, $2) ON CONFLICT DO NOTHING",
		userID, orgID,
	); err != nil {
		return fmt.Errorf("adding user to org: %w", err)
	}

	// Remove all existing role assignments for this user in this org.
	if _, err := db.pool.Exec(ctx,
		"DELETE FROM user_organisation_roles WHERE user_id = $1 AND org_id = $2",
		userID, orgID,
	); err != nil {
		return fmt.Errorf("clearing roles for user %d in org %d: %w", userID, orgID, err)
	}

	// Insert the new roles.
	for _, roleName := range roleNames {
		if _, err := db.pool.Exec(ctx, `
			INSERT INTO user_organisation_roles (user_id, org_id, role_id)
			SELECT $1, $2, id FROM roles WHERE name = $3
			ON CONFLICT DO NOTHING
		`, userID, orgID, roleName); err != nil {
			return fmt.Errorf("assigning role %q to user %d: %w", roleName, userID, err)
		}
	}
	return db.BumpUserTokenVersion(ctx, userID)
}

// AssignUserToOrg adds a user to a named organisation and grants the given role names.
func (db *PostgresDB) AssignUserToOrg(ctx context.Context, userID int, orgName string, roleNames []string) error {
	var orgID int
	if err := db.pool.QueryRow(ctx,
		"SELECT id FROM organisations WHERE name = $1", orgName,
	).Scan(&orgID); err != nil {
		return fmt.Errorf("org %q not found: %w", orgName, err)
	}

	if _, err := db.pool.Exec(ctx,
		"INSERT INTO user_organisations (user_id, org_id) VALUES ($1, $2) ON CONFLICT DO NOTHING",
		userID, orgID,
	); err != nil {
		return fmt.Errorf("adding user to org: %w", err)
	}

	for _, roleName := range roleNames {
		if _, err := db.pool.Exec(ctx, `
			INSERT INTO user_organisation_roles (user_id, org_id, role_id)
			SELECT $1, $2, id FROM roles WHERE name = $3
			ON CONFLICT DO NOTHING
		`, userID, orgID, roleName); err != nil {
			return fmt.Errorf("assigning role %q: %w", roleName, err)
		}
	}
	return db.BumpUserTokenVersion(ctx, userID)
}

// scanUser scans a user row (without password_hash column).
type userScanner interface {
	Scan(dest ...any) error
}

func scanUser(row userScanner) (*sqldb.User, error) {
	var u sqldb.User
	var lastLogin *time.Time
	err := row.Scan(
		&u.ID, &u.FirstName, &u.LastName, &u.LoginName, &u.Email,
		&u.NotificationOptions, &u.Active, &lastLogin, &u.TokenVersion, &u.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	u.LastLogin = lastLogin
	return &u, nil
}
