package psql

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/xact-iot/xact/sqldb"
)

var _ sqldb.TreeConfigStore = (*PostgresDB)(nil)

func (db *PostgresDB) LoadTreeNodes(ctx context.Context, org string) ([]sqldb.TreeConfigNode, bool, error) {
	// A single query provides a consistent view of both marker and rows, even
	// when another transaction replaces the entire snapshot.
	rows, err := db.pool.Query(ctx, `
		SELECT s.format_version, n.path, COALESCE(n.parent_path, ''),
			COALESCE(n.node_type, ''), COALESCE(n.description, ''),
			COALESCE(n.template_name, ''), COALESCE(n.locked, FALSE),
			COALESCE(n.is_array, FALSE), COALESCE(n.tags, '[]'::jsonb)
		FROM rtdb_config_snapshots s JOIN organisations o ON o.id = s.org_id
		LEFT JOIN rtdb_config_nodes n ON n.org_id = s.org_id
		WHERE o.name = $1 ORDER BY length(n.path), n.path
	`, org)
	if err != nil {
		return nil, false, fmt.Errorf("load tree nodes: %w", err)
	}
	defer rows.Close()
	var nodes []sqldb.TreeConfigNode
	initialized := false
	for rows.Next() {
		var version int
		var path *string
		var n sqldb.TreeConfigNode
		if err := rows.Scan(&version, &path, &n.ParentPath, &n.Type, &n.Description,
			&n.TemplateName, &n.Locked, &n.IsArray, &n.Tags); err != nil {
			return nil, false, err
		}
		if version != 1 {
			return nil, false, fmt.Errorf("unsupported tree configuration format %d", version)
		}
		initialized = true
		if path != nil {
			n.Path = *path
			nodes = append(nodes, n)
		}
	}
	return nodes, initialized, rows.Err()
}

func (db *PostgresDB) SaveTreeNodes(ctx context.Context, org string, batch sqldb.TreeConfigBatch) error {
	if err := sqldb.ValidateTreeConfigBatch(batch); err != nil {
		return err
	}
	orgID, err := db.resolveOrgID(ctx, org)
	if err != nil {
		return err
	}
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	// Updating the marker also serializes writers to this snapshot owner.
	marker, err := tx.Exec(ctx, `INSERT INTO rtdb_config_snapshots (org_id, format_version, updated_at)
		VALUES ($1, 1, NOW()) ON CONFLICT (org_id) DO UPDATE SET updated_at = NOW()
		WHERE rtdb_config_snapshots.format_version = 1`, orgID)
	if err != nil {
		return err
	}
	if marker.RowsAffected() != 1 {
		return fmt.Errorf("unsupported tree configuration format")
	}
	if batch.Replace {
		if _, err := tx.Exec(ctx, `DELETE FROM rtdb_config_nodes WHERE org_id = $1`, orgID); err != nil {
			return err
		}
	} else {
		for _, path := range batch.DeletePaths {
			// Binary collation makes this an indexed literal subtree range;
			// underscores, percent signs and case differences are not wildcards.
			if _, err := tx.Exec(ctx, `DELETE FROM rtdb_config_nodes WHERE org_id = $1
				AND (path = $2 OR (path >= $3 AND path < $4))`, orgID, path, path+".", path+"/"); err != nil {
				return err
			}
		}
	}
	// Bound temporary JSON buffers and SQL round trips during initial migration.
	for offset := 0; offset < len(batch.Upserts); offset += 256 {
		data, err := json.Marshal(batch.Upserts[offset:min(offset+256, len(batch.Upserts))])
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO rtdb_config_nodes
				(org_id, path, parent_path, node_type, description, template_name, locked, is_array, tags)
			SELECT $1, n.path, n.parent_path, n.node_type, n.description, n.template_name, n.locked, n.is_array, n.tags
			FROM jsonb_to_recordset($2::jsonb) AS n(path TEXT, parent_path TEXT, node_type TEXT,
				description TEXT, template_name TEXT, locked BOOLEAN, is_array BOOLEAN, tags JSONB)
			ON CONFLICT (org_id, path) DO UPDATE SET parent_path = EXCLUDED.parent_path,
				node_type = EXCLUDED.node_type, description = EXCLUDED.description,
				template_name = EXCLUDED.template_name, locked = EXCLUDED.locked,
				is_array = EXCLUDED.is_array, tags = EXCLUDED.tags
		`, orgID, data); err != nil {
			return fmt.Errorf("upsert tree nodes: %w", err)
		}
	}
	return tx.Commit(ctx)
}

const treeConfigSchema = `
		CREATE TABLE IF NOT EXISTS rtdb_config_snapshots (
			org_id INTEGER PRIMARY KEY REFERENCES organisations(id) ON DELETE CASCADE,
			format_version INTEGER NOT NULL DEFAULT 1,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		CREATE TABLE IF NOT EXISTS rtdb_config_nodes (
			org_id INTEGER NOT NULL REFERENCES rtdb_config_snapshots(org_id) ON DELETE CASCADE,
			path TEXT COLLATE "C" NOT NULL,
			parent_path TEXT COLLATE "C" NOT NULL,
			node_type TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			template_name TEXT NOT NULL DEFAULT '',
			locked BOOLEAN NOT NULL DEFAULT FALSE,
			is_array BOOLEAN NOT NULL DEFAULT FALSE,
			tags JSONB NOT NULL DEFAULT '[]',
			PRIMARY KEY (org_id, path)
		);
		CREATE INDEX IF NOT EXISTS idx_rtdb_config_nodes_parent ON rtdb_config_nodes(org_id, parent_path);

`
