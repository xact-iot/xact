package events

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

type recordingInserter struct {
	mu      sync.Mutex
	batches [][]EventEntry
	err     error
}

func (r *recordingInserter) InsertEventEntries(_ context.Context, entries []EventEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := append([]EventEntry(nil), entries...)
	r.batches = append(r.batches, cp)
	return r.err
}

type recordingPurger struct {
	before time.Time
	calls  int
}

func (r *recordingPurger) PurgeEventsBefore(_ context.Context, before time.Time) error {
	r.before = before
	r.calls++
	return nil
}

func TestParseArgsIgnoresDanglingAndNonStringKeys(t *testing.T) {
	got := parseArgs([]any{"a", 1, 2, "ignored", "b"})
	if len(got) != 1 || got["a"] != 1 {
		t.Fatalf("parseArgs = %#v", got)
	}
}

func TestEventWriterBuffersFlushesAndSkipsDebug(t *testing.T) {
	inserter := &recordingInserter{}
	w := NewEventWriter(inserter)
	w.WriteEvent(EventEntry{Severity: string(Debug), Message: "skip"})
	w.WriteEvent(EventEntry{Severity: string(Info), Message: "keep"})
	w.flush()
	w.flush()

	if len(inserter.batches) != 1 {
		t.Fatalf("batches = %#v", inserter.batches)
	}
	if len(inserter.batches[0]) != 1 || inserter.batches[0][0].Message != "keep" {
		t.Fatalf("flushed entries = %#v", inserter.batches[0])
	}
}

func TestEventWriterFlushesAtBufferSizeAndPurger(t *testing.T) {
	inserter := &recordingInserter{}
	w := NewEventWriter(inserter)
	for i := 0; i < dbBufferSize; i++ {
		w.WriteEvent(EventEntry{Severity: string(Info), Message: "bulk"})
	}
	if len(inserter.batches) != 1 || len(inserter.batches[0]) != dbBufferSize {
		t.Fatalf("flush at capacity batches = %#v", inserter.batches)
	}

	p := &recordingPurger{}
	pg := newPurger(p, 7)
	pg.purge()
	if p.calls != 1 || time.Since(p.before) < 6*24*time.Hour {
		t.Fatalf("purger calls=%d before=%v", p.calls, p.before)
	}
}

type fakeResolver struct {
	recipients []RecipientRecord
	org        string
	profileID  int
}

func (f *fakeResolver) GetNotificationRecipients(_ context.Context, org string, profileID int) ([]RecipientRecord, error) {
	f.org = org
	f.profileID = profileID
	return f.recipients, nil
}

type recordingNotifier struct {
	name    string
	targets []NotificationTarget
	fail    bool
}

func (n *recordingNotifier) Name() string { return n.name }
func (n *recordingNotifier) Send(_ context.Context, target NotificationTarget, _, _ string) error {
	n.targets = append(n.targets, target)
	if n.fail {
		return context.DeadlineExceeded
	}
	return nil
}

