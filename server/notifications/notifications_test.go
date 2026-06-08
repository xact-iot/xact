package notifications

import (
	"context"
	"strings"
	"testing"

	"github.com/xact-iot/xact/events"
)

func TestEmailSenderValidationNameAndMIME(t *testing.T) {
	sender := NewEmailSender(EmailConfig{})
	if sender.Name() != "email" {
		t.Fatalf("Name = %q", sender.Name())
	}
	err := sender.Send(context.Background(), events.NotificationTarget{Email: "to@example.test"}, "Subject", "Body")
	if err == nil || !strings.Contains(err.Error(), "SMTP host not configured") {
		t.Fatalf("Send without host error = %v", err)
	}

	msg := string(buildMIME("from@example.test", "to@example.test", "Subject", "Body"))
	for _, part := range []string{
		"From: from@example.test\r\n",
		"To: to@example.test\r\n",
		"Subject: Subject\r\n",
		"Content-Type: text/plain",
		"\r\n\r\nBody",
	} {
		if !strings.Contains(msg, part) {
			t.Fatalf("MIME message missing %q: %q", part, msg)
		}
	}
}

func TestTelegramSenderValidationNameAndMarkdownEscaping(t *testing.T) {
	sender := NewTelegramSender(TelegramConfig{})
	if sender.Name() != "telegram" {
		t.Fatalf("Name = %q", sender.Name())
	}
	err := sender.Send(context.Background(), events.NotificationTarget{UserName: "Ada"}, "Subject", "Body")
	if err == nil || !strings.Contains(err.Error(), "bot token not configured") {
		t.Fatalf("Send without token error = %v", err)
	}

	sender = NewTelegramSender(TelegramConfig{BotToken: "token"})
	err = sender.Send(context.Background(), events.NotificationTarget{UserName: "Ada"}, "Subject", "Body")
	if err == nil || !strings.Contains(err.Error(), "no chat ID") {
		t.Fatalf("Send without chat error = %v", err)
	}

	got := escapeMarkdown("_[]()~`>#+-=|{}.!")
	want := "\\_\\[\\]\\(\\)\\~\\`\\>\\#\\+\\-\\=\\|\\{\\}\\.\\!"
	if got != want {
		t.Fatalf("escapeMarkdown = %q, want %q", got, want)
	}
}
