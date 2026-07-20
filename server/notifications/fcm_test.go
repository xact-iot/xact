package notifications

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/xact-iot/xact/events"
)

func TestFCMSenderValidation(t *testing.T) {
	sender := NewFCMSender(FCMConfig{})
	if sender.Name() != "fcm" {
		t.Fatalf("Name = %q", sender.Name())
	}
	if err := sender.Send(context.Background(), events.NotificationTarget{}, "subject", "body"); err == nil || !strings.Contains(err.Error(), "no registration token") {
		t.Fatalf("missing token error = %v", err)
	}
	err := sender.Send(context.Background(), events.NotificationTarget{FCMToken: "token"}, "subject", "body")
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("missing config error = %v", err)
	}
}

func TestParseFirebaseClientConfig(t *testing.T) {
	document := `{
		"project_info":{"project_number":"123456","project_id":"xact-server"},
		"client":[
			{"client_info":{"mobilesdk_app_id":"wrong","android_client_info":{"package_name":"example.wrong"}},"api_key":[{"current_key":"wrong"}]},
			{"client_info":{"mobilesdk_app_id":"1:123456:android:abc","android_client_info":{"package_name":"com.xact.iot.mobile"}},"api_key":[{"current_key":"AIza-public"}]}
		]
	}`
	cfg, err := ParseFirebaseClientConfig(document)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Configured || cfg.ProjectID != "xact-server" || cfg.AppID != "1:123456:android:abc" || cfg.APIKey != "AIza-public" || cfg.MessagingSenderID != "123456" {
		t.Fatalf("config = %#v", cfg)
	}
	if _, err := ParseFirebaseClientConfig(`{"project_info":{},"client":[]}`); err == nil || !strings.Contains(err.Error(), AndroidPackageName) {
		t.Fatalf("missing package error = %v", err)
	}
	if empty, err := ParseFirebaseClientConfig(""); err != nil || empty.Configured {
		t.Fatalf("empty config = %#v, %v", empty, err)
	}
}

func TestFCMSenderGetsOAuthTokenAndSendsMessage(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keyBytes, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	privateKey := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyBytes}))
	credential, _ := json.Marshal(map[string]string{
		"project_id": "test-project", "client_email": "sender@test-project.iam.gserviceaccount.com", "private_key": privateKey,
	})

	var tokenCalls atomic.Int32
	var messageCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			tokenCalls.Add(1)
			body, _ := io.ReadAll(r.Body)
			values, _ := url.ParseQuery(string(body))
			if values.Get("grant_type") == "" || len(strings.Split(values.Get("assertion"), ".")) != 3 {
				t.Fatalf("invalid OAuth form: %s", body)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"access-token","expires_in":3600}`))
		case "/v1/projects/test-project/messages:send":
			messageCalls.Add(1)
			if r.Header.Get("Authorization") != "Bearer access-token" {
				t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
			}
			var payload struct {
				Message struct {
					Token        string            `json:"token"`
					Notification map[string]string `json:"notification"`
					Data         map[string]string `json:"data"`
				} `json:"message"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload.Message.Token != "device-token" || payload.Message.Notification["title"] != "Alert" || payload.Message.Data["device"] != "pump" {
				t.Fatalf("payload = %#v", payload)
			}
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	sender := NewFCMSender(FCMConfig{ServiceAccountJSON: string(credential)})
	sender.client = server.Client()
	sender.tokenURL = server.URL + "/token"
	sender.fcmBaseURL = server.URL
	target := events.NotificationTarget{UserName: "Android User", FCMToken: "device-token", Device: "pump", OrgName: "default"}
	if err := sender.Send(context.Background(), target, "Alert", "Pump is hot"); err != nil {
		t.Fatal(err)
	}
	if err := sender.Send(context.Background(), target, "Alert", "Pump is hot"); err != nil {
		t.Fatal(err)
	}
	if tokenCalls.Load() != 1 || messageCalls.Load() != 2 {
		t.Fatalf("token calls=%d message calls=%d", tokenCalls.Load(), messageCalls.Load())
	}
	target.FCMProjectID = "different-project"
	if err := sender.Send(context.Background(), target, "Alert", "Body"); err == nil || !strings.Contains(err.Error(), "belongs to project") {
		t.Fatalf("project mismatch error = %v", err)
	}
}
