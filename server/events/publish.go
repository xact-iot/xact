package events

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	natsgo "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/xact-iot/xact/logging"
)

// StreamName is the NATS JetStream worker queue stream for notifications.
const StreamName = "notifications"

// subjectPrefix is prepended to every published event subject.
const subjectPrefix = "xact.internal.worker.notifications."

// SubjectPrefix returns the NATS subject prefix for the notifications stream.
func SubjectPrefix() string { return subjectPrefix }

// Publisher is the events API. It writes events to the console logger and
// publishes them to the NATS notifications stream.
type Publisher struct {
	serverName string
	js         jetstream.JetStream
	console    *logging.Logger
}

// Init creates a Publisher and ensures the NATS notifications stream exists.
func Init(nc *natsgo.Conn, console *logging.Logger) (*Publisher, error) {
	serverName, _ := os.Hostname()
	if env := os.Getenv("SERVER_NAME"); env != "" {
		serverName = env
	}

	js, err := jetstream.New(nc)
	if err != nil {
		return nil, fmt.Errorf("events: create JetStream: %w", err)
	}
	_, err = js.CreateOrUpdateStream(context.Background(), jetstream.StreamConfig{
		Name:              StreamName,
		Subjects:          []string{subjectPrefix + ">"},
		Retention:         jetstream.WorkQueuePolicy,
		MaxAge:            24 * time.Hour,
		Storage:           jetstream.MemoryStorage,
		MaxMsgsPerSubject: 1000,
	})
	if err != nil {
		return nil, fmt.Errorf("events: create stream: %w", err)
	}
	return &Publisher{serverName: serverName, js: js, console: console}, nil
}

// PublishEvent creates an EventEntry and writes it to the console logger and
// the NATS notifications stream.
//
// De-duplicating publishes must be done before calling PublishEvent.
func PublishEvent(
	p *Publisher,
	timestamp time.Time,
	orgName string,
	device string,
	userID int,
	severity Severity,
	notificationID int,
	message string,
	params ...any,
) {
	if p == nil {
		return
	}

	entry := EventEntry{
		Timestamp:      timestamp,
		Server:         p.serverName,
		OrgName:        orgName,
		Severity:       string(severity),
		NotificationID: notificationID,
		Device:         device,
		Message:        message,
	}
	if userID != 0 {
		id := userID
		entry.UserID = &id
	}
	if ps := parseArgs(params); len(ps) > 0 {
		entry.Params = ps
	}

	// Write to console logger
	if p.console != nil {
		p.console.Log(string(severity), device, message, params...)
	}

	// Publish to NATS stream
	data, err := json.Marshal(entry)
	if err != nil {
		log.Printf("events: marshal: %v", err)
		return
	}
	if _, err := p.js.Publish(context.Background(), subjectPrefix+string(severity), data); err != nil {
		log.Printf("events: publish: %v", err)
	}
}

// PublishEntry writes an already-assembled event to the console logger and
// publishes it to the notifications stream.
func (p *Publisher) PublishEntry(entry EventEntry) error {
	if p == nil {
		return nil
	}
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}
	if entry.Server == "" {
		entry.Server = p.serverName
	}
	if p.console != nil {
		var args []any
		for k, v := range entry.Params {
			args = append(args, k, v)
		}
		p.console.Log(entry.Severity, entry.Device, entry.Message, args...)
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("events: marshal: %w", err)
	}
	if _, err := p.js.Publish(context.Background(), subjectPrefix+entry.Severity, data); err != nil {
		return fmt.Errorf("events: publish: %w", err)
	}
	return nil
}
