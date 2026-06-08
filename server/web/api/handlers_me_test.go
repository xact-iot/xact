package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/xact-iot/xact/sqldb"
	"github.com/xact-iot/xact/sqldb/sqlite"
)

func TestMeHandlersGetAndUpdateCurrentUser(t *testing.T) {
	ctx := context.Background()
	db := newWebAPITestDB(t, "me-profile")
	user := createWebAPIUser(t, ctx, db, "sam", "old-password")

	h := NewMeHandlers(db, func(context.Context) (int, bool) { return user.ID, true })
	r := chi.NewRouter()
	r.Get("/me", h.HandleGetMe)
	r.Put("/me", h.HandleUpdateMe)

	var got sqldb.User
	doJSON(t, r, http.MethodGet, "/me", nil, http.StatusOK, &got)
	if got.LoginName != "sam" || got.Email != "sam@example.test" {
		t.Fatalf("get me = %#v", got)
	}

	var updated sqldb.User
	doJSON(t, r, http.MethodPut, "/me", map[string]any{
		"firstName":           "Samantha",
		"lastName":            "Carter",
		"email":               "sam.carter@example.test",
		"notificationOptions": map[string]any{"email": true},
	}, http.StatusOK, &updated)
	if updated.FirstName != "Samantha" || updated.LastName != "Carter" || updated.Email != "sam.carter@example.test" {
		t.Fatalf("updated me = %#v", updated)
	}
	var notificationOptions map[string]bool
	if err := json.Unmarshal(updated.NotificationOptions, &notificationOptions); err != nil {
		t.Fatalf("decode notification options: %v", err)
	}
	if !notificationOptions["email"] {
		t.Fatalf("notification options = %#v, want email true", notificationOptions)
	}
}

func TestMeHandlersUnauthorizedAndMissingUser(t *testing.T) {
	db := newWebAPITestDB(t, "me-errors")
	h := NewMeHandlers(db, func(context.Context) (int, bool) { return 0, false })

	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	rr := httptest.NewRecorder()
	h.HandleGetMe(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized get status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}

	h = NewMeHandlers(db, func(context.Context) (int, bool) { return 999, true })
	req = httptest.NewRequest(http.MethodPut, "/me", strings.NewReader(`{"firstName":"Ghost"}`))
	rr = httptest.NewRecorder()
	h.HandleUpdateMe(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("missing update status = %d, want %d", rr.Code, http.StatusNotFound)
	}
}

func TestMeHandlersChangePassword(t *testing.T) {
	ctx := context.Background()
	db := newWebAPITestDB(t, "me-password")
	user := createWebAPIUser(t, ctx, db, "lee", "old-password")

	h := NewMeHandlers(db, func(context.Context) (int, bool) { return user.ID, true })
	r := chi.NewRouter()
	r.Post("/me/password", h.HandleChangePassword)

	doJSON(t, r, http.MethodPost, "/me/password", map[string]any{
		"oldPassword": "wrong",
		"newPassword": "new-password",
	}, http.StatusUnauthorized, nil)

	doJSON(t, r, http.MethodPost, "/me/password", map[string]any{
		"oldPassword": "old-password",
		"newPassword": "new-password",
	}, http.StatusNoContent, nil)

	_, hash, err := db.GetUserByLogin(ctx, "lee")
	if err != nil {
		t.Fatalf("get user by login: %v", err)
	}
	if !sqlite.CheckPassword(hash, "new-password") {
		t.Fatalf("stored password hash does not match new password")
	}
}

func createWebAPIUser(t *testing.T, ctx context.Context, db sqldb.DB, login, password string) *sqldb.User {
	t.Helper()
	hash, err := sqlite.HashPassword(password)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	user := &sqldb.User{
		FirstName: "Test",
		LastName:  "User",
		LoginName: login,
		Email:     login + "@example.test",
		Active:    true,
	}
	if err := db.CreateUser(ctx, user, hash); err != nil {
		t.Fatalf("create user: %v", err)
	}
	return user
}
