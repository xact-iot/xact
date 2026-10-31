package events

import (
	"encoding/json"
	"log"
	"strings"
	"time"

	natsgo "github.com/nats-io/nats.go"
)

// ApplicationEventSubject is where application services publish events:
// xact.internal.event_request.<tenant>.<application>. The manifest grants a
// service only its own application's subject, so the application token is
// trustworthy and recorded with each event.
const ApplicationEventSubject = "xact.internal.event_request."

// ApplicationEvent is what a service publishes.
type ApplicationEvent struct {
	Severity       string         `json:"severity"`
	Device         string         `json:"device"`
	Message        string         `json:"message"`
	NotificationID int            `json:"notification_id,omitempty"`
	Params         map[string]any `json:"params,omitempty"`
	Timestamp      time.Time      `json:"timestamp,omitempty"`
}

var validSeverity = map[string]Severity{"DEBUG": Debug, "INFO": Info, "WARN": Warn, "ERROR": Error, "CRITICAL": Critical}

// ServeApplicationEvents relays application events into the event log and
// notifications (XI-10). Malformed events are dropped.
func (p *Publisher) ServeApplicationEvents(nc *natsgo.Conn) (*natsgo.Subscription, error) {
	return nc.Subscribe(ApplicationEventSubject+"*.*", func(m *natsgo.Msg) {
		parts := strings.Split(m.Subject, ".")
		if len(parts) != 5 || len(m.Data) > 16<<10 {
			return
		}
		tenant, app := parts[3], parts[4]
		var ev ApplicationEvent
		if err := json.Unmarshal(m.Data, &ev); err != nil {
			log.Printf("events: application %s sent an invalid event: %v", app, err)
			return
		}
		sev, ok := validSeverity[strings.ToUpper(ev.Severity)]
		if !ok || ev.Message == "" || len(ev.Message) > 2000 || len(ev.Device) > 256 {
			return
		}
		if ev.Timestamp.IsZero() || time.Since(ev.Timestamp) > 24*time.Hour || time.Until(ev.Timestamp) > time.Minute {
			ev.Timestamp = time.Now()
		}
		params := ev.Params
		if params == nil {
			params = map[string]any{}
		}
		params["application"] = app
		entry := EventEntry{Timestamp: ev.Timestamp, OrgName: tenant, Severity: string(sev), NotificationID: ev.NotificationID,
			Device: ev.Device, Message: ev.Message, Params: params}
		if err := p.PublishEntry(entry); err != nil {
			log.Printf("events: application %s: %v", app, err)
		}
	})
}
