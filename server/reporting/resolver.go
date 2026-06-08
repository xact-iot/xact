// Package reporting handles PDF report generation for XACT.
package reporting

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/xact-iot/xact/sqldb"
)

// VariableType describes where a variable's value comes from.
type VariableType string

const (
	VariableTypeBuiltin VariableType = "builtin"
	VariableTypeRTDB    VariableType = "rtdb"
	VariableTypeSQL     VariableType = "sql"
	// VariableTypeCustom is a user-supplied parameter whose value must be
	// provided at report-generation time (preview, on-demand, scheduler).
	// The DefaultValue is used when no runtime override is supplied.
	VariableTypeCustom VariableType = "custom"
)

// Variable defines a named placeholder used in template text.
type Variable struct {
	Name  string       `json:"name"`
	Label string       `json:"label"`
	Type  VariableType `json:"type"`
	// For builtin: "now", "org_name", "org_slug", "report_name", "page_no", "page_count"
	Source string `json:"source,omitempty"`
	// For rtdb: dotted path to the tag
	Path string `json:"path,omitempty"`
	// For sql: parameterised read-only query; $1 = org_name
	Query string `json:"query,omitempty"`
	// Go time format string (for builtin "now") or fmt verb (e.g. "%.2f")
	Format string `json:"format,omitempty"`
	// For custom: suggested HTML input type ("text", "date", "datetime", "number")
	InputType string `json:"inputType,omitempty"`
	// For custom: value used when no runtime override is provided
	DefaultValue string `json:"defaultValue,omitempty"`
}

// ResolveContext carries runtime information available during variable resolution.
type ResolveContext struct {
	// OrgName is the stable organisation slug used in database paths and queries.
	OrgName string
	// OrgDisplayName is the human-friendly organisation label shown in reports.
	// When empty, built-in org_name falls back to OrgName.
	OrgDisplayName string
	ReportName     string
	// TagReader resolves an RTDB tag path to its current string value.
	// May be nil - RTDB variables will be empty strings if no reader is set.
	TagReader func(path string) (string, bool)
	// SQLQueryer executes a read-only SQL query and returns the first row's first column as string.
	// May be nil - SQL variables will be empty strings if no queryier is set.
	SQLQueryer func(ctx context.Context, query string, orgName string) (string, error)
}

// ResolveVariables evaluates all variable bindings and returns a name→value map.
func ResolveVariables(ctx context.Context, variables []Variable, rc ResolveContext) map[string]string {
	vals := make(map[string]string, len(variables))
	for _, v := range variables {
		val := resolveOne(ctx, v, rc)
		vals[v.Name] = val
	}
	return vals
}

func resolveOne(ctx context.Context, v Variable, rc ResolveContext) string {
	switch v.Type {
	case VariableTypeBuiltin:
		switch v.Source {
		case "now":
			format := v.Format
			if format == "" {
				format = "2006-01-02 15:04"
			}
			return time.Now().Format(format)
		case "org_name":
			if rc.OrgDisplayName != "" {
				return rc.OrgDisplayName
			}
			return rc.OrgName
		case "org_slug":
			return rc.OrgName
		case "report_name":
			return rc.ReportName
		case "page_no":
			return "«PAGE_NO»"
		case "page_count":
			return "«PAGE_COUNT»"
		}
	case VariableTypeRTDB:
		if rc.TagReader != nil && v.Path != "" {
			if val, ok := rc.TagReader(v.Path); ok {
				return val
			}
		}
	case VariableTypeSQL:
		if rc.SQLQueryer != nil && v.Query != "" {
			val, err := rc.SQLQueryer(ctx, v.Query, rc.OrgName)
			if err == nil {
				return val
			}
		}
	case VariableTypeCustom:
		// Runtime value is injected via the override map in resolveAndGenerate.
		// Fall back to the designer-specified default.
		return v.DefaultValue
	}
	return ""
}

// SubstituteTemplate replaces all {{name}} tokens in the template JSON with
// the resolved variable values and returns the substituted JSON.
func SubstituteTemplate(templateJSON json.RawMessage, vals map[string]string) (json.RawMessage, error) {
	s := string(templateJSON)
	for name, val := range vals {
		s = strings.ReplaceAll(s, "{{"+name+"}}", jsonEscape(val))
	}
	// Validate the result is still valid JSON
	var check any
	if err := json.Unmarshal([]byte(s), &check); err != nil {
		return nil, fmt.Errorf("substitution produced invalid JSON: %w", err)
	}
	return json.RawMessage(s), nil
}

// ParseVariables decodes the variables JSONB field from a PDFTemplate.
func ParseVariables(t *sqldb.PDFTemplate) ([]Variable, error) {
	if len(t.Variables) == 0 {
		return nil, nil
	}
	var vars []Variable
	if err := json.Unmarshal(t.Variables, &vars); err != nil {
		return nil, fmt.Errorf("parsing template variables: %w", err)
	}
	return vars, nil
}

// jsonEscape escapes a string value so it is safe inside a JSON string literal.
// Only the characters that would break a JSON string need escaping.
func jsonEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return s
}
