package psql

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/xact-iot/xact/sqldb"
)

func TestPostgresTreeBatchRollsBackOnWriteFailure(t *testing.T) {
	db, mock := newMockPostgres(t)
	mock.ExpectQuery("SELECT id FROM organisations").WithArgs("default").WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO rtdb_config_snapshots").WithArgs(1).WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectExec("DELETE FROM rtdb_config_nodes").WithArgs(1, "default.bus", "default.bus.", "default.bus/").WillReturnResult(pgxmock.NewResult("DELETE", 2))
	mock.ExpectExec("INSERT INTO rtdb_config_nodes").WithArgs(1, pgxmock.AnyArg()).WillReturnError(fmt.Errorf("database write failed"))
	mock.ExpectRollback()
	err := db.SaveTreeNodes(context.Background(), "default", sqldb.TreeConfigBatch{DeletePaths: []string{"default.bus"}, Upserts: []sqldb.TreeConfigNode{{Path: "default.bus", ParentPath: "default", Type: "Device", Tags: json.RawMessage(`[]`)}}})
	if err == nil {
		t.Fatal("expected failure")
	}
}

// The opt-in integration test uses a disposable schema and the production DDL,
// without migrating or modifying the application's tables in the public schema.
func TestPostgresTreeNodesIntegration(t *testing.T) {
	url := os.Getenv("XACT_TEST_POSTGRES_URL")
	if url == "" {
		t.Skip("set XACT_TEST_POSTGRES_URL for isolated-schema integration")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal("integration database connection failed")
	}
	defer admin.Close(ctx)
	schema := fmt.Sprintf("xact_node_test_%d", time.Now().UnixNano())
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec(ctx, "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Error(err)
		}
	}()
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal("integration connection configuration invalid")
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	db := &PostgresDB{pool: pool, rawPool: pool}
	if _, err := pool.Exec(ctx, `CREATE TABLE organisations(id INTEGER PRIMARY KEY, name TEXT UNIQUE);
		INSERT INTO organisations VALUES (1,'default'),(2,'other');`+treeConfigSchema); err != nil {
		t.Fatal(err)
	}
	if _, initialized, err := db.LoadTreeNodes(ctx, "default"); err != nil || initialized {
		t.Fatalf("initial state: %t %v", initialized, err)
	}
	if err := db.SaveTreeNodes(ctx, "default", sqldb.TreeConfigBatch{Replace: true}); err != nil {
		t.Fatal(err)
	}
	if nodes, initialized, err := db.LoadTreeNodes(ctx, "default"); err != nil || !initialized || len(nodes) != 0 {
		t.Fatalf("empty state: %v %t %v", nodes, initialized, err)
	}
	makeNode := func(path, parent string) sqldb.TreeConfigNode {
		return sqldb.TreeConfigNode{Path: path, ParentPath: parent, Type: "Standard", Locked: true, IsArray: true, Tags: json.RawMessage(`[{"name":"lat","type":"float","deadband":0.001}]`)}
	}
	nodes := []sqldb.TreeConfigNode{makeNode("default", ""), makeNode("default.bus_%", "default"), makeNode("default.bus_%.meta", "default.bus_%"), makeNode("default.BUS_%", "default"), makeNode("default.bus_%0", "default")}
	// Cross the PostgreSQL insert chunk boundary too.
	for i := 0; i < 270; i++ {
		nodes = append(nodes, makeNode(fmt.Sprintf("default.static%d", i), "default"))
	}
	if err := db.SaveTreeNodes(ctx, "default", sqldb.TreeConfigBatch{Upserts: nodes}); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveTreeNodes(ctx, "other", sqldb.TreeConfigBatch{Upserts: []sqldb.TreeConfigNode{makeNode("default.bus_%", "default")}}); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveTreeNodes(ctx, "default", sqldb.TreeConfigBatch{DeletePaths: []string{"default.bus_%"}}); err != nil {
		t.Fatal(err)
	}
	rows, _, err := db.LoadTreeNodes(ctx, "default")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 273 {
		t.Fatalf("literal subtree deletion changed siblings: %d", len(rows))
	}
	if !rows[0].Locked || !rows[0].IsArray {
		t.Fatal("node flags did not round-trip")
	}
	if rows, _, err := db.LoadTreeNodes(ctx, "other"); err != nil || len(rows) != 1 {
		t.Fatal("delete escaped snapshot owner")
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE rtdb_config_nodes ADD CONSTRAINT reject_bad CHECK (path <> 'default.bad')`); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveTreeNodes(ctx, "default", sqldb.TreeConfigBatch{Replace: true, Upserts: []sqldb.TreeConfigNode{makeNode("default.bad", "default")}}); err == nil {
		t.Fatal("expected failed transaction")
	}
	if rows, _, err := db.LoadTreeNodes(ctx, "default"); err != nil || len(rows) != 273 {
		t.Fatal("failed transaction damaged snapshot")
	}
	if _, err := pool.Exec(ctx, `UPDATE rtdb_config_snapshots SET format_version=2 WHERE org_id=1`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.LoadTreeNodes(ctx, "default"); err == nil {
		t.Fatal("future format accepted")
	}
	if err := db.SaveTreeNodes(ctx, "default", sqldb.TreeConfigBatch{Replace: true}); err == nil {
		t.Fatal("future format overwritten")
	}
}
