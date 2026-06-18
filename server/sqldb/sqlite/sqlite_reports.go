package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/xact-iot/xact/sqldb"
)

// ListPDFTemplates returns all PDF templates for an organisation.
func (db *SQLiteDB) ListPDFTemplates(ctx context.Context, org string) ([]sqldb.PDFTemplate, error) {
	rows, err := db.db.QueryContext(ctx, `
		SELECT id, org_name, name, description, template_json, variables, created_at, updated_at
		FROM pdf_templates
		WHERE org_name = ?
		ORDER BY name ASC
	`, org)
	if err != nil {
		return nil, fmt.Errorf("listing pdf templates: %w", err)
	}
	defer rows.Close()

	templates := []sqldb.PDFTemplate{}
	for rows.Next() {
		var t sqldb.PDFTemplate
		var tjStr, varsStr, createdAtStr, updatedAtStr string
		if err := rows.Scan(&t.ID, &t.OrgName, &t.Name, &t.Description,
			&tjStr, &varsStr, &createdAtStr, &updatedAtStr); err != nil {
			return nil, fmt.Errorf("scanning pdf template: %w", err)
		}
		t.TemplateJSON = json.RawMessage(tjStr)
		t.Variables = json.RawMessage(varsStr)
		t.CreatedAt = parseTimestamp(createdAtStr)
		t.UpdatedAt = parseTimestamp(updatedAtStr)
		templates = append(templates, t)
	}
	return templates, rows.Err()
}

// GetPDFTemplate returns a single PDF template by ID. Returns nil if not found.
func (db *SQLiteDB) GetPDFTemplate(ctx context.Context, org string, id string) (*sqldb.PDFTemplate, error) {
	var t sqldb.PDFTemplate
	var tjStr, varsStr, createdAtStr, updatedAtStr string
	err := db.db.QueryRowContext(ctx, `
		SELECT id, org_name, name, description, template_json, variables, created_at, updated_at
		FROM pdf_templates
		WHERE org_name = ? AND id = ?
	`, org, id).Scan(&t.ID, &t.OrgName, &t.Name, &t.Description,
		&tjStr, &varsStr, &createdAtStr, &updatedAtStr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting pdf template: %w", err)
	}
	t.TemplateJSON = json.RawMessage(tjStr)
	t.Variables = json.RawMessage(varsStr)
	t.CreatedAt = parseTimestamp(createdAtStr)
	t.UpdatedAt = parseTimestamp(updatedAtStr)
	return &t, nil
}

// CreatePDFTemplate inserts a new PDF template. Sets t.ID, t.CreatedAt, t.UpdatedAt on success.
func (db *SQLiteDB) CreatePDFTemplate(ctx context.Context, org string, t *sqldb.PDFTemplate) error {
	tj := t.TemplateJSON
	if tj == nil {
		tj = json.RawMessage("{}")
	}
	vars := t.Variables
	if vars == nil {
		vars = json.RawMessage("[]")
	}
	id := t.ID
	if id == "" {
		id = newUUID()
	}
	now := formatTimestamp(time.Now())
	_, err := db.db.ExecContext(ctx, `
		INSERT INTO pdf_templates (id, org_name, name, description, template_json, variables, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, id, org, t.Name, t.Description, string(tj), string(vars), now, now)
	if err != nil {
		return fmt.Errorf("creating pdf template: %w", err)
	}
	t.ID = id
	t.OrgName = org
	t.CreatedAt = parseTimestamp(now)
	t.UpdatedAt = parseTimestamp(now)
	return nil
}

// UpdatePDFTemplate replaces an existing PDF template by ID.
func (db *SQLiteDB) UpdatePDFTemplate(ctx context.Context, org string, id string, t *sqldb.PDFTemplate) error {
	tj := t.TemplateJSON
	if tj == nil {
		tj = json.RawMessage("{}")
	}
	vars := t.Variables
	if vars == nil {
		vars = json.RawMessage("[]")
	}
	now := formatTimestamp(time.Now())
	result, err := db.db.ExecContext(ctx, `
		UPDATE pdf_templates
		SET name = ?, description = ?, template_json = ?, variables = ?, updated_at = ?
		WHERE org_name = ? AND id = ?
	`, t.Name, t.Description, string(tj), string(vars), now, org, id)
	if err != nil {
		return fmt.Errorf("updating pdf template: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("pdf template not found")
	}
	return nil
}

// DeletePDFTemplate removes a PDF template by ID.
func (db *SQLiteDB) DeletePDFTemplate(ctx context.Context, org string, id string) error {
	_, err := db.db.ExecContext(ctx,
		"DELETE FROM pdf_templates WHERE org_name = ? AND id = ?", org, id)
	return err
}
