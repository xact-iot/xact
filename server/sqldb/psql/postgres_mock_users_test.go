package psql

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/xact-iot/xact/sqldb"
)

func jsonRaw(s string) json.RawMessage {
	return json.RawMessage(s)
}

func TestPostgresPasswordHelpers(t *testing.T) {
	hash, err := HashPassword("secret")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !CheckPassword(hash, "secret") {
		t.Fatal("expected password to verify")
	}
	if CheckPassword(hash, "wrong") {
		t.Fatal("wrong password verified")
	}
	password, err := GenerateRandomPassword()
	if err != nil {
		t.Fatalf("GenerateRandomPassword: %v", err)
	}
	if len(password) != 12 {
		t.Fatalf("unexpected generated password length: %d", len(password))
	}
}

func TestPostgresUsersWithPGXMock(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 6, 1, 2, 3, 4, 0, time.UTC)
	db, mock := newMockPostgres(t)

	userRows := func() *pgxmock.Rows {
		return pgxmock.NewRows([]string{"id", "first_name", "last_name", "login_name", "email", "notification_options", "active", "last_login", "token_version", "created_at"}).
			AddRow(5, "Grace", "Hopper", "grace", "grace@example.test", []byte(`{"email":true}`), true, &now, 2, now)
	}
	expectUserOrgs := func(userID int) {
		mock.ExpectQuery("SELECT o.id, o.name").
			WithArgs(userID).
			WillReturnRows(pgxmock.NewRows([]string{"id", "name"}).AddRow(9, "default"))
		mock.ExpectQuery("SELECT r.name").
			WithArgs(userID, 9).
			WillReturnRows(pgxmock.NewRows([]string{"name"}).AddRow("Admin").AddRow("User"))
	}

	mock.ExpectQuery("SELECT id, name, description FROM roles ORDER BY id").
		WillReturnRows(pgxmock.NewRows([]string{"id", "name", "description"}).AddRow(1, "Admin", "admin"))
	roles, err := db.ListRoles(ctx)
	if err != nil {
		t.Fatalf("ListRoles: %v", err)
	}
	if len(roles) != 1 || roles[0].Name != "Admin" {
		t.Fatalf("unexpected roles: %#v", roles)
	}

	mock.ExpectQuery("SELECT id, first_name, last_name, login_name, email").
		WillReturnRows(userRows())
	expectUserOrgs(5)
	users, err := db.ListUsers(ctx)
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 1 || len(users[0].Orgs) != 1 || len(users[0].Orgs[0].Roles) != 2 {
		t.Fatalf("unexpected users: %#v", users)
	}

	mock.ExpectQuery("SELECT id, first_name, last_name, login_name, email").
		WithArgs(5).
		WillReturnRows(userRows())
	expectUserOrgs(5)
	user, err := db.GetUserByID(ctx, 5)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if user == nil || user.LoginName != "grace" {
		t.Fatalf("unexpected user: %#v", user)
	}

	mock.ExpectQuery("SELECT id, first_name, last_name, login_name, email").
		WithArgs("grace").
		WillReturnRows(pgxmock.NewRows([]string{"id", "first_name", "last_name", "login_name", "email", "notification_options", "active", "last_login", "token_version", "created_at", "password_hash"}).
			AddRow(5, "Grace", "Hopper", "grace", "grace@example.test", []byte(`{}`), true, &now, 2, now, "hash"))
	expectUserOrgs(5)
	loginUser, hash, err := db.GetUserByLogin(ctx, "grace")
	if err != nil {
		t.Fatalf("GetUserByLogin: %v", err)
	}
	if loginUser == nil || hash != "hash" {
		t.Fatalf("unexpected login user/hash: %#v %q", loginUser, hash)
	}

	mock.ExpectQuery("SELECT id, first_name, last_name, login_name, email").
		WithArgs(404).
		WillReturnError(pgx.ErrNoRows)
	missing, err := db.GetUserByID(ctx, 404)
	if err != nil || missing != nil {
		t.Fatalf("GetUserByID missing = %#v, %v", missing, err)
	}

	create := &sqldb.User{FirstName: "Ada", LastName: "Lovelace", LoginName: "ada", Email: "ada@example.test", Active: true}
	mock.ExpectQuery("INSERT INTO users").
		WithArgs(create.FirstName, create.LastName, create.LoginName, "pw-hash", create.Email, jsonRaw("{}"), create.Active).
		WillReturnRows(pgxmock.NewRows([]string{"id", "token_version", "created_at"}).AddRow(6, 1, now))
	if err := db.CreateUser(ctx, create, "pw-hash"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if create.ID != 6 || create.TokenVersion != 1 {
		t.Fatalf("create did not hydrate user: %#v", create)
	}

	mock.ExpectExec("UPDATE users SET").
		WithArgs(6, create.FirstName, create.LastName, create.Email, jsonRaw("{}"), create.Active).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	if err := db.UpdateUser(ctx, create); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}

	mock.ExpectExec("UPDATE users SET password_hash").
		WithArgs(6, "new-hash").
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))
	if err := db.SetUserPassword(ctx, 6, "new-hash"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected SetUserPassword not found, got %v", err)
	}

	mock.ExpectQuery("SELECT active, token_version FROM users WHERE id").
		WithArgs(6).
		WillReturnRows(pgxmock.NewRows([]string{"active", "token_version"}).AddRow(true, 3))
	active, version, err := db.GetUserAuthState(ctx, 6)
	if err != nil || !active || version != 3 {
		t.Fatalf("GetUserAuthState = %t %d %v", active, version, err)
	}

	mock.ExpectExec("UPDATE users SET token_version").
		WithArgs(6).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	if err := db.BumpUserTokenVersion(ctx, 6); err != nil {
		t.Fatalf("BumpUserTokenVersion: %v", err)
	}

	mock.ExpectExec("UPDATE users SET last_login").WithArgs(6).WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	if err := db.UpdateLastLogin(ctx, 6); err != nil {
		t.Fatalf("UpdateLastLogin: %v", err)
	}
}