func TestNotificationDispatchTargetsEnabledChannelsAndLogsResult(t *testing.T) {
	inserter := &recordingInserter{}
	writer := NewEventWriter(inserter)
	resolver := &fakeResolver{recipients: []RecipientRecord{
		{FirstName: "Ada", LastName: "Lovelace", Email: "ada@example.test", NotificationOptions: json.RawMessage(`{"emailEnabled":true}`)},
		{Email: "bot@example.test", NotificationOptions: json.RawMessage(`{"telegramEnabled":true,"telegramId":"123"}`)},
		{ID: 42, FirstName: "Mobile", LastName: "Operator", NotificationOptions: json.RawMessage(`{"mobileEnabled":true}`)},
		{ID: 43, FirstName: "Android", LastName: "Operator", NotificationOptions: json.RawMessage(`{"mobileEnabled":true,"fcmEnabled":true,"fcmToken":"device-token","fcmProjectId":"firebase-project"}`)},
		{ID: 44, FirstName: "REST", LastName: "API", Email: "api@example.test", NotificationOptions: json.RawMessage(`{"emailEnabled":false,"telegramEnabled":false,"mobileEnabled":false,"fcmEnabled":false}`)},
	}}
	email := &recordingNotifier{name: "email"}
	telegram := &recordingNotifier{name: "telegram"}
	mobile := &recordingNotifier{name: "mobile"}
	fcm := &recordingNotifier{name: "fcm"}
	h := &NotificationHandler{writer: writer, resolver: resolver, notifiers: []Notifier{email, telegram, mobile, fcm}}

	h.dispatch(EventEntry{Server: "srv", NotificationID: 9, Severity: string(Warn), Device: "pump", Message: "hot"})

	if resolver.org != "default" || resolver.profileID != 9 {
		t.Fatalf("resolver org/profile = %q/%d", resolver.org, resolver.profileID)
	}
	if len(email.targets) != 1 || email.targets[0].UserName != "Ada Lovelace" {
		t.Fatalf("email targets = %#v", email.targets)
	}
	if len(telegram.targets) != 1 || telegram.targets[0].TelegramID != "123" || telegram.targets[0].UserName != "bot@example.test" {
		t.Fatalf("telegram targets = %#v", telegram.targets)
	}
	if len(mobile.targets) != 1 || mobile.targets[0].UserID != 42 || mobile.targets[0].Device != "pump" || mobile.targets[0].OrgName != "default" {
		t.Fatalf("mobile targets = %#v", mobile.targets)
	}
	if len(fcm.targets) != 1 || fcm.targets[0].UserID != 43 || fcm.targets[0].FCMToken != "device-token" || fcm.targets[0].FCMProjectID != "firebase-project" {
		t.Fatalf("FCM targets = %#v", fcm.targets)
	}
	writer.flush()
	if len(inserter.batches) != 1 || inserter.batches[0][0].Severity != string(Info) {
		t.Fatalf("notification log batch = %#v", inserter.batches)
	}
	recipients, _ := inserter.batches[0][0].Params["recipients"].(string)
	if strings.Contains(recipients, "REST API") {
		t.Fatalf("disabled recipient included in notification audit: %q", recipients)
	}
}

func TestNotificationDispatchFallsBackToMobileWhenFCMFails(t *testing.T) {
	inserter := &recordingInserter{}
	writer := NewEventWriter(inserter)
	resolver := &fakeResolver{recipients: []RecipientRecord{{
		ID: 43, FirstName: "Android", LastName: "Operator",
		NotificationOptions: json.RawMessage(`{"mobileEnabled":true,"fcmEnabled":true,"fcmToken":"device-token"}`),
	}}}
	fcm := &recordingNotifier{name: "fcm", fail: true}
	mobile := &recordingNotifier{name: "mobile"}
	h := &NotificationHandler{writer: writer, resolver: resolver, notifiers: []Notifier{mobile, fcm}}

	h.dispatch(EventEntry{NotificationID: 1, Severity: string(Warn), Device: "AQ-B-0001", Message: "AQI high"})

	if len(fcm.targets) != 1 || len(mobile.targets) != 1 {
		t.Fatalf("FCM/mobile fallback targets = %d/%d", len(fcm.targets), len(mobile.targets))
	}
	writer.flush()
	if len(inserter.batches) != 1 || inserter.batches[0][0].Params["recipients"] != "Android Operator" {
		t.Fatalf("fallback notification audit = %#v", inserter.batches)
	}
}

func TestNotificationFormattingHelpers(t *testing.T) {
	if recordToTarget(RecipientRecord{Email: "x@example.test"}).UserName != "x@example.test" {
		t.Fatal("recordToTarget should fall back to email")
	}
	if severityEmoji(string(Critical)) == severityEmoji("unknown") {
		t.Fatal("severityEmoji should distinguish critical and unknown")
	}
	body := formatNotificationBody(EventEntry{
		Timestamp: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		OrgName:   "default",
		Severity:  string(Error),
		Device:    "pump",
		Message:   "failed",
		Params:    map[string]any{"code": 7},
	})
	for _, part := range []string{"Severity:", "Organisation: default", "Device: pump", "Message: failed", "code: 7"} {
		if !strings.Contains(body, part) {
			t.Fatalf("body missing %q: %q", part, body)
		}
	}
}

func TestNotifierFactoriesAndReload(t *testing.T) {
	if NewEmailSenderFromConfig(EmailConfig{}).Name() != "email" || NewTelegramSenderFromConfig(TelegramConfig{}).Name() != "telegram" {
		t.Fatal("notifier factory names mismatch")
	}
	h := &NotificationHandler{}
	h.ReloadNotifiers(EmailConfig{Host: "smtp.example.test"}, TelegramConfig{BotToken: "token"}, &recordingNotifier{name: "fcm"})
	if len(h.notifiers) != 3 {
		t.Fatalf("notifiers = %#v", h.notifiers)
	}
}
