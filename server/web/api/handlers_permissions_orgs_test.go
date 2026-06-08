package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/xact-iot/xact/sqldb"
	"github.com/xact-iot/xact/sqldb/sqlite"
)

func TestPermissionHandlersMergeCurrentUserRolePermissions(t *testing.T) {
	db := newWebAPITestDB(t, "permissions-merge")

	updateRolePermissions(t, db, "default", "Operator", map[string]map[string]bool{
		"nodes": {"read": true, "write": false},
		"logs":  {"read": false},
	})
	updateRolePermissions(t, db, "default", "Technician", map[string]map[string]bool{
		"nodes":   {"write": true},
		"logs":    {"read": true},
		"ignored": {"view": false},
	})

	h := NewPermissionHandlers(
		db,
		func(context.Context) ([]string, bool) { return []string{"Operator", "Technician"}, true },
		func(context.Context) (string, bool) { return "default", true },
	)

	req := httptest.NewRequest(http.MethodGet, "/permissions/me", nil)
	rr := httptest.NewRecorder()
	h.HandleGetMyPermissions(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}

	var got map[string]map[string]bool
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode merged permissions: %v", err)
	}
	want := map[string]map[string]bool{
		"nodes": {"read": true, "write": true},
		"logs":  {"read": true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merged permissions = %#v, want %#v", got, want)
	}
}

