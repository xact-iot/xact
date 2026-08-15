package visualscripts

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

const maxExactJSONInteger = int64(9007199254740991)

func timestampMillis(value any) (int64, error) {
	switch item := value.(type) {
	case time.Time:
		return item.UnixMilli(), nil
	case string:
		text := strings.TrimSpace(item)
		if text == "" {
			return 0, errors.New("timestamp is required")
		}
		if parsed, err := time.Parse(time.RFC3339Nano, text); err == nil {
			return parsed.UnixMilli(), nil
		}
		integer, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return 0, errors.New("timestamp must be ISO-8601 or Unix epoch milliseconds")
		}
		return validateEpochMillis(integer)
	case json.Number:
		integer, err := strconv.ParseInt(string(item), 10, 64)
		if err != nil {
			return 0, errors.New("timestamp must be an integer number of milliseconds")
		}
		return validateEpochMillis(integer)
	default:
		number, ok := asFloat(value)
		if !ok || math.IsNaN(number) || math.IsInf(number, 0) || math.Trunc(number) != number || number > float64(maxExactJSONInteger) || number < -float64(maxExactJSONInteger) {
			return 0, errors.New("timestamp must be an integer number of milliseconds")
		}
		return validateEpochMillis(int64(number))
	}
}

func validateEpochMillis(value int64) (int64, error) {
	absolute := value
	if absolute < 0 {
		absolute = -absolute
	}
	if value != 0 && absolute < 100_000_000_000 {
		return 0, errors.New("timestamp appears to be Unix seconds; use epoch milliseconds")
	}
	if absolute > maxExactJSONInteger {
		return 0, errors.New("timestamp exceeds the exact JSON integer range")
	}
	return value, nil
}

func configuredOrMessageTime(msg Message, config map[string]any, source, fieldKey, configuredKey string, now func() time.Time) (int64, error) {
	switch source {
	case "now":
		return now().UnixMilli(), nil
	case "field":
		field := stringConfig(config, fieldKey)
		if field == "" {
			return 0, errors.New("time field is required")
		}
		value, ok := getField(msg.Fields, field)
		if !ok {
			return 0, fmt.Errorf("field %q does not exist", field)
		}
		return timestampMillis(value)
	case "configured":
		return timestampMillis(config[configuredKey])
	case "value":
		return timestampMillis(msg.Value)
	default:
		return 0, fmt.Errorf("unsupported time source %q", source)
	}
}

func compareTimeValues(left, right int64, operator string) (bool, error) {
	switch operator {
	case "before":
		return left < right, nil
	case "before-or-equal":
		return left <= right, nil
	case "equal":
		return left == right, nil
	case "after-or-equal":
		return left >= right, nil
	case "after":
		return left > right, nil
	default:
		return false, fmt.Errorf("unsupported time comparison %q", operator)
	}
}

func elapsedInUnit(milliseconds int64, unit string) (any, error) {
	switch unit {
	case "milliseconds", "":
		return milliseconds, nil
	case "seconds":
		return float64(milliseconds) / 1000, nil
	case "minutes":
		return float64(milliseconds) / float64(time.Minute/time.Millisecond), nil
	case "hours":
		return float64(milliseconds) / float64(time.Hour/time.Millisecond), nil
	default:
		return nil, fmt.Errorf("unsupported time unit %q", unit)
	}
}

func debugFormattedTimes(msg Message, config map[string]any) map[string]string {
	display := stringConfig(config, "timeDisplay")
	if display == "" || display == "raw" {
		return nil
	}
	fields, ok := config["timeFields"].([]any)
	if !ok || len(fields) == 0 {
		return nil
	}
	formatted := make(map[string]string)
	for _, configured := range fields {
		field, ok := configured.(string)
		if !ok {
			continue
		}
		var value any
		if field == "$value" {
			value = msg.Value
		} else {
			value, ok = getField(msg.Fields, field)
			if !ok {
				continue
			}
		}
		milliseconds, err := timestampMillis(value)
		if err != nil {
			continue
		}
		instant := time.UnixMilli(milliseconds)
		if display == "utc" {
			instant = instant.UTC()
		} else {
			instant = instant.Local()
		}
		formatted[field] = instant.Format(time.RFC3339Nano)
	}
	if len(formatted) == 0 {
		return nil
	}
	return formatted
}
