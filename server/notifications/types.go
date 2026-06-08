package notifications

import (
	"context"
	"encoding/json"
)

type NotificationTarget struct {
	UserName   string
	Email      string
	TelegramID string
	EmailOn    bool
	TelegramOn bool
}

type RecipientRecord struct {
	ID                  int
	FirstName           string
	LastName            string
	Email               string
	NotificationOptions json.RawMessage
}

type RecipientResolver interface {
	GetNotificationRecipients(ctx context.Context, org string, profileID int) ([]RecipientRecord, error)
}