func TestPermissionHandlersTenantAndRoleValidation(t *testing.T) {
	db := newWebAPITestDB(t, "permissions-validation")
	h := NewPermissionHandlers(
		db,
		func(context.Context) ([]string, bool) { return []string{"SystemAdmin"}, true },
		func(context.Context) (string, bool) { return "", false },
	)

	req := httptest.NewRequest(http.MethodGet, "/permissions", nil)
	rr := httptest.NewRecorder()
	h.HandleListRolePermissions(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("list status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}

	h = NewPermissionHandlers(
		db,
		func(context.Context) ([]string, bool) { return []string{"SystemAdmin"}, true },
		func(context.Context) (string, bool) { return "default", true },
	)
	r := chi.NewRouter()
	r.Put("/permissions/{role}", h.HandleUpdateRolePermissions)

	req = httptest.NewRequest(http.MethodPut, "/permissions/SystemAdmin", bytes.NewBufferString("{invalid"))
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid update status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestOrgHandlersFilterAndProtectOrganisations(t *testing.T) {
	ctx := context.Background()
	db := newWebAPITestDB(t, "orgs-filter")
	createOrganisation(t, ctx, db, "alpha", "Alpha")
	createOrganisation(t, ctx, db, "beta", "Beta")

	h := NewOrgHandlers(db, nil, nil)
	h.AllowedOrgSet = func(*http.Request) (map[string]bool, bool) {
		return map[string]bool{"alpha": true}, false
	}
	h.CanAccessOrg = func(_ *http.Request, org string) bool {
		return org == "alpha"
	}
	h.IsSystemAdmin = func(*http.Request) bool { return false }

	r := chi.NewRouter()
	r.Get("/orgs", h.HandleListOrganisations)
	r.Get("/orgs/{name}", h.HandleGetOrganisation)
	r.Post("/orgs", h.HandleCreateOrganisation)
	r.Delete("/orgs/{name}", h.HandleDeleteOrganisation)

	var orgs []sqldb.Organisation
	doJSON(t, r, http.MethodGet, "/orgs", nil, http.StatusOK, &orgs)
	if len(orgs) != 1 || orgs[0].Name != "alpha" {
		t.Fatalf("visible orgs = %#v, want only alpha", orgs)
	}

	req := httptest.NewRequest(http.MethodGet, "/orgs/beta", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("restricted get status = %d, want %d", rr.Code, http.StatusNotFound)
	}

	doJSON(t, r, http.MethodPost, "/orgs", map[string]any{"name": "gamma", "displayName": "Gamma", "active": true}, http.StatusForbidden, nil)
	doJSON(t, r, http.MethodDelete, "/orgs/default", nil, http.StatusForbidden, nil)
}

func TestOrgHandlersCreateUpdateDeleteSyncHooksAndCreatorRole(t *testing.T) {
	ctx := context.Background()
	db := newWebAPITestDB(t, "orgs-hooks")
	user := &sqldb.User{
		FirstName: "Ada",
		LastName:  "Lovelace",
		LoginName: "ada",
		Email:     "ada@example.test",
		Active:    true,
	}
	hash, err := sqlite.HashPassword("password")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if err := db.CreateUser(ctx, user, hash); err != nil {
		t.Fatalf("create user: %v", err)
	}

	var synced []string
	var deleted []string
	h := NewOrgHandlers(
		db,
		func(name, displayName string, area *sqldb.OrgArea) {
			synced = append(synced, name+":"+displayName)
			if area == nil {
				t.Fatalf("sync area should be passed through")
			}
		},
		func(name string) { deleted = append(deleted, name) },
	)
	h.GetUserID = func(context.Context) (int, bool) { return user.ID, true }
	h.IsSystemAdmin = func(*http.Request) bool { return true }
	h.CanAccessOrg = func(*http.Request, string) bool { return true }

	r := chi.NewRouter()
	r.Post("/orgs", h.HandleCreateOrganisation)
	r.Put("/orgs/{name}", h.HandleUpdateOrganisation)
	r.Delete("/orgs/{name}", h.HandleDeleteOrganisation)

	area := map[string]float64{"north": 10, "south": 1, "east": 20, "west": 2}
	var created sqldb.Organisation
	doJSON(t, r, http.MethodPost, "/orgs", map[string]any{
		"name": "gamma", "displayName": "Gamma", "active": true, "area": area,
	}, http.StatusCreated, &created)
	if created.ID == 0 || created.Name != "gamma" {
		t.Fatalf("created org = %#v", created)
	}

	orgs, err := db.GetUserOrgs(ctx, user.ID)
	if err != nil {
		t.Fatalf("get user orgs: %v", err)
	}
	if !hasOrgRole(orgs, "gamma", "SystemAdmin") {
		t.Fatalf("creator org roles = %#v, want gamma SystemAdmin", orgs)
	}

	var updated sqldb.Organisation
	doJSON(t, r, http.MethodPut, "/orgs/gamma", map[string]any{
		"displayName": "Gamma Plant", "active": true, "area": area,
	}, http.StatusOK, &updated)
	doJSON(t, r, http.MethodDelete, "/orgs/gamma", nil, http.StatusNoContent, nil)

	if want := []string{"gamma:Gamma", "gamma:Gamma Plant"}; !reflect.DeepEqual(synced, want) {
		t.Fatalf("sync calls = %#v, want %#v", synced, want)
	}
	if want := []string{"gamma"}; !reflect.DeepEqual(deleted, want) {
		t.Fatalf("delete calls = %#v, want %#v", deleted, want)
	}
}

func newWebAPITestDB(t *testing.T, name string) sqldb.DB {
	t.Helper()
	ctx := context.Background()
	db, err := sqlite.NewSQLiteDB(ctx, "file:"+name+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("new sqlite db: %v", err)
	}
	t.Cleanup(db.Close)
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate sqlite db: %v", err)
	}
	return db
}

func updateRolePermissions(t *testing.T, db sqldb.DB, org, role string, ui map[string]map[string]bool) {
	t.Helper()
	rawUI, err := json.Marshal(ui)
	if err != nil {
		t.Fatalf("marshal role permissions: %v", err)
	}
	if err := db.UpdatePermissions(context.Background(), org, role, &sqldb.RolePermissions{
		UI:     rawUI,
		Server: json.RawMessage(`{}`),
	}); err != nil {
		t.Fatalf("update %s permissions: %v", role, err)
	}
}

func createOrganisation(t *testing.T, ctx context.Context, db sqldb.DB, name, displayName string) {
	t.Helper()
	if err := db.CreateOrganisation(ctx, &sqldb.Organisation{
		Name:        name,
		DisplayName: displayName,
		Active:      true,
	}); err != nil {
		t.Fatalf("create org %s: %v", name, err)
	}
}

func hasOrgRole(orgs []sqldb.UserOrg, orgName, role string) bool {
	for _, org := range orgs {
		if org.OrgName != orgName {
			continue
		}
		for _, gotRole := range org.Roles {
			if gotRole == role {
				return true
			}
		}
	}
	return false
}
