package sqlite

import (
	"context"
	"path/filepath"
	"testing"
)

func TestAgentTokenMigrationInvalidatesLegacyCredentials(t *testing.T) {
	ctx := context.Background()
	t.Setenv("XACT_BOOTSTRAP_ADMIN_PASSWORD", "test-bootstrap-password")
	dbi, err := NewSQLiteDB(ctx, filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer dbi.Close()
	db := dbi.(*SQLiteDB)
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	user, _, err := db.GetUserByLogin(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	old, err := db.CreateAgentToken(ctx, "default", user.ID, "old", []string{"SystemAdmin"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.RawDB().Exec("ALTER TABLE org_agent_tokens DROP COLUMN user_token_version"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := db.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
		if token, err := db.ResolveAgentToken(ctx, old.Token); err != nil || token != nil {
			t.Fatalf("legacy token survived migration: %v", err)
		}
	}
	fresh, err := db.CreateAgentToken(ctx, "default", user.ID, "fresh", []string{"SystemAdmin"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token, err := db.ResolveAgentToken(ctx, fresh.Token); err != nil || token == nil {
		t.Fatalf("reissued token rejected: %v", err)
	}
}
