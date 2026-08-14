package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/xact-iot/xact/visualscripts"
)

func (db *SQLiteDB) ListVisualScripts(ctx context.Context, org string) ([]visualscripts.Script, error) {
	rows, err := db.db.QueryContext(ctx, `SELECT id, org_name, name, description, desired_state, latest_revision, active_revision, backup_revision, simulation, activate, created_by, updated_by, created_at, updated_at FROM visual_scripts WHERE org_name = ? ORDER BY name`, org)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []visualscripts.Script{}
	for rows.Next() {
		item, err := scanSQLiteScript(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (db *SQLiteDB) GetVisualScript(ctx context.Context, org, id string) (*visualscripts.Script, error) {
	item, err := scanSQLiteScript(db.db.QueryRowContext(ctx, `SELECT id, org_name, name, description, desired_state, latest_revision, active_revision, backup_revision, simulation, activate, created_by, updated_by, created_at, updated_at FROM visual_scripts WHERE org_name = ? AND id = ?`, org, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return item, err
}

func (db *SQLiteDB) ListActivatedVisualScripts(ctx context.Context) ([]visualscripts.Script, error) {
	rows, err := db.db.QueryContext(ctx, `SELECT id, org_name, name, description, desired_state, latest_revision, active_revision, backup_revision, simulation, activate, created_by, updated_by, created_at, updated_at FROM visual_scripts WHERE activate = 1 ORDER BY org_name, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []visualscripts.Script{}
	for rows.Next() {
		item, err := scanSQLiteScript(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

type sqliteScanner interface{ Scan(...any) error }

func scanSQLiteScript(row sqliteScanner) (*visualscripts.Script, error) {
	var item visualscripts.Script
	var active, backup sql.NullInt64
	var simulation, activate int
	var created, updated string
	if err := row.Scan(&item.ID, &item.OrgName, &item.Name, &item.Description, &item.DesiredState, &item.LatestRevision, &active, &backup, &simulation, &activate, &item.CreatedBy, &item.UpdatedBy, &created, &updated); err != nil {
		return nil, err
	}
	if active.Valid {
		value := int(active.Int64)
		item.ActiveRevision = &value
	}
	if backup.Valid {
		value := int(backup.Int64)
		item.BackupRevision = &value
		item.HasBackup = true
	}
	item.Simulation, item.Activate = simulation != 0, activate != 0
	item.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	item.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	item.OutOfDate = item.LatestRevision > 0 && (item.ActiveRevision == nil || item.LatestRevision > *item.ActiveRevision)
	return &item, nil
}

func (db *SQLiteDB) CreateVisualScript(ctx context.Context, org string, item *visualscripts.Script) error {
	item.ID = newUUID()
	now := time.Now().UTC()
	item.OrgName, item.DesiredState, item.CreatedAt, item.UpdatedAt = org, "stopped", now, now
	_, err := db.db.ExecContext(ctx, `INSERT INTO visual_scripts (id, org_name, name, description, desired_state, latest_revision, created_by, updated_by, created_at, updated_at) VALUES (?, ?, ?, ?, 'stopped', 0, ?, ?, ?, ?)`, item.ID, org, item.Name, item.Description, item.CreatedBy, item.UpdatedBy, formatTimestamp(now), formatTimestamp(now))
	return err
}

func (db *SQLiteDB) UpdateVisualScript(ctx context.Context, org, id, name, description string, actor int) error {
	res, err := db.db.ExecContext(ctx, `UPDATE visual_scripts SET name = ?, description = ?, updated_by = ?, updated_at = ? WHERE org_name = ? AND id = ?`, name, description, actor, formatTimestamp(time.Now().UTC()), org, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return visualscripts.ErrNotFound
	}
	return nil
}

func (db *SQLiteDB) DeleteVisualScript(ctx context.Context, org, id string) error {
	res, err := db.db.ExecContext(ctx, `DELETE FROM visual_scripts WHERE org_name = ? AND id = ?`, org, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return visualscripts.ErrNotFound
	}
	return nil
}

func (db *SQLiteDB) ListVisualScriptRevisions(ctx context.Context, org, scriptID string) ([]visualscripts.Revision, error) {
	rows, err := db.db.QueryContext(ctx, `SELECT script_id, org_name, revision, schema_version, graph_json, graph_hash, validation_status, diagnostics_json, capabilities_json, created_by, created_at FROM visual_script_revisions WHERE org_name = ? AND script_id = ? ORDER BY revision DESC`, org, scriptID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []visualscripts.Revision{}
	for rows.Next() {
		item, err := scanSQLiteRevision(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (db *SQLiteDB) GetVisualScriptRevision(ctx context.Context, org, scriptID string, revision int) (*visualscripts.Revision, error) {
	item, err := scanSQLiteRevision(db.db.QueryRowContext(ctx, `SELECT script_id, org_name, revision, schema_version, graph_json, graph_hash, validation_status, diagnostics_json, capabilities_json, created_by, created_at FROM visual_script_revisions WHERE org_name = ? AND script_id = ? AND revision = ?`, org, scriptID, revision))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return item, err
}

func scanSQLiteRevision(row sqliteScanner) (*visualscripts.Revision, error) {
	var item visualscripts.Revision
	var graph, diagnostics, capabilities, created string
	if err := row.Scan(&item.ScriptID, &item.OrgName, &item.Revision, &item.SchemaVersion, &graph, &item.GraphHash, &item.ValidationStatus, &diagnostics, &capabilities, &item.CreatedBy, &created); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(graph), &item.Graph); err != nil {
		return nil, fmt.Errorf("decoding visual script graph: %w", err)
	}
	_ = json.Unmarshal([]byte(diagnostics), &item.Diagnostics)
	_ = json.Unmarshal([]byte(capabilities), &item.Capabilities)
	item.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return &item, nil
}

func (db *SQLiteDB) CreateVisualScriptRevision(ctx context.Context, org, scriptID string, base int, item *visualscripts.Revision) error {
	tx, err := db.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var latest int
	if err := tx.QueryRowContext(ctx, `SELECT latest_revision FROM visual_scripts WHERE org_name = ? AND id = ?`, org, scriptID).Scan(&latest); errors.Is(err, sql.ErrNoRows) {
		return visualscripts.ErrNotFound
	} else if err != nil {
		return err
	}
	if latest != base {
		return visualscripts.ErrConflict
	}
	item.ScriptID, item.OrgName, item.Revision, item.CreatedAt = scriptID, org, latest+1, time.Now().UTC()
	graph, err := json.Marshal(item.Graph)
	if err != nil {
		return err
	}
	diagnostics, _ := json.Marshal(item.Diagnostics)
	capabilities, _ := json.Marshal(item.Capabilities)
	if _, err = tx.ExecContext(ctx, `INSERT INTO visual_script_revisions (script_id, org_name, revision, schema_version, graph_json, graph_hash, validation_status, diagnostics_json, capabilities_json, created_by, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, scriptID, org, item.Revision, item.SchemaVersion, string(graph), item.GraphHash, item.ValidationStatus, string(diagnostics), string(capabilities), item.CreatedBy, formatTimestamp(item.CreatedAt)); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `UPDATE visual_scripts SET latest_revision = ?, updated_by = ?, updated_at = ? WHERE org_name = ? AND id = ? AND latest_revision = ?`, item.Revision, item.CreatedBy, formatTimestamp(item.CreatedAt), org, scriptID, base)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return visualscripts.ErrConflict
	}
	return tx.Commit()
}

func (db *SQLiteDB) SetVisualScriptActiveRevision(ctx context.Context, org, scriptID string, revision *int) error {
	var value any
	if revision != nil {
		value = *revision
	}
	res, err := db.db.ExecContext(ctx, `UPDATE visual_scripts SET active_revision = ?, updated_at = ? WHERE org_name = ? AND id = ?`, value, formatTimestamp(time.Now().UTC()), org, scriptID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return visualscripts.ErrNotFound
	}
	return nil
}

func (db *SQLiteDB) SetVisualScriptDesiredState(ctx context.Context, org, scriptID, state string) error {
	res, err := db.db.ExecContext(ctx, `UPDATE visual_scripts SET desired_state = ?, updated_at = ? WHERE org_name = ? AND id = ?`, state, formatTimestamp(time.Now().UTC()), org, scriptID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return visualscripts.ErrNotFound
	}
	return nil
}

func (db *SQLiteDB) SetVisualScriptOptions(ctx context.Context, org, scriptID string, simulation, activate bool, actor int) error {
	res, err := db.db.ExecContext(ctx, `UPDATE visual_scripts SET simulation = ?, activate = ?, updated_by = ?, updated_at = ? WHERE org_name = ? AND id = ?`, simulation, activate, actor, formatTimestamp(time.Now().UTC()), org, scriptID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return visualscripts.ErrNotFound
	}
	return nil
}

func (db *SQLiteDB) SetVisualScriptBackupRevision(ctx context.Context, org, scriptID string, revision *int, actor int) error {
	var value any
	if revision != nil {
		value = *revision
	}
	res, err := db.db.ExecContext(ctx, `UPDATE visual_scripts SET backup_revision = ?, updated_by = ?, updated_at = ? WHERE org_name = ? AND id = ?`, value, actor, formatTimestamp(time.Now().UTC()), org, scriptID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return visualscripts.ErrNotFound
	}
	return nil
}

func (db *SQLiteDB) AppendVisualScriptRun(ctx context.Context, run *visualscripts.Run) error {
	trace, _ := json.Marshal(run.Trace)
	_, err := db.db.ExecContext(ctx, `INSERT INTO visual_script_runs (run_id, org_name, script_id, active_revision, trigger_node_id, instance_key, started_at, status, trace_json) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, run.RunID, run.OrgName, run.ScriptID, run.ActiveRevision, run.TriggerNodeID, run.InstanceKey, formatTimestamp(run.StartedAt), run.Status, string(trace))
	return err
}

func (db *SQLiteDB) CompleteVisualScriptRun(ctx context.Context, run *visualscripts.Run) error {
	trace, _ := json.Marshal(run.Trace)
	var completed any
	if run.CompletedAt != nil {
		completed = formatTimestamp(*run.CompletedAt)
	}
	_, err := db.db.ExecContext(ctx, `UPDATE visual_script_runs SET completed_at = ?, status = ?, duration_ms = ?, first_error_node_id = ?, message = ?, nodes_executed = ?, actions_attempted = ?, warnings = ?, dropped_traces = ?, trace_json = ? WHERE org_name = ? AND script_id = ? AND run_id = ?`, completed, run.Status, run.DurationMS, run.FirstErrorNodeID, run.Message, run.NodesExecuted, run.ActionsAttempted, run.Warnings, run.DroppedTraces, string(trace), run.OrgName, run.ScriptID, run.RunID)
	return err
}

func (db *SQLiteDB) CancelIncompleteVisualScriptRuns(ctx context.Context, completed time.Time, reason string) error {
	formatted := formatTimestamp(completed)
	_, err := db.db.ExecContext(ctx, `UPDATE visual_script_runs SET completed_at = ?, status = 'cancelled', message = ?, duration_ms = MAX(0, CAST((julianday(?) - julianday(started_at)) * 86400000 AS INTEGER)) WHERE completed_at IS NULL AND status IN ('queued', 'running')`, formatted, reason, formatted)
	return err
}

func (db *SQLiteDB) ClearVisualScriptRuns(ctx context.Context, org, scriptID string) error {
	_, err := db.db.ExecContext(ctx, `DELETE FROM visual_script_runs WHERE org_name = ? AND script_id = ?`, org, scriptID)
	return err
}

func (db *SQLiteDB) ListVisualScriptRuns(ctx context.Context, org, scriptID string, limit int) ([]visualscripts.Run, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := db.db.QueryContext(ctx, visualRunSelect+` WHERE org_name = ? AND script_id = ? ORDER BY started_at DESC LIMIT ?`, org, scriptID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []visualscripts.Run{}
	for rows.Next() {
		item, err := scanSQLiteRun(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (db *SQLiteDB) GetVisualScriptRun(ctx context.Context, org, scriptID, runID string) (*visualscripts.Run, error) {
	item, err := scanSQLiteRun(db.db.QueryRowContext(ctx, visualRunSelect+` WHERE org_name = ? AND script_id = ? AND run_id = ?`, org, scriptID, runID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return item, err
}

const visualRunSelect = `SELECT run_id, org_name, script_id, active_revision, trigger_node_id, instance_key, started_at, completed_at, status, duration_ms, first_error_node_id, message, nodes_executed, actions_attempted, warnings, dropped_traces, trace_json FROM visual_script_runs`

func scanSQLiteRun(row sqliteScanner) (*visualscripts.Run, error) {
	var item visualscripts.Run
	var started string
	var completed sql.NullString
	var trace string
	if err := row.Scan(&item.RunID, &item.OrgName, &item.ScriptID, &item.ActiveRevision, &item.TriggerNodeID, &item.InstanceKey, &started, &completed, &item.Status, &item.DurationMS, &item.FirstErrorNodeID, &item.Message, &item.NodesExecuted, &item.ActionsAttempted, &item.Warnings, &item.DroppedTraces, &trace); err != nil {
		return nil, err
	}
	item.StartedAt, _ = time.Parse(time.RFC3339Nano, started)
	if completed.Valid {
		parsed, _ := time.Parse(time.RFC3339Nano, completed.String)
		item.CompletedAt = &parsed
	}
	_ = json.Unmarshal([]byte(trace), &item.Trace)
	return &item, nil
}

var _ visualscripts.Store = (*SQLiteDB)(nil)
