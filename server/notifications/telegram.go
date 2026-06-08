package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/xact-iot/xact/events"
)

// TelegramConfig holds the Telegram bot configuration.
type TelegramConfig struct {
	BotToken string `json:"botToken"`
}

// TelegramSender sends notifications via the Telegram Bot API.
type TelegramSender struct {
	cfg TelegramConfig
}

// NewTelegramSender creates a Telegram notifier from the given config.
func NewTelegramSender(cfg TelegramConfig) *TelegramSender {
	return &TelegramSender{cfg: cfg}
}

func (t *TelegramSender) Name() string { return "telegram" }

func (t *TelegramSender) Send(ctx context.Context, target events.NotificationTarget, subject, body string) error {
	fmt.Printf("Send telegram from telegram\n")
	if t.cfg.BotToken == "" {
		return fmt.Errorf("telegram: bot token not configured")
	}
	if target.TelegramID == "" {
		return fmt.Errorf("telegram: no chat ID for user %s", target.UserName)
	}

	text := fmt.Sprintf("*%s*\n\n%s", escapeMarkdown(subject), body)

	payload := map[string]any{
		"chat_id":    target.TelegramID,
		"text":       text,
		"parse_mode": "Markdown",
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("telegram: marshal: %w", err)
	}

	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.cfg.BotToken)

	// Use a dedicated short-lived context so the request cannot hang beyond 10s,
	// regardless of what the parent context or HTTP transport do.
	reqCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, apiURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("telegram: request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	type result struct {
		resp *http.Response
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		resp, err := http.DefaultClient.Do(req)
		ch <- result{resp, err}
	}()

	select {
	case r := <-ch:
		if r.err != nil {
			return fmt.Errorf("telegram: send: %w", r.err)
		}
		defer r.resp.Body.Close()
		if r.resp.StatusCode != http.StatusOK {
			respBody, _ := io.ReadAll(io.LimitReader(r.resp.Body, 512))
			log.Printf("events: telegram: API %d: %s", r.resp.StatusCode, respBody)
			return fmt.Errorf("telegram: API returned %d", r.resp.StatusCode)
		}
		return nil
	case <-reqCtx.Done():
		return fmt.Errorf("telegram: request timed out")
	}
}

func escapeMarkdown(s string) string {
	r := strings.NewReplacer(
		"_", "\\_",
		"[", "\\[",
		"]", "\\]",
		"(", "\\(",
		")", "\\)",
		"~", "\\~",
		"`", "\\`",
		">", "\\>",
		"#", "\\#",
		"+", "\\+",
		"-", "\\-",
		"=", "\\=",
		"|", "\\|",
		"{", "\\{",
		"}", "\\}",
		".", "\\.",
		"!", "\\!",
	).Replace(s)
	return r
}
