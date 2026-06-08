package backups

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"
)

func Backup(ctx context.Context, db Adapter, w io.Writer) error {
	gz := gzip.NewWriter(w)
	defer gz.Close()

	tw := tar.NewWriter(gz)
	defer tw.Close()

	schema, err := db.ExportSchema(ctx)
	if err != nil {
		return err
	}

	if err := writeJSON(tw, "schema.json", schema); err != nil {
		return err
	}

	tables, err := db.ListTables(ctx)
	if err != nil {
		return err
	}

	for _, table := range tables {
		if skipBackupTableData(table) {
			continue
		}

		buf := &bytes.Buffer{}

		if err := db.ExportTable(ctx, table, buf); err != nil {
			return err
		}

		path := "tables/" + table + ".csv"

		if err := writeFile(tw, path, buf.Bytes()); err != nil {
			return err
		}
	}

	return nil
}

func Restore(ctx context.Context, db Adapter, r io.Reader) error {
	return restore(ctx, db, r, nil)
}

func RestoreReplacing(ctx context.Context, db Adapter, r io.Reader, clearTarget func(context.Context) error) error {
	if clearTarget == nil {
		return fmt.Errorf("restore replacement requires a target clear function")
	}
	return restore(ctx, db, r, clearTarget)
}

func restore(ctx context.Context, db Adapter, r io.Reader, clearTarget func(context.Context) error) error {

	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)

	var schema Schema
	tableData := map[string][]byte{}
	seenSchema := false

	for {

		hdr, err := tr.Next()

		if err == io.EOF {
			break
		}

		if err != nil {
			return err
		}

		data, err := io.ReadAll(tr)
		if err != nil {
			return err
		}

		name := path.Clean(hdr.Name)
		if strings.HasPrefix(name, "../") || strings.HasPrefix(name, "/") {
			return fmt.Errorf("backup contains unsafe path %q", hdr.Name)
		}
		if hdr.FileInfo().IsDir() {
			continue
		}

		if name == "schema.json" {
			if err := json.Unmarshal(data, &schema); err != nil {
				return fmt.Errorf("parsing schema.json: %w", err)
			}
			seenSchema = true
			continue
		}

		if strings.HasPrefix(name, "tables/") {
			if path.Dir(name) != "tables" || !strings.HasSuffix(name, ".csv") {
				return fmt.Errorf("backup contains unexpected table entry %q", hdr.Name)
			}

			base := path.Base(name)
			table := strings.TrimSuffix(base, ".csv")
			if err := validateIdentifier("table", table); err != nil {
				return err
			}
			if _, exists := tableData[table]; exists {
				return fmt.Errorf("backup contains duplicate table data for %q", table)
			}

			tableData[table] = data
			continue
		}

		return fmt.Errorf("backup contains unexpected entry %q", hdr.Name)
	}

	if !seenSchema {
		return fmt.Errorf("backup missing schema.json")
	}
	if err := ValidateSchema(&schema); err != nil {
		return fmt.Errorf("validating schema: %w", err)
	}
	for name := range tableData {
		if skipBackupTableData(name) {
			delete(tableData, name)
			continue
		}
		if _, ok := schema.Tables[name]; !ok {
			return fmt.Errorf("backup contains data for table %q not present in schema", name)
		}
	}

	if clearTarget != nil {
		if err := clearTarget(ctx); err != nil {
			return fmt.Errorf("clearing target database: %w", err)
		}
	}

	for name, table := range schema.Tables {

		if err := db.CreateTable(ctx, name, table); err != nil {
			return err
		}
	}

	for name, data := range tableData {

		if err := db.ImportTable(ctx, name, schema.Tables[name], bytes.NewReader(data)); err != nil {
			return err
		}
	}

	for name, table := range schema.Tables {

		if err := db.FinalizeTable(ctx, name, table); err != nil {
			return err
		}
	}

	return nil
}

func skipBackupTableData(table string) bool {
	return table == "org_api_keys"
}

func WriteCSV(rows *sql.Rows, w io.Writer) error {

	cols, err := rows.Columns()
	if err != nil {
		return err
	}

	writer := csv.NewWriter(w)

	if err := writer.Write(cols); err != nil {
		return err
	}

	values := make([]interface{}, len(cols))
	ptrs := make([]interface{}, len(cols))

	for i := range values {
		ptrs[i] = &values[i]
	}

	for rows.Next() {

		if err := rows.Scan(ptrs...); err != nil {
			return err
		}

		record := make([]string, len(cols))

		for i, v := range values {

			record[i] = csvValueString(v)
		}

		if err := writer.Write(record); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	writer.Flush()

	return writer.Error()
}

func csvValueString(v any) string {
	switch value := v.(type) {
	case nil:
		return ""
	case []byte:
		return string(value)
	case sql.RawBytes:
		return string(value)
	default:
		return fmt.Sprint(value)
	}
}

func writeJSON(tw *tar.Writer, name string, v any) error {

	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}

	return writeFile(tw, name, data)
}

func writeFile(tw *tar.Writer, name string, data []byte) error {

	hdr := &tar.Header{
		Name: name,
		Mode: 0600,
		Size: int64(len(data)),
	}

	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}

	_, err := tw.Write(data)

	return err
}
