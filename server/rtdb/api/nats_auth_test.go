package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xact-iot/xact/rtdb/nats"
)

func natsConfigRequest() *http.Request {
	req := newAPIRequestWithClaims(http.MethodGet, "/nats", nil, &JWTClaims{TenantID: "default", UserID: "1"})
	req.Header.Set("Authorization", "Bearer test-session")
	return req
}

func TestNATSConfigReturnsOnlyCurrentSessionCredentials(t *testing.T) {
	db := newTestDB("alice", "password")
	s := NewServer(ServerConfig{}, nil, nil, nil, "test-secret", db, "")
	s.SetNATSBrowserConfig(NATSBrowserConfig{Username: "old-browser", Password: "legacy-shared-secret", NATSWSPath: "/ws"})
	token, err := s.buildJWT("1", "alice", "default", []string{"Admin"}, []string{"default"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/nats-config", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	s.Router().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	var cfg NATSBrowserConfig
	if err := json.Unmarshal(rr.Body.Bytes(), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Username != "browser" || cfg.Password != token || cfg.InboxPrefix != nats.BrowserInboxPrefix(token) {
		t.Fatal("configuration did not use the authenticated session")
	}
	if rr.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("credentials must not be cached")
	}
	for _, bearer := range []string{"", "legacy-shared-secret"} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/system/nats-config", nil)
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		rr := httptest.NewRecorder()
		s.Router().ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("invalid session returned %d", rr.Code)
		}
	}
}

func TestNATSAuthenticationChecksSessionStateAndCommandPermission(t *testing.T) {
	db := newTestDB("alice", "password")
	s := &Server{jwtSecret: []byte("test-secret"), db: db}
	token, err := s.buildJWT("1", "alice", "default", []string{"Admin"}, []string{"default"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("NATS_BROWSER_ALLOW_COMMANDS", "no")
	session, ok := s.AuthenticateNATS(context.Background(), token)
	if !ok || session.Org != "default" || !session.ReadTree || !session.ReadTags || session.SendCommands {
		t.Fatalf("incorrect session permissions: %+v", session)
	}
	t.Setenv("NATS_BROWSER_ALLOW_COMMANDS", "yes")
	session, ok = s.AuthenticateNATS(context.Background(), token)
	if !ok || !session.SendCommands {
		t.Fatal("authorised commands rejected")
	}
	readOnly, err := s.buildJWT("1", "alice", "default", []string{"Operator"}, []string{"default"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	session, ok = s.AuthenticateNATS(context.Background(), readOnly)
	if !ok || session.SendCommands || session.ReadTree || session.ReadTags {
		t.Fatal("permissions absent from DB must fail closed")
	}
	db.authVersion, db.authActive = 2, true
	if _, ok := s.AuthenticateNATS(context.Background(), token); ok {
		t.Fatal("revoked session accepted")
	}
	db.authVersion, db.authActive = 1, false
	if _, ok := s.AuthenticateNATS(context.Background(), token); ok {
		t.Fatal("disabled user accepted")
	}
	if _, ok := s.AuthenticateNATS(context.Background(), "legacy-shared-secret"); ok {
		t.Fatal("legacy shared password accepted")
	}
}
