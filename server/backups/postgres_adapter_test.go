package backups

import (
	"reflect"
	"strings"
	"testing"
)

func TestSplitHypertablesDropsHypertablesSeparately(t *testing.T) {
	hypertables, regular := splitHypertables(
		[]string{"system_config", "events", "device_metrics", "users"},
		map[string]string{"events": "timestamp", "device_metrics": "time"},
	)

	if want := []string{"events", "device_metrics"}; !reflect.DeepEqual(hypertables, want) {
		t.Fatalf("hypertables = %#v, want %#v", hypertables, want)
	}
	if want := []string{"system_config", "users"}; !reflect.DeepEqual(regular, want) {
		t.Fatalf("regular = %#v, want %#v", regular, want)
	}
}

func TestPostgresImportValueNormalizesGoTimestamp(t *testing.T) {
	got, err := postgresImportValue("timestamp with time zone", "2026-03-22 07:56:11.095511 -0400 AST")
	if err != nil {
		t.Fatalf("postgresImportValue: %v", err)
	}
	text, ok := got.(string)
	if !ok {
		t.Fatalf("value type = %T, want string", got)
	}
	if !strings.HasPrefix(text, "2026-03-22T07:56:11.095511-04:00") {
		t.Fatalf("normalized timestamp = %q", text)
	}
}

func TestPostgresSequenceSetvalUsesValidEmptyTableValue(t *testing.T) {
	sql := postgresSequenceSetvalSQL("'org_api_keys_id_seq'", "id", "org_api_keys")
	if strings.Contains(sql, " 0)") || strings.Contains(sql, ", 0)") {
		t.Fatalf("sequence reset SQL should not set empty sequence to 0: %s", sql)
	}
	if !strings.Contains(sql, "COALESCE((SELECT MAX(id) FROM org_api_keys), 1)") {
		t.Fatalf("sequence reset SQL missing empty-table fallback: %s", sql)
	}
}
