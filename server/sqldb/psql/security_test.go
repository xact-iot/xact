package psql

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"github.com/pashagolub/pgxmock/v4"
	"github.com/xact-iot/xact/sqldb"
)

func TestPostgresBootstrapClaimIsConditional(t *testing.T) {
	for _, rows := range []int64{0, 1} {
		db, mock := newMockPostgres(t)
		mock.ExpectExec(regexp.QuoteMeta("UPDATE users SET password_hash = $2, token_version = token_version + 1, updated_at = NOW() WHERE id = $1 AND login_name = 'admin' AND active = TRUE AND password_hash = $3")).
			WithArgs(1, "new-hash", sqldb.UnsetBootstrapAdminHash).WillReturnResult(pgxmock.NewResult("UPDATE", rows))
		claimed, err := db.ClaimBootstrapAdminPassword(context.Background(), 1, "new-hash")
		if err != nil || claimed != (rows == 1) {
			t.Fatalf("rows=%d claim=%v error=%v", rows, claimed, err)
		}
	}
	db, mock := newMockPostgres(t)
	mock.ExpectExec("UPDATE users SET password_hash").WithArgs(1, "new-hash", sqldb.UnsetBootstrapAdminHash).WillReturnError(errors.New("database unavailable"))
	if claimed, err := db.ClaimBootstrapAdminPassword(context.Background(), 1, "new-hash"); err == nil || claimed {
		t.Fatal("database error authorized bootstrap")
	}
}
