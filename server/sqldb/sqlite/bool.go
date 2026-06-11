package sqlite

import (
	"database/sql/driver"
	"fmt"
	"strconv"
	"strings"
)

type sqliteBool struct {
	Bool bool
}

func (b *sqliteBool) Scan(value any) error {
	if value == nil {
		b.Bool = false
		return nil
	}
	switch v := value.(type) {
	case bool:
		b.Bool = v
	case int64:
		b.Bool = v != 0
	case int:
		b.Bool = v != 0
	case float64:
		b.Bool = v != 0
	case []byte:
		return b.scanString(string(v))
	case string:
		return b.scanString(v)
	default:
		return fmt.Errorf("cannot scan %T as sqlite bool", value)
	}
	return nil
}

func (b *sqliteBool) scanString(value string) error {
	parsed, err := parseSQLiteBool(value)
	if err != nil {
		return err
	}
	b.Bool = parsed
	return nil
}

func (b sqliteBool) Value() (driver.Value, error) {
	if b.Bool {
		return int64(1), nil
	}
	return int64(0), nil
}

func parseSQLiteBool(value string) (bool, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "1", "t", "true", "y", "yes", "on":
		return true, nil
	case "", "0", "f", "false", "n", "no", "off":
		return false, nil
	}
	if n, err := strconv.ParseFloat(normalized, 64); err == nil {
		return n != 0, nil
	}
	return false, fmt.Errorf("cannot parse %q as sqlite bool", value)
}
