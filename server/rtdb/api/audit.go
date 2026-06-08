package api

import (
	"context"
	"log"
	"time"

	"github.com/xact-iot/xact/events"
)

func (s *Server) auditSecurityEvent(ctx context.Context, orgName string, userID int, severity events.Severity, device, message string, params map[string]any) {
	if orgName == "" {
		orgName = "default"
	}
	entry := events.EventEntry{
		Timestamp: time.Now(),
		OrgName:   orgName,
		Severity:  string(severity),
		Device:    device,
		Message:   message,
		Params:    params,
	}
	if userID > 0 {
		entry.UserID = &userID
	}

	if s.eventPublisher != nil {
		if err := s.eventPublisher.PublishEntry(entry); err != nil {
			log.Printf("audit: publish security event: %v", err)
		}
		return
	}
	if s.db != nil {
		if err := s.db.InsertEventEntries(ctx, []events.EventEntry{entry}); err != nil {
			log.Printf("audit: insert security event: %v", err)
		}
	}
}
