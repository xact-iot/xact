package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/xact-iot/xact/sqldb"
)

var _ sqldb.TreeConfigStore = (*SQLiteDB)(nil)

func (db *SQLiteDB) LoadTreeNodes(ctx context.Context, org string) ([]sqldb.TreeConfigNode, bool, error) {
	rows, err := db.db.QueryContext(ctx, `
		SELECT s.format_version, n.path, COALESCE(n.parent_path, ''),
			COALESCE(n.node_type, ''), COALESCE(n.description, ''),
			COALESCE(n.template_name, ''), COALESCE(n.locked, 0),
			COALESCE(n.is_array, 0), COALESCE(n.tags, '[]')
		FROM rtdb_config_snapshots s JOIN organisations o ON o.id = s.org_id
		LEFT JOIN rtdb_config_nodes n ON n.org_id = s.org_id
		WHERE o.name = ? ORDER BY length(n.path), n.path
	`, org)
	if err != nil {
		return nil, false, fmt.Errorf("load tree nodes: %w", err)
	}
	defer rows.Close()
	var nodes []sqldb.TreeConfigNode
	initialized := false
	for rows.Next() {
		var version int
		var path sql.NullString
		var tags string
		var n sqldb.TreeConfigNode
		if err := rows.Scan(&version, &path, &n.ParentPath, &n.Type, &n.Description,
			&n.TemplateName, &n.Locked, &n.IsArray, &tags); err != nil {
			return nil, false, err
		}
		if version != 1 {
			return nil, false, fmt.Errorf("unsupported tree configuration format %d", version)
		}
		initialized = true
		if path.Valid {
			n.Path = path.String
			n.Tags = []byte(tags)
			nodes = append(nodes, n)
		}
	}
	return nodes, initialized, rows.Err()
}

func (db *SQLiteDB) SaveTreeNodes(ctx context.Context, org string, batch sqldb.TreeConfigBatch) error {
	if err := sqldb.ValidateTreeConfigBatch(batch); err != nil {
		return err
	}
	orgID, err := db.resolveOrgID(ctx, org)
	if err != nil {
		return err
	}
	tx, err := db.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	marker, err := tx.ExecContext(ctx, `INSERT INTO rtdb_config_snapshots (org_id, format_version, updated_at)
		VALUES (?, 1, ?) ON CONFLICT (org_id) DO UPDATE SET updated_at = excluded.updated_at
		WHERE rtdb_config_snapshots.format_version = 1`, orgID, formatTimestamp(time.Now()))
	if err != nil {
		return err
	}
	if affected, err := marker.RowsAffected(); err != nil || affected != 1 {
		return fmt.Errorf("unsupported tree configuration format")
	}
	if batch.Replace {
		if _, err := tx.ExecContext(ctx, `DELETE FROM rtdb_config_nodes WHERE org_id = ?`, orgID); err != nil {
			return err
		}
	} else {
		for _, path := range batch.DeletePaths {
			if _, err := tx.ExecContext(ctx, `DELETE FROM rtdb_config_nodes WHERE org_id = ?
				AND (path = ? OR (path >= ? AND path < ?))`, orgID, path, path+".", path+"/"); err != nil {
				return err
			}
		}
	}
	if len(batch.Upserts) > 0 {
		stmt, err := tx.PrepareContext(ctx, `INSERT INTO rtdb_config_nodes
			(org_id, path, parent_path, node_type, description, template_name, locked, is_array, tags)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (org_id, path) DO UPDATE SET parent_path = excluded.parent_path,
				node_type = excluded.node_type, description = excluded.description,
				template_name = excluded.template_name, locked = excluded.locked,
				is_array = excluded.is_array, tags = excluded.tags`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, n := range batch.Upserts {
			if _, err := stmt.ExecContext(ctx, orgID, n.Path, n.ParentPath, n.Type,
				n.Description, n.TemplateName, n.Locked, n.IsArray, string(n.Tags)); err != nil {
				return fmt.Errorf("upsert tree node %q: %w", n.Path, err)
			}
		}
	}
	return tx.Commit()
}
