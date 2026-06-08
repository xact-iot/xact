package events

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/smtp"
	"strings"
	"time"

	natsgo "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Notifier sends a notification message to a single recipient.
type Notifier interface {
	// Name returns the channel name (e.g. "email", "telegram").
	Name() string
	// Send delivers a notification. Implementations must be safe for concurrent use.
	Send(ctx context.Context, recipient NotificationTarget, subject, body string) error
}

// EmailConfig holds SMTP connection settings.
type EmailConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	From     string `json:"from"`
	UseTLS   bool   `json:"useTls"`
}

// TelegramConfig holds the Telegram bot configuration.
type TelegramConfig struct {
	BotToken string `json:"botToken"`
}

// NotificationTarget represents a user who should receive a notification.
type NotificationTarget struct {
	UserName   string
	Email      string
	TelegramID string
	EmailOn    bool
	TelegramOn bool
}

// NotificationOptions is the JSON structure stored in users.notification_options.
type NotificationOptions struct {
	EmailEnabled    bool   `json:"emailEnabled"`
	TelegramEnabled bool   `json:"telegramEnabled"`
	TelegramID      string `json:"telegramId"`
}

// RecipientRecord is a minimal user record returned by the RecipientResolver.
type RecipientRecord struct {
	ID                  int
	FirstName           string
	LastName            string
	Email               string
	NotificationOptions json.RawMessage
}

// RecipientResolver looks up notification recipients for a given profile ID.
type RecipientResolver interface {
	GetNotificationRecipients(ctx context.Context, org string, profileID int) ([]RecipientRecord, error)
}

// NotificationHandler subscribes to the NATS notifications stream and:
//  1. Passes every event to the EventWriter for database persistence.
//  2. If the event has a NotificationID, looks up the profile, assembles
//     the recipient list, and dispatches via configured channels.
type NotificationHandler struct {
	writer    *EventWriter
	resolver  RecipientResolver
	notifiers []Notifier
	cc        jetstream.ConsumeContext
}

// NewNotificationHandler creates a handler that subscribes to the notifications
// stream and dispatches events to the EventWriter and notification channels.
func NewNotificationHandler(nc *natsgo.Conn, writer *EventWriter, resolver RecipientResolver, notifiers []Notifier) (*NotificationHandler, error) {
	js, err := jetstream.New(nc)
	if err != nil {
		return nil, fmt.Errorf("events: handler: jetstream: %w", err)
	}
	stream, err := js.Stream(context.Background(), StreamName)
	if err != nil {
		return nil, fmt.Errorf("events: handler: stream: %w", err)
	}
	consumer, err := stream.CreateOrUpdateConsumer(context.Background(), jetstream.ConsumerConfig{
		Durable:       "notifications-handler",
		FilterSubject: subjectPrefix + ">",
		AckPolicy:     jetstream.AckExplicitPolicy,
		DeliverPolicy: jetstream.DeliverAllPolicy,
	})
	if err != nil {
		return nil, fmt.Errorf("events: handler: consumer: %w", err)
	}

	h := &NotificationHandler{
		writer:    writer,
		resolver:  resolver,
		notifiers: notifiers,
	}

	cc, err := consumer.Consume(h.handle)
	if err != nil {
		return nil, fmt.Errorf("events: handler: consume: %w", err)
	}
	h.cc = cc
	return h, nil
}

// Stop unsubscribes from NATS.
func (h *NotificationHandler) Stop() {
	h.cc.Stop()
}

// NewEmailSenderFromConfig creates an email notifier from the given config.
func NewEmailSenderFromConfig(cfg EmailConfig) Notifier {
	return &emailSender{cfg: cfg}
}

// NewTelegramSenderFromConfig creates a Telegram notifier from the given config.
func NewTelegramSenderFromConfig(cfg TelegramConfig) Notifier {
	return &telegramSender{cfg: cfg}
}

// emailSender sends notifications via SMTP.
type emailSender struct {
	cfg EmailConfig
}

func (e *emailSender) Name() string { return "email" }

func (e *emailSender) Send(ctx context.Context, target NotificationTarget, subject, body string) error {
	if e.cfg.Host == "" {
		return fmt.Errorf("email: SMTP host not configured")
	}
	addr := net.JoinHostPort(e.cfg.Host, fmt.Sprintf("%d", e.cfg.Port))
	msg := buildEmailMIME(e.cfg.From, target.Email, subject, body)
	var auth smtp.Auth
	if e.cfg.Username != "" {
		auth = smtp.PlainAuth("", e.cfg.Username, e.cfg.Password, e.cfg.Host)
	}
	if e.cfg.UseTLS {
		return sendEmailTLS(addr, e.cfg.Host, auth, e.cfg.From, target.Email, msg)
	}
	return smtp.SendMail(addr, auth, e.cfg.From, []string{target.Email}, msg)
}

func buildEmailMIME(from, to, subject, body string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
	b.WriteString("\r\n")
	b.WriteString(body)
	return []byte(b.String())
}

