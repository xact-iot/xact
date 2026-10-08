package sqlite

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/xact-iot/xact/sqldb"
)

func TestSQLiteTreeNodeTransactionsAndLiteralSubtrees(t *testing.T) {
	ctx := context.Background()
	dbi, err := NewSQLiteDB(ctx, filepath.Join(t.TempDir(), "nodes.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer dbi.Close()
	db := dbi.(*SQLiteDB)
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, initialized, err := db.LoadTreeNodes(ctx, "default"); err != nil || initialized {
		t.Fatalf("initial state: %t %v", initialized, err)
	}
	if err := db.SaveTreeNodes(ctx, "default", sqldb.TreeConfigBatch{Replace: true}); err != nil {
		t.Fatal(err)
	}
	if nodes, initialized, err := db.LoadTreeNodes(ctx, "default"); err != nil || !initialized || len(nodes) != 0 {
		t.Fatalf("empty initialized state: %v %t %v", nodes, initialized, err)
	}
	makeNode := func(path, parent string) sqldb.TreeConfigNode {
		return sqldb.TreeConfigNode{Path: path, ParentPath: parent, Type: "Standard", Tags: json.RawMessage(`[{"name":"counter","type":"integer","enumValues":{"1":"one"},"pipeline":[{"type":"publish"}]}]`)}
	}
	batch := sqldb.TreeConfigBatch{Upserts: []sqldb.TreeConfigNode{
		makeNode("default", ""), makeNode("default.bus_%", "default"),
		makeNode("default.bus_%.meta", "default.bus_%"), makeNode("default.BUS_%", "default"),
		makeNode("default.bus_%0", "default"), makeNode("default.bus_XX", "default"),
	}}
	if err := db.SaveTreeNodes(ctx, "default", batch); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveTreeNodes(ctx, "default", sqldb.TreeConfigBatch{DeletePaths: []string{"default.bus_%"}}); err != nil {
		t.Fatal(err)
	}
	rows, _, err := db.LoadTreeNodes(ctx, "default")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatalf("literal subtree delete affected siblings: %+v", rows)
	}
	// A database-side failure after delete and one insert must roll back all
	// changes, not just the failing row.
	if _, err := db.db.ExecContext(ctx, `CREATE TRIGGER reject_bad_node BEFORE INSERT ON rtdb_config_nodes
		WHEN NEW.path = 'default.bad' BEGIN SELECT RAISE(ABORT, 'test rejection'); END`); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveTreeNodes(ctx, "default", sqldb.TreeConfigBatch{Replace: true, Upserts: []sqldb.TreeConfigNode{makeNode("default.good", "default"), makeNode("default.bad", "default")}}); err == nil {
		t.Fatal("expected database failure")
	}
	rows, initialized, err := db.LoadTreeNodes(ctx, "default")
	if err != nil || !initialized || len(rows) != 4 {
		t.Fatalf("failed transaction damaged snapshot: %v %v", rows, err)
	}
	if _, err := db.db.ExecContext(ctx, `UPDATE rtdb_config_snapshots SET format_version = 2`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.LoadTreeNodes(ctx, "default"); err == nil {
		t.Fatal("future format accepted")
	}
	if err := db.SaveTreeNodes(ctx, "default", sqldb.TreeConfigBatch{Replace: true}); err == nil {
		t.Fatal("future format overwritten")
	}
}
