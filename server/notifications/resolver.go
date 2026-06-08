package notifications

import (
	"context"
	"encoding/json"

	"github.com/xact-iot/xact/events"
	"github.com/xact-iot/xact/sqldb"
)

// ChannelConfig holds the combined notification channel configuration.
type ChannelConfig struct {
	Email    EmailConfig    `json:"email"`
	Telegram TelegramConfig `json:"telegram"`
}

// ConfigName is the system_config key used to store channel settings.
const ConfigName = "notification-channels"

// DBResolver adapts sqldb.DB to the events.RecipientResolver interface.
type DBResolver struct {
	DB sqldb.DB
}

func (r *DBResolver) GetNotificationRecipients(ctx context.Context, org string, profileID int) ([]events.RecipientRecord, error) {
	recipients, err := r.DB.GetNotificationRecipients(ctx, org, profileID)
	if err != nil {
		return nil, err
	}

	result := make([]events.RecipientRecord, len(recipients))
	for i, rec := range recipients {
		result[i] = events.RecipientRecord{
			ID:                  rec.ID,
			FirstName:           rec.FirstName,
			LastName:            rec.LastName,
			Email:               rec.Email,
			NotificationOptions: rec.NotificationOptions,
		}
	}
	return result, nil
}

// LoadChannelConfig loads the notification channel configuration from the database.
func LoadChannelConfig(ctx context.Context, db sqldb.DB, org string) (ChannelConfig, error) {
	var cfg ChannelConfig
	data, err := db.LoadConfig(ctx, org, ConfigName)
	if err != nil {
		return cfg, err
	}
	if data == nil {
		return cfg, nil
	}
	err = json.Unmarshal(data, &cfg)
	return cfg, err
}

// SaveChannelConfig saves the notification channel configuration to the database.
func SaveChannelConfig(ctx context.Context, db sqldb.DB, org string, cfg ChannelConfig) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return db.SaveConfig(ctx, org, ConfigName, data)
}
