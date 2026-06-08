// Package events provides the public events API for the XACT server.
// Events are written to the console logger and published to a NATS JetStream
// worker queue ("notifications") for database persistence.
package events

import (
	"time"
)

// Severity represents the severity level of an event.
type Severity string

const (
	Debug    Severity = "DEBUG"
	Info     Severity = "INFO"
	Warn     Severity = "WARN"
	Error    Severity = "ERROR"
	Critical Severity = "CRITICAL"
)

// EventEntry is the canonical event record - the wire format on the NATS
// stream and the type stored in the database.
type EventEntry struct {
	ID             int64          `json:"id"`
	Timestamp      time.Time      `json:"timestamp"`
	Server         string         `json:"server"`
	OrgName        string         `json:"orgName"`
	UserID         *int           `json:"userId,omitempty"`
	UserName       string         `json:"userName,omitempty"`
	Severity       string         `json:"severity"`
	NotificationID int            `json:"notificationId"`
	Device         string         `json:"device"`
	Message        string         `json:"message"`
	Params         map[string]any `json:"params,omitempty"`
}

// parseArgs converts variadic key-value args into a map.
func parseArgs(args []any) map[string]any {
	m := make(map[string]any, len(args)/2)
	for i := 0; i+1 < len(args); i += 2 {
		if k, ok := args[i].(string); ok {
			m[k] = args[i+1]
		}
	}
	return m
}
