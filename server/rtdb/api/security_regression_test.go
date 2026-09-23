package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/xact-iot/xact/rtdb/tree"
	"github.com/xact-iot/xact/sqldb"
	"github.com/xact-iot/xact/sqldb/psql"
	"github.com/xact-iot/xact/sqldb/sqlite"
)

const auditSetupToken = "test-only-operator-setup-secret-32-bytes"

func securityTestDB(t *testing.T, unset bool) *sqlite.SQLiteDB {
	t.Helper()
	password := "test-bootstrap-password"
	if unset {
		password = ""
	}
	t.Setenv("XACT_BOOTSTRAP_ADMIN_PASSWORD", password)
	t.Setenv("XACT_BOOTSTRAP_ADMIN_PASSWORD_FILE", filepath.Join(t.TempDir(), "absent"))
	db, err := sqlite.NewSQLiteDB(context.Background(), filepath.Join(t.TempDir(), "security.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return db.(*sqlite.SQLiteDB)
}

func securityRequest(s *Server, method, path, bearer string, body any) *httptest.ResponseRecorder {
	data, _ := json.Marshal(body)
	r := httptest.NewRequest(method, path, bytes.NewReader(data))
	if bearer != "" {
		r.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	s.Router().ServeHTTP(w, r)
	return w
}

func securityUser(t *testing.T, db *sqlite.SQLiteDB) *sqldb.User {
	t.Helper()
	u := &sqldb.User{LoginName: "tenant-admin", Email: "audit@example.invalid", Active: true}
	hash, err := psql.HashPassword("test-user-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateUser(context.Background(), u, hash); err != nil {
		t.Fatal(err)
	}
	if err := db.AssignUserToOrg(context.Background(), u.ID, "default", []string{"Admin"}); err != nil {
		t.Fatal(err)
	}
	return u
}

func securityLogin(t *testing.T, s *Server, username, password string) string {
	t.Helper()
	w := securityRequest(s, "POST", "/login", "", map[string]string{"username": username, "password": password})
	if w.Code != 200 {
		t.Fatalf("login: %d %s", w.Code, w.Body.String())
	}
	var login LoginResponse
	if err := json.Unmarshal(w.Body.Bytes(), &login); err != nil {
		t.Fatal(err)
	}
	return login.Token
}

func TestAgentTokenPrivilegeCeilingHTTP(t *testing.T) {
	ctx := context.Background()
	db := securityTestDB(t, false)
	owner := securityUser(t, db)
	if err := db.CreateOrganisation(ctx, &sqldb.Organisation{Name: "beta", DisplayName: "Beta"}); err != nil {
		t.Fatal(err)
	}
	if err := db.AssignUserToOrg(ctx, owner.ID, "beta", []string{"Viewer"}); err != nil {
		t.Fatal(err)
	}
	s := NewServer(ServerConfig{}, tree.NewTreeWithOperations(nil), nil, nil, "test-secret", db, "")
	bearer := securityLogin(t, s, "tenant-admin", "test-user-password")
	admin, _, err := db.GetUserByLogin(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	w := securityRequest(s, "POST", "/api/v1/agent-tokens/", bearer, map[string]any{"name": "escalate", "userId": admin.ID})
	if w.Code != 403 {
		t.Fatalf("elevated create = %d: %s", w.Code, w.Body.String())
	}
	elevated, err := db.CreateAgentToken(ctx, "default", admin.ID, "system", []string{"SystemAdmin"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	w = securityRequest(s, "GET", fmt.Sprintf("/api/v1/agent-tokens/%d", elevated.ID), bearer, nil)
	if w.Code != 403 || bytes.Contains(w.Body.Bytes(), []byte(elevated.Token)) {
		t.Fatalf("elevated retrieval = %d", w.Code)
	}
	w = securityRequest(s, "POST", "/api/v1/agent-tokens/", bearer, map[string]string{"name": "own"})
	if w.Code != 201 {
		t.Fatalf("personal create = %d: %s", w.Code, w.Body.String())
	}
	var own sqldb.AgentToken
	if err := json.Unmarshal(w.Body.Bytes(), &own); err != nil {
		t.Fatal(err)
	}
	w = securityRequest(s, "GET", fmt.Sprintf("/api/v1/agent-tokens/%d", own.ID), bearer, nil)
	if w.Code != 200 {
		t.Fatalf("personal retrieval = %d", w.Code)
	}
	for _, request := range []struct{ method, path string }{{"GET", "/api/v1/me/"}, {"PUT", "/api/v1/me/"}, {"POST", "/api/v1/me/change-password"}} {
		w = securityRequest(s, request.method, request.path, own.Token, map[string]string{"email": "attacker@example.invalid"})
		if w.Code != 401 {
			t.Fatalf("agent accessed personal account endpoint %s: %d", request.path, w.Code)
		}
	}
	w = securityRequest(s, "GET", "/api/v1/auth/my-orgs", own.Token, nil)
	var orgs MyOrgsResponse
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &orgs) != nil || len(orgs.Orgs) != 1 || orgs.Orgs[0].Name != "default" {
		t.Fatalf("agent escaped its org scope: %d %s", w.Code, w.Body.String())
	}
	w = securityRequest(s, "POST", "/api/v1/auth/switch-org", own.Token, map[string]string{"org": "default"})
	if w.Code != 403 {
		t.Fatalf("agent minted user session: %d", w.Code)
	}
	w = securityRequest(s, "POST", "/api/v1/organisations/", own.Token, map[string]string{"name": "escalated"})
	if w.Code != 403 {
		t.Fatalf("agent gained SystemAdmin: %d", w.Code)
	}
	systemBearer := securityLogin(t, s, "admin", "test-bootstrap-password")
	w = securityRequest(s, "POST", "/api/v1/agent-tokens/", systemBearer, map[string]any{"name": "authorized", "userId": admin.ID})
	if w.Code != 201 {
		t.Fatalf("SystemAdmin delegation = %d", w.Code)
	}
}

func TestAgentTokenLiveOwnerAndRevocation(t *testing.T) {
	for _, scenario := range []string{"disabled", "reactivated", "removed", "demoted", "password", "version", "legacy", "expired"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			db := securityTestDB(t, false)
			u := securityUser(t, db)
			token, err := db.CreateAgentToken(ctx, "default", u.ID, "agent", []string{"Admin"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			claims, ok := resolveAgentBearer(ctx, db, token.Token)
			if !ok || claims.UserID != fmt.Sprint(u.ID) {
				t.Fatal("active owner was not preserved")
			}
			switch scenario {
			case "reactivated":
				u.Active = false
				if err = db.UpdateUser(ctx, u); err != nil {
					t.Fatal(err)
				}
				u.Active = true
				err = db.UpdateUser(ctx, u)
			case "disabled":
				u.Active = false
				err = db.UpdateUser(ctx, u)
			case "removed":
				_, err = db.RawDB().Exec("DELETE FROM user_organisations WHERE user_id = ?", u.ID)
			case "demoted":
				// Even a role change without a version bump must fail closed.
				_, err = db.RawDB().Exec("DELETE FROM user_organisation_roles WHERE user_id = ?", u.ID)
			case "password":
				err = db.SetUserPassword(ctx, u.ID, "replacement-hash")
			case "version":
				err = db.BumpUserTokenVersion(ctx, u.ID)
			case "legacy":
				_, err = db.RawDB().Exec("UPDATE org_agent_tokens SET user_token_version = 0 WHERE id = ?", token.ID)
			case "expired":
				_, err = db.RawDB().Exec("UPDATE org_agent_tokens SET expires_at = ? WHERE id = ?", time.Now().Add(-time.Hour).Format(time.RFC3339Nano), token.ID)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := resolveAgentBearer(ctx, db, token.Token); ok {
				t.Fatal("revoked token authenticated")
			}
			s := NewServer(ServerConfig{}, tree.NewTreeWithOperations(nil), nil, nil, "test-secret", db, "")
			if w := securityRequest(s, "GET", "/api/v1/agent-tokens/", token.Token, nil); w.Code != 401 {
				t.Fatalf("revoked HTTP token = %d", w.Code)
			}
			if _, ok := s.AuthenticateNATS(ctx, token.Token); ok {
				t.Fatal("revoked token authenticated to NATS")
			}
		})
	}
}

func TestBootstrapRequiresOperatorAndAtomicClaim(t *testing.T) {
	db := securityTestDB(t, true)
	s := NewServer(ServerConfig{}, tree.NewTreeWithOperations(nil), nil, nil, "test-secret", db, "")
	for _, secret := range []string{"", "too-short", auditSetupToken} {
		t.Setenv("XACT_BOOTSTRAP_SETUP_TOKEN", secret)
		for _, supplied := range []string{"", "wrong-secret"} {
			w := securityRequest(s, "POST", "/api/v1/bootstrap/admin/password", "", map[string]string{"password": "attacker-password", "setupToken": supplied})
			if w.Code != 403 {
				t.Fatalf("unauthorized bootstrap = %d", w.Code)
			}
		}
	}
	t.Setenv("XACT_BOOTSTRAP_SETUP_TOKEN", auditSetupToken)
	var wg sync.WaitGroup
	results := make(chan *httptest.ResponseRecorder, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- securityRequest(s, "POST", "/api/v1/bootstrap/admin/password", "", map[string]string{"password": "operator-password", "setupToken": auditSetupToken})
		}()
	}
	wg.Wait()
	close(results)
	codes := map[int]int{}
	for w := range results {
		codes[w.Code]++
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("bootstrap response may be cached")
		}
	}
	if codes[200] != 1 || codes[409] != 1 {
		t.Fatalf("concurrent setup responses = %v", codes)
	}
	_, hash, err := db.GetUserByLogin(context.Background(), "admin")
	if err != nil || !psql.CheckPassword(hash, "operator-password") {
		t.Fatal("operator password was not retained")
	}
}

type reportDiscardPublisher struct{}

func (reportDiscardPublisher) TagValuePublish(string, []byte) error { return nil }

func TestReportCannotReadOtherTenantHTTP(t *testing.T) {
	db := securityTestDB(t, false)
	securityUser(t, db)
	previous := tree.TagValuePublisher
	tree.TagValuePublisher = reportDiscardPublisher{}
	t.Cleanup(func() { tree.TagValuePublisher = previous })
	ops := tree.NewTreeWithOperations(nil)
	for _, org := range []string{"default", "beta"} {
		if err := ops.CreateTag("/"+org+"/device/secret", tree.TypeString, tree.TagConfig{}); err != nil {
			t.Fatal(err)
		}
		if err := ops.SetLeafValue("/"+org+"/device/secret", org+"_REPORT_MARKER"); err != nil {
			t.Fatal(err)
		}
	}
	s := NewServer(ServerConfig{}, ops, nil, nil, "test-secret", db, "")
	bearer := securityLogin(t, s, "tenant-admin", "test-user-password")
	for _, org := range []string{"default", "beta"} {
		template := map[string]any{"name": "audit-" + org, "variables": []map[string]string{{"name": "secret", "type": "rtdb", "path": org + ".device.secret"}}, "templateJson": map[string]any{"config": map[string]string{"documentTitle": "{{secret}}"}, "elements": []map[string]any{{"type": "title", "rows": [][]map[string]string{{{"text": "{{secret}}"}}}}}}}
		w := securityRequest(s, "POST", "/api/v1/reports/templates/", bearer, template)
		if w.Code != 201 {
			t.Fatalf("template creation = %d: %s", w.Code, w.Body.String())
		}
		var saved sqldb.PDFTemplate
		if err := json.Unmarshal(w.Body.Bytes(), &saved); err != nil {
			t.Fatal(err)
		}
		w = securityRequest(s, "POST", "/api/v1/reports/templates/"+saved.ID+"/preview", bearer, map[string]any{})
		if w.Code != 200 {
			t.Fatalf("preview = %d: %s", w.Code, w.Body.String())
		}
		if bytes.Contains(w.Body.Bytes(), []byte(org+"_REPORT_MARKER")) != (org == "default") {
			t.Fatalf("report tenant isolation failed for %s", org)
		}
	}
}
