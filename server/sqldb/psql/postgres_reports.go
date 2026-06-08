package psql

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/xact-iot/xact/sqldb"
)

// ListPDFTemplates returns all PDF templates for an organisation.
func (db *PostgresDB) ListPDFTemplates(ctx context.Context, org string) ([]sqldb.PDFTemplate, error) {
	rows, err := db.pool.Query(ctx, `
		SELECT id, org_name, name, description, template_json, variables, created_at, updated_at
		FROM pdf_templates
		WHERE org_name = $1
		ORDER BY name ASC
	`, org)
	if err != nil {
		return nil, fmt.Errorf("listing pdf templates: %w", err)
	}
	defer rows.Close()

	var templates []sqldb.PDFTemplate
	for rows.Next() {
		var t sqldb.PDFTemplate
		var tj, vars []byte
		if err := rows.Scan(&t.ID, &t.OrgName, &t.Name, &t.Description,
			&tj, &vars, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning pdf template: %w", err)
		}
		t.TemplateJSON = json.RawMessage(tj)
		t.Variables = json.RawMessage(vars)
		templates = append(templates, t)
	}
	if templates == nil {
		templates = []sqldb.PDFTemplate{}
	}
	return templates, nil
}

// GetPDFTemplate returns a single PDF template by ID. Returns nil if not found.
func (db *PostgresDB) GetPDFTemplate(ctx context.Context, org string, id string) (*sqldb.PDFTemplate, error) {
	var t sqldb.PDFTemplate
	var tj, vars []byte
	err := db.pool.QueryRow(ctx, `
		SELECT id, org_name, name, description, template_json, variables, created_at, updated_at
		FROM pdf_templates
		WHERE org_name = $1 AND id = $2
	`, org, id).Scan(&t.ID, &t.OrgName, &t.Name, &t.Description,
		&tj, &vars, &t.CreatedAt, &t.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting pdf template: %w", err)
	}
	t.TemplateJSON = json.RawMessage(tj)
	t.Variables = json.RawMessage(vars)
	return &t, nil
}

// CreatePDFTemplate inserts a new PDF template and sets t.ID, t.CreatedAt, t.UpdatedAt.
func (db *PostgresDB) CreatePDFTemplate(ctx context.Context, org string, t *sqldb.PDFTemplate) error {
	tj := t.TemplateJSON
	if tj == nil {
		tj = json.RawMessage("{}")
	}
	vars := t.Variables
	if vars == nil {
		vars = json.RawMessage("[]")
	}
	return db.pool.QueryRow(ctx, `
		INSERT INTO pdf_templates (org_name, name, description, template_json, variables)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at, updated_at
	`, org, t.Name, t.Description, []byte(tj), []byte(vars)).
		Scan(&t.ID, &t.CreatedAt, &t.UpdatedAt)
}

// UpdatePDFTemplate replaces an existing PDF template by ID.
func (db *PostgresDB) UpdatePDFTemplate(ctx context.Context, org string, id string, t *sqldb.PDFTemplate) error {
	tj := t.TemplateJSON
	if tj == nil {
		tj = json.RawMessage("{}")
	}
	vars := t.Variables
	if vars == nil {
		vars = json.RawMessage("[]")
	}
	tag, err := db.pool.Exec(ctx, `
		UPDATE pdf_templates
		SET name = $3, description = $4, template_json = $5, variables = $6, updated_at = NOW()
		WHERE org_name = $1 AND id = $2
	`, org, id, t.Name, t.Description, []byte(tj), []byte(vars))
	if err != nil {
		return fmt.Errorf("updating pdf template: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("pdf template not found")
	}
	return nil
}

// DeletePDFTemplate removes a PDF template by ID.
func (db *PostgresDB) DeletePDFTemplate(ctx context.Context, org string, id string) error {
	_, err := db.pool.Exec(ctx, `
		DELETE FROM pdf_templates WHERE org_name = $1 AND id = $2
	`, org, id)
	return err
}
