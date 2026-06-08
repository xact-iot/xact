package backups

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	identifierPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	columnTypePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_ ]*(\([0-9]+(\s*,\s*[0-9]+)?\))?(\[\])?$`)
)

func ValidateSchema(schema *Schema) error {
	if schema == nil || schema.Tables == nil {
		return fmt.Errorf("schema is missing")
	}
	for name, table := range schema.Tables {
		if err := ValidateTable(name, table); err != nil {
			return err
		}
	}
	return nil
}

func ValidateTable(name string, table Table) error {
	if err := validateIdentifier("table", name); err != nil {
		return err
	}
	if len(table.Columns) == 0 {
		return fmt.Errorf("table %q has no columns", name)
	}
	columns := make(map[string]bool, len(table.Columns))
	for _, column := range table.Columns {
		if err := validateIdentifier("column", column.Name); err != nil {
			return fmt.Errorf("table %q: %w", name, err)
		}
		if columns[column.Name] {
			return fmt.Errorf("table %q has duplicate column %q", name, column.Name)
		}
		columns[column.Name] = true
		if err := validateColumnType(column.Type); err != nil {
			return fmt.Errorf("table %q column %q: %w", name, column.Name, err)
		}
	}
	for _, column := range table.PrimaryKey {
		if err := validateIdentifier("primary key column", column); err != nil {
			return fmt.Errorf("table %q: %w", name, err)
		}
		if !columns[column] {
			return fmt.Errorf("table %q primary key references unknown column %q", name, column)
		}
	}
	for _, index := range table.Indexes {
		for _, column := range index.Columns {
			if err := validateIdentifier("index column", column); err != nil {
				return fmt.Errorf("table %q: %w", name, err)
			}
			if !columns[column] {
				return fmt.Errorf("table %q index references unknown column %q", name, column)
			}
		}
	}
	if ext, ok := table.Extensions["timescaledb"]; ok {
		cfg, ok := ext.(map[string]any)
		if !ok {
			return fmt.Errorf("table %q has invalid timescaledb extension metadata", name)
		}
		if hypertable, ok := cfg["hypertable"].(bool); ok && hypertable {
			timeColumn, ok := cfg["time_column"].(string)
			if !ok || !columns[timeColumn] {
				return fmt.Errorf("table %q timescaledb time_column is invalid", name)
			}
		}
	}
	for extName := range table.Extensions {
		if extName != "timescaledb" {
			return fmt.Errorf("table %q has unsupported extension %q", name, extName)
		}
	}
	return nil
}

func validateIdentifier(kind, value string) error {
	value = strings.TrimSpace(value)
	if !identifierPattern.MatchString(value) {
		return fmt.Errorf("invalid %s identifier %q", kind, value)
	}
	return nil
}

func validateColumnType(value string) error {
	value = strings.TrimSpace(value)
	if !columnTypePattern.MatchString(value) {
		return fmt.Errorf("invalid column type %q", value)
	}
	return nil
}

func quoteIdent(value string) (string, error) {
	if err := validateIdentifier("SQL", value); err != nil {
		return "", err
	}
	return `"` + value + `"`, nil
}

func quoteIdentList(kind string, values []string) ([]string, error) {
	quoted := make([]string, len(values))
	for i, value := range values {
		if err := validateIdentifier(kind, value); err != nil {
			return nil, err
		}
		quoted[i] = `"` + value + `"`
	}
	return quoted, nil
}