func TestPostgresUserOrgMutationsWithPGXMock(t *testing.T) {
	ctx := context.Background()
	db, mock := newMockPostgres(t)

	mock.ExpectExec("INSERT INTO user_organisations").
		WithArgs(5, 9).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectExec("UPDATE users SET token_version").WithArgs(5).WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	if err := db.AddUserToOrg(ctx, 5, 9); err != nil {
		t.Fatalf("AddUserToOrg: %v", err)
	}

	mock.ExpectExec("INSERT INTO user_organisation_roles").
		WithArgs(5, 9, "Admin").
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectExec("UPDATE users SET token_version").WithArgs(5).WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	if err := db.AssignRoleToUser(ctx, 5, 9, "Admin"); err != nil {
		t.Fatalf("AssignRoleToUser: %v", err)
	}

	mock.ExpectExec("DELETE FROM user_organisation_roles").
		WithArgs(5, 9, "User").
		WillReturnResult(pgxmock.NewResult("DELETE", 1))
	mock.ExpectExec("UPDATE users SET token_version").WithArgs(5).WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	if err := db.RemoveRoleFromUser(ctx, 5, 9, "User"); err != nil {
		t.Fatalf("RemoveRoleFromUser: %v", err)
	}

	mock.ExpectQuery("SELECT id FROM organisations WHERE name").WithArgs("default").WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow(9))
	mock.ExpectExec("INSERT INTO user_organisations").WithArgs(5, 9).WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectExec("DELETE FROM user_organisation_roles WHERE user_id").WithArgs(5, 9).WillReturnResult(pgxmock.NewResult("DELETE", 2))
	mock.ExpectExec("INSERT INTO user_organisation_roles").WithArgs(5, 9, "Admin").WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectExec("INSERT INTO user_organisation_roles").WithArgs(5, 9, "Operator").WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectExec("UPDATE users SET token_version").WithArgs(5).WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	if err := db.SetUserOrgRoles(ctx, 5, "default", []string{"Admin", "Operator"}); err != nil {
		t.Fatalf("SetUserOrgRoles: %v", err)
	}

	mock.ExpectQuery("SELECT id FROM organisations WHERE name").WithArgs("ops").WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow(10))
	mock.ExpectExec("INSERT INTO user_organisations").WithArgs(5, 10).WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectExec("INSERT INTO user_organisation_roles").WithArgs(5, 10, "Manager").WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectExec("UPDATE users SET token_version").WithArgs(5).WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	if err := db.AssignUserToOrg(ctx, 5, "ops", []string{"Manager"}); err != nil {
		t.Fatalf("AssignUserToOrg: %v", err)
	}
}

func TestPostgresAPIKeysWithPGXMock(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 6, 2, 3, 4, 5, 0, time.UTC)
	db, mock := newMockPostgres(t)

	mock.ExpectQuery("SELECT k.id, o.name, k.name, k.key_prefix, k.key_last4, k.created_at").
		WithArgs("default").
		WillReturnRows(pgxmock.NewRows([]string{"id", "org_name", "name", "key_prefix", "key_last4", "created_at"}).
			AddRow(1, "default", "ingest", "xact_", "abcd", now))
	keys, err := db.ListAPIKeys(ctx, "default")
	if err != nil {
		t.Fatalf("ListAPIKeys: %v", err)
	}
	if len(keys) != 1 || keys[0].Key == "" || keys[0].KeyLast4 != "abcd" {
		t.Fatalf("unexpected api keys: %#v", keys)
	}

	mock.ExpectExec("DELETE FROM org_api_keys").
		WithArgs("default", 1).
		WillReturnResult(pgxmock.NewResult("DELETE", 0))
	if err := db.DeleteAPIKey(ctx, "default", 1); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected DeleteAPIKey not found, got %v", err)
	}

	rawKey := "xact_abcdefghijklmnopqrstuvwxyz"
	keyHash := sqldb.HashAPIKey(rawKey)
	mock.ExpectQuery("SELECT o.name FROM org_api_keys").
		WithArgs(keyHash).
		WillReturnError(pgx.ErrNoRows)
	mock.ExpectQuery("SELECT k.id, o.name FROM org_api_keys").
		WithArgs(rawKey).
		WillReturnRows(pgxmock.NewRows([]string{"id", "name"}).AddRow(7, "default"))
	mock.ExpectExec("UPDATE org_api_keys").
		WithArgs(7, sqldb.APIKeyPlaceholder(7, keyHash), keyHash, sqldb.APIKeyPrefix(rawKey), sqldb.APIKeyLast4(rawKey)).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	org, err := db.GetAPIKeyOrg(ctx, rawKey)
	if err != nil {
		t.Fatalf("GetAPIKeyOrg legacy: %v", err)
	}
	if org != "default" {
		t.Fatalf("unexpected org: %q", org)
	}
}
