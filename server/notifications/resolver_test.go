package notifications

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/xact-iot/xact/sqldb"
	"github.com/xact-iot/xact/sqldb/sqlite"
)

func TestChannelConfigRoundTripAndDBResolver(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.NewSQLiteDB(ctx, filepath.Join(t.TempDir(), "xact.db"))
	if err != nil {
		t.Fatalf("NewSQLiteDB: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	empty, err := LoadChannelConfig(ctx, db, "default")
	if err != nil {
		t.Fatalf("Load empty config: %v", err)
	}
	if empty.Email.Host != "" || empty.Telegram.BotToken != "" || empty.FCM.ServiceAccountJSON != "" {
		t.Fatalf("empty config = %#v", empty)
	}

	cfg := ChannelConfig{
		Email:    EmailConfig{Host: "smtp.example.test", Port: 2525, From: "from@example.test"},
		Telegram: TelegramConfig{BotToken: "token"},
		FCM:      FCMConfig{ServiceAccountJSON: `{"project_id":"project"}`},
	}
	if err := SaveChannelConfig(ctx, db, "default", cfg); err != nil {
		t.Fatalf("SaveChannelConfig: %v", err)
	}
	got, err := LoadChannelConfig(ctx, db, "default")
	if err != nil {
		t.Fatalf("LoadChannelConfig: %v", err)
	}
	if got.Email.Host != cfg.Email.Host || got.Telegram.BotToken != cfg.Telegram.BotToken || got.FCM.ServiceAccountJSON != cfg.FCM.ServiceAccountJSON {
		t.Fatalf("config = %#v", got)
	}
	if err := SaveFirebaseAndroidConfig(ctx, db, `{"project_info":{"project_id":"project"}}`); err != nil {
		t.Fatalf("SaveFirebaseAndroidConfig: %v", err)
	}
	androidConfig, err := LoadFirebaseAndroidConfig(ctx, db)
	if err != nil || androidConfig != `{"project_info":{"project_id":"project"}}` {
		t.Fatalf("LoadFirebaseAndroidConfig = %q, %v", androidConfig, err)
	}

	hash, _ := sqlite.HashPassword("pw")
	user := &sqldb.User{
		FirstName:           "Notify",
		LastName:            "Target",
		LoginName:           "notify-target",
		Email:               "notify-target@example.test",
		Active:              true,
		NotificationOptions: json.RawMessage(`{"emailEnabled":true}`),
	}
	if err := db.CreateUser(ctx, user, hash); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := db.AssignUserToOrg(ctx, user.ID, "default", []string{"Operator"}); err != nil {
		t.Fatalf("AssignUserToOrg: %v", err)
	}
	profile := &sqldb.NotificationProfile{Name: "Ops", Roles: []string{"Operator"}}
	if err := db.CreateNotificationProfile(ctx, "default", profile); err != nil {
		t.Fatalf("CreateNotificationProfile: %v", err)
	}
	recipients, err := (&DBResolver{DB: db}).GetNotificationRecipients(ctx, "default", profile.ID)
	if err != nil {
		t.Fatalf("GetNotificationRecipients: %v", err)
	}
	if len(recipients) != 1 || recipients[0].Email != user.Email || recipients[0].FirstName != "Notify" {
		t.Fatalf("recipients = %#v", recipients)
	}
}