func sendEmailTLS(addr, host string, auth smtp.Auth, from, to string, msg []byte) error {
	conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: host})
	if err != nil {
		return fmt.Errorf("email: TLS dial: %w", err)
	}
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("email: SMTP client: %w", err)
	}
	defer client.Close()
	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("email: auth: %w", err)
		}
	}
	if err := client.Mail(from); err != nil {
		return fmt.Errorf("email: MAIL FROM: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("email: RCPT TO: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("email: DATA: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("email: write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("email: close: %w", err)
	}
	return client.Quit()
}

// telegramSender sends notifications via the Telegram Bot API.
type telegramSender struct {
	cfg TelegramConfig
}

func (t *telegramSender) Name() string { return "telegram" }

func (t *telegramSender) Send(ctx context.Context, target NotificationTarget, subject, body string) error {
	fmt.Printf("Send telegram from handler\n")
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
	return strings.NewReplacer(
		"_", "\\_", "[", "\\[", "]", "\\]", "(", "\\(",
		")", "\\)", "~", "\\~", "`", "\\`", ">", "\\>",
		"#", "\\#", "+", "\\+", "-", "\\-", "=", "\\=",
		"|", "\\|", "{", "\\{", "}", "\\}", ".", "\\.", "!", "\\!",
	).Replace(s)
}

// ReloadNotifiers rebuilds the notifiers slice from the given channel config.
func (h *NotificationHandler) ReloadNotifiers(emailCfg EmailConfig, telegramCfg TelegramConfig) {
	var newNotifiers []Notifier
	if emailCfg.Host != "" {
		newNotifiers = append(newNotifiers, NewEmailSenderFromConfig(emailCfg))
	}
	if telegramCfg.BotToken != "" {
		newNotifiers = append(newNotifiers, NewTelegramSenderFromConfig(telegramCfg))
	}
	h.notifiers = newNotifiers
}

func (h *NotificationHandler) handle(msg jetstream.Msg) {
	var entry EventEntry
	if err := json.Unmarshal(msg.Data(), &entry); err != nil {
		log.Printf("events: handler: decode: %v", err)
		msg.Nak()
		return
	}
	// 1. Pass to EventWriter for DB persistence.
	h.writer.WriteEvent(entry)

	// Ack immediately so NATS does not redeliver while dispatch is in flight.
	msg.Ack()

	// 2. If there's a notification profile, dispatch in a goroutine so the
	//    consumer callback is never blocked by slow network calls.
	if entry.NotificationID != 0 && h.resolver != nil {
		go h.dispatch(entry)
	}
}

func (h *NotificationHandler) dispatch(entry EventEntry) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	orgName := entry.OrgName
	if orgName == "" {
		orgName = "default"
	}

	recipients, err := h.resolver.GetNotificationRecipients(ctx, orgName, entry.NotificationID)
	if err != nil {
		log.Printf("events: handler: recipients: %v", err)
		return
	}
	if len(recipients) == 0 {
		return
	}

	subject := fmt.Sprintf("%s %s", severityEmoji(entry.Severity), entry.Device)
	body := formatNotificationBody(entry)

	var notifiedNames []string
	for _, r := range recipients {
		target := recordToTarget(r)

		for _, n := range h.notifiers {
			switch n.Name() {
			case "email":
				if target.EmailOn && target.Email != "" {
					if err := n.Send(ctx, target, subject, body); err != nil {
						log.Printf("events: handler: email to %s: %v", target.UserName, err)
					}
				}
			case "telegram":
				if target.TelegramOn && target.TelegramID != "" {
					if err := n.Send(ctx, target, subject, body); err != nil {
						log.Printf("events: handler: telegram to %s: %v", target.UserName, err)
					}
				}
			}
		}
		notifiedNames = append(notifiedNames, target.UserName)
	}

	// Log that notifications were sent.
	if len(notifiedNames) > 0 {
		h.writer.WriteEvent(EventEntry{
			Timestamp: time.Now(),
			Server:    entry.Server,
			OrgName:   entry.OrgName,
			Severity:  string(Info),
			Device:    entry.Device,
			Message:   fmt.Sprintf("Notification sent: %s", entry.Message),
			Params:    map[string]any{"recipients": strings.Join(notifiedNames, ", ")},
		})
	}
}

func recordToTarget(r RecipientRecord) NotificationTarget {
	t := NotificationTarget{
		UserName: strings.TrimSpace(r.FirstName + " " + r.LastName),
		Email:    r.Email,
	}
	if t.UserName == "" {
		t.UserName = r.Email
	}

	var opts NotificationOptions
	if len(r.NotificationOptions) > 0 {
		_ = json.Unmarshal(r.NotificationOptions, &opts)
	}
	t.EmailOn = opts.EmailEnabled
	t.TelegramOn = opts.TelegramEnabled
	t.TelegramID = opts.TelegramID
	return t
}

func severityEmoji(s string) string {
	switch s {
	case string(Info):
		return "ℹ️"
	case string(Warn):
		return "⚠️"
	case string(Error):
		return "❌"
	case string(Critical):
		return "🚨"
	default:
		return "🔵" // DEBUG or unknown
	}
}

func formatNotificationBody(e EventEntry) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Severity: %s %s\r\n", severityEmoji(e.Severity), e.Severity)
	fmt.Fprintf(&b, "Time: %s\r\n", e.Timestamp.Format("2006-01-02 15:04:05"))
	if e.OrgName != "" {
		fmt.Fprintf(&b, "Organisation: %s\r\n", e.OrgName)
	}
	if e.Device != "" {
		fmt.Fprintf(&b, "Device: %s\r\n", e.Device)
	}
	fmt.Fprintf(&b, "Message: %s\r\n", e.Message)
	if len(e.Params) > 0 {
		for k, v := range e.Params {
			fmt.Fprintf(&b, "  %s: %v\r\n", k, v)
		}
	}
	return b.String()
}
