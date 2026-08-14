package psql

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/xact-iot/xact/visualscripts"
)

type pgScanner interface{ Scan(...any) error }

func scanPGScript(row pgScanner) (*visualscripts.Script, error) {
	var item visualscripts.Script
	if err := row.Scan(&item.ID, &item.OrgName, &item.Name, &item.Description, &item.DesiredState, &item.LatestRevision, &item.ActiveRevision, &item.BackupRevision, &item.Simulation, &item.Activate, &item.CreatedBy, &item.UpdatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	item.HasBackup = item.BackupRevision != nil
	item.OutOfDate = item.LatestRevision > 0 && (item.ActiveRevision == nil || item.LatestRevision > *item.ActiveRevision)
	return &item, nil
}

const pgScriptSelect = `SELECT id::text, org_name, name, description, desired_state, latest_revision, active_revision, backup_revision, simulation, activate, created_by, updated_by, created_at, updated_at FROM visual_scripts`

func (db *PostgresDB) ListVisualScripts(ctx context.Context, org string) ([]visualscripts.Script, error) {
	rows, err := db.pool.Query(ctx, pgScriptSelect+` WHERE org_name = $1 ORDER BY name`, org)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []visualscripts.Script{}
	for rows.Next() {
		item, err := scanPGScript(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}
func (db *PostgresDB) GetVisualScript(ctx context.Context, org, id string) (*visualscripts.Script, error) {
	item, err := scanPGScript(db.pool.QueryRow(ctx, pgScriptSelect+` WHERE org_name = $1 AND id = $2`, org, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return item, err
}
func (db *PostgresDB) ListActivatedVisualScripts(ctx context.Context) ([]visualscripts.Script, error) {
	rows, err := db.pool.Query(ctx, pgScriptSelect+` WHERE activate = TRUE ORDER BY org_name, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []visualscripts.Script{}
	for rows.Next() {
		item, err := scanPGScript(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}
func (db *PostgresDB) CreateVisualScript(ctx context.Context, org string, item *visualscripts.Script) error {
	item.ID = newUUID()
	item.OrgName, item.DesiredState = org, "stopped"
	item.UpdatedBy = item.CreatedBy
	return db.pool.QueryRow(ctx, `INSERT INTO visual_scripts (id, org_name, name, description, created_by, updated_by) VALUES ($1, $2, $3, $4, $5, $5) RETURNING created_at, updated_at`, item.ID, org, item.Name, item.Description, item.CreatedBy).Scan(&item.CreatedAt, &item.UpdatedAt)
}
func (db *PostgresDB) UpdateVisualScript(ctx context.Context, org, id, name, description string, actor int) error {
	tag, err := db.pool.Exec(ctx, `UPDATE visual_scripts SET name = $1, description = $2, updated_by = $3, updated_at = NOW() WHERE org_name = $4 AND id = $5`, name, description, actor, org, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return visualscripts.ErrNotFound
	}
	return nil
}
func (db *PostgresDB) DeleteVisualScript(ctx context.Context, org, id string) error {
	tag, err := db.pool.Exec(ctx, `DELETE FROM visual_scripts WHERE org_name = $1 AND id = $2`, org, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return visualscripts.ErrNotFound
	}
	return nil
}

const pgRevisionSelect = `SELECT script_id::text, org_name, revision, schema_version, graph_json, graph_hash, validation_status, diagnostics_json, capabilities_json, created_by, created_at FROM visual_script_revisions`

func scanPGRevision(row pgScanner) (*visualscripts.Revision, error) {
	var item visualscripts.Revision
	var graph, diagnostics, capabilities []byte
	if err := row.Scan(&item.ScriptID, &item.OrgName, &item.Revision, &item.SchemaVersion, &graph, &item.GraphHash, &item.ValidationStatus, &diagnostics, &capabilities, &item.CreatedBy, &item.CreatedAt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(graph, &item.Graph); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(diagnostics, &item.Diagnostics)
	_ = json.Unmarshal(capabilities, &item.Capabilities)
	return &item, nil
}
func (db *PostgresDB) ListVisualScriptRevisions(ctx context.Context, org, scriptID string) ([]visualscripts.Revision, error) {
	rows, err := db.pool.Query(ctx, pgRevisionSelect+` WHERE org_name = $1 AND script_id = $2 ORDER BY revision DESC`, org, scriptID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []visualscripts.Revision{}
	for rows.Next() {
		item, err := scanPGRevision(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}
func (db *PostgresDB) GetVisualScriptRevision(ctx context.Context, org, scriptID string, revision int) (*visualscripts.Revision, error) {
	item, err := scanPGRevision(db.pool.QueryRow(ctx, pgRevisionSelect+` WHERE org_name = $1 AND script_id = $2 AND revision = $3`, org, scriptID, revision))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return item, err
}
func (db *PostgresDB) CreateVisualScriptRevision(ctx context.Context, org, scriptID string, base int, item *visualscripts.Revision) error {
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var latest int
	if err := tx.QueryRow(ctx, `SELECT latest_revision FROM visual_scripts WHERE org_name = $1 AND id = $2 FOR UPDATE`, org, scriptID).Scan(&latest); errors.Is(err, pgx.ErrNoRows) {
		return visualscripts.ErrNotFound
	} else if err != nil {
		return err
	}
	if latest != base {
		return visualscripts.ErrConflict
	}
	item.ScriptID, item.OrgName, item.Revision = scriptID, org, latest+1
	graph, err := json.Marshal(item.Graph)
	if err != nil {
		return err
	}
	diagnostics, _ := json.Marshal(item.Diagnostics)
	capabilities, _ := json.Marshal(item.Capabilities)
	if err = tx.QueryRow(ctx, `INSERT INTO visual_script_revisions (script_id, org_name, revision, schema_version, graph_json, graph_hash, validation_status, diagnostics_json, capabilities_json, created_by) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING created_at`, scriptID, org, item.Revision, item.SchemaVersion, graph, item.GraphHash, item.ValidationStatus, diagnostics, capabilities, item.CreatedBy).Scan(&item.CreatedAt); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE visual_scripts SET latest_revision = $1, updated_by = $2, updated_at = NOW() WHERE org_name = $3 AND id = $4 AND latest_revision = $5`, item.Revision, item.CreatedBy, org, scriptID, base)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return visualscripts.ErrConflict
	}
	return tx.Commit(ctx)
}
func (db *PostgresDB) SetVisualScriptActiveRevision(ctx context.Context, org, scriptID string, revision *int) error {
	tag, err := db.pool.Exec(ctx, `UPDATE visual_scripts SET active_revision=$1, updated_at=NOW() WHERE org_name=$2 AND id=$3`, revision, org, scriptID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return visualscripts.ErrNotFound
	}
	return nil
}
func (db *PostgresDB) SetVisualScriptDesiredState(ctx context.Context, org, scriptID, state string) error {
	tag, err := db.pool.Exec(ctx, `UPDATE visual_scripts SET desired_state=$1, updated_at=NOW() WHERE org_name=$2 AND id=$3`, state, org, scriptID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return visualscripts.ErrNotFound
	}
	return nil
}

func (db *PostgresDB) SetVisualScriptOptions(ctx context.Context, org, scriptID string, simulation, activate bool, actor int) error {
	tag, err := db.pool.Exec(ctx, `UPDATE visual_scripts SET simulation=$1,activate=$2,updated_by=$3,updated_at=NOW() WHERE org_name=$4 AND id=$5`, simulation, activate, actor, org, scriptID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return visualscripts.ErrNotFound
	}
	return nil
}

func (db *PostgresDB) SetVisualScriptBackupRevision(ctx context.Context, org, scriptID string, revision *int, actor int) error {
	tag, err := db.pool.Exec(ctx, `UPDATE visual_scripts SET backup_revision=$1,updated_by=$2,updated_at=NOW() WHERE org_name=$3 AND id=$4`, revision, actor, org, scriptID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return visualscripts.ErrNotFound
	}
	return nil
}

func (db *PostgresDB) AppendVisualScriptRun(ctx context.Context, run *visualscripts.Run) error {
	trace, _ := json.Marshal(run.Trace)
	_, err := db.pool.Exec(ctx, `INSERT INTO visual_script_runs (run_id,org_name,script_id,active_revision,trigger_node_id,instance_key,started_at,status,trace_json) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`, run.RunID, run.OrgName, run.ScriptID, run.ActiveRevision, run.TriggerNodeID, run.InstanceKey, run.StartedAt, run.Status, trace)
	return err
}
func (db *PostgresDB) CompleteVisualScriptRun(ctx context.Context, run *visualscripts.Run) error {
	trace, _ := json.Marshal(run.Trace)
	_, err := db.pool.Exec(ctx, `UPDATE visual_script_runs SET completed_at=$1,status=$2,duration_ms=$3,first_error_node_id=$4,message=$5,nodes_executed=$6,actions_attempted=$7,warnings=$8,dropped_traces=$9,trace_json=$10 WHERE org_name=$11 AND script_id=$12 AND run_id=$13`, run.CompletedAt, run.Status, run.DurationMS, run.FirstErrorNodeID, run.Message, run.NodesExecuted, run.ActionsAttempted, run.Warnings, run.DroppedTraces, trace, run.OrgName, run.ScriptID, run.RunID)
	return err
}

func (db *PostgresDB) CancelIncompleteVisualScriptRuns(ctx context.Context, completed time.Time, reason string) error {
	_, err := db.pool.Exec(ctx, `UPDATE visual_script_runs SET completed_at=$1,status='cancelled',message=$2,duration_ms=GREATEST(0,EXTRACT(EPOCH FROM ($1-started_at))*1000)::BIGINT WHERE completed_at IS NULL AND status IN ('queued','running')`, completed, reason)
	return err
}

func (db *PostgresDB) ClearVisualScriptRuns(ctx context.Context, org, scriptID string) error {
	_, err := db.pool.Exec(ctx, `DELETE FROM visual_script_runs WHERE org_name=$1 AND script_id=$2`, org, scriptID)
	return err
}

const pgRunSelect = `SELECT run_id,org_name,script_id::text,active_revision,trigger_node_id,instance_key,started_at,completed_at,status,duration_ms,first_error_node_id,message,nodes_executed,actions_attempted,warnings,dropped_traces,trace_json FROM visual_script_runs`

func scanPGRun(row pgScanner) (*visualscripts.Run, error) {
	var item visualscripts.Run
	var trace []byte
	if err := row.Scan(&item.RunID, &item.OrgName, &item.ScriptID, &item.ActiveRevision, &item.TriggerNodeID, &item.InstanceKey, &item.StartedAt, &item.CompletedAt, &item.Status, &item.DurationMS, &item.FirstErrorNodeID, &item.Message, &item.NodesExecuted, &item.ActionsAttempted, &item.Warnings, &item.DroppedTraces, &trace); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(trace, &item.Trace)
	return &item, nil
}
func (db *PostgresDB) ListVisualScriptRuns(ctx context.Context, org, scriptID string, limit int) ([]visualscripts.Run, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := db.pool.Query(ctx, pgRunSelect+` WHERE org_name=$1 AND script_id=$2 ORDER BY started_at DESC LIMIT $3`, org, scriptID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []visualscripts.Run{}
	for rows.Next() {
		item, err := scanPGRun(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}
func (db *PostgresDB) GetVisualScriptRun(ctx context.Context, org, scriptID, runID string) (*visualscripts.Run, error) {
	item, err := scanPGRun(db.pool.QueryRow(ctx, pgRunSelect+` WHERE org_name=$1 AND script_id=$2 AND run_id=$3`, org, scriptID, runID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return item, err
}

var _ visualscripts.Store = (*PostgresDB)(nil)
