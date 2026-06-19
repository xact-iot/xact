package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/xact-iot/xact/sqldb"
)

func requestWithClaims(claims *JWTClaims) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	return req.WithContext(context.WithValue(req.Context(), claimsContextKey, claims))
}

func TestAuthorizationHelpersWithClaims(t *testing.T) {
	claims := &JWTClaims{UserID: "42", TenantID: "default", Roles: []string{"Operator"}, AllowedOrgs: []string{"plant"}}
	req := requestWithClaims(claims)
	s := &Server{}

	if id, ok := currentUserID(req); !ok || id != 42 {
		t.Fatalf("currentUserID = %d/%v", id, ok)
	}
	if !claimsCanAccessOrg(claims, "plant") || claimsCanAccessOrg(claims, "other") {
		t.Fatal("claimsCanAccessOrg mismatch")
	}
	if s.canAccessOrg(req, "other") {
		t.Fatal("server should not access other org")
	}
	allowed, systemAdmin := s.allowedOrgSet(req)
	if systemAdmin || !allowed["default"] || !allowed["plant"] {
		t.Fatalf("allowed=%#v systemAdmin=%v", allowed, systemAdmin)
	}
	if org := currentOrgFromRequest(req); org != "default" {
		t.Fatalf("current org = %q", org)
	}
	if path, ok := s.rtdbPathForRequest(req, "pump.temp", false); !ok || path != "default/pump/temp" {
		t.Fatalf("rtdb path = %q/%v", path, ok)
	}
	if !s.requireRequestPathInTenant(req, "default/pump") || s.requireRequestPathInTenant(req, "other/pump") {
		t.Fatal("tenant path check mismatch")
	}

	admin := &JWTClaims{UserID: "bad", TenantID: "default", Roles: []string{"SystemAdmin"}}
	if !claimsHasSystemAdmin(admin) || !claimsPathInTenant(admin, "anything") {
		t.Fatal("system admin helpers mismatch")
	}
	if id, ok := currentUserID(requestWithClaims(admin)); ok || id != 0 {
		t.Fatalf("bad currentUserID = %d/%v", id, ok)
	}
}

func TestFilterUsersByAllowedOrgs(t *testing.T) {
	users := []sqldb.User{
		{LoginName: "a", Orgs: []sqldb.UserOrg{{OrgName: "default"}, {OrgName: "plant"}}},
		{LoginName: "b", Orgs: []sqldb.UserOrg{{OrgName: "other"}}},
	}
	filtered := filterUsersByAllowedOrgs(users, map[string]bool{"plant": true}, false)
	if len(filtered) != 1 || filtered[0].LoginName != "a" || len(filtered[0].Orgs) != 1 || filtered[0].Orgs[0].OrgName != "plant" {
		t.Fatalf("filtered users = %#v", filtered)
	}
	if got := filterUsersByAllowedOrgs(users, nil, true); len(got) != 2 {
		t.Fatalf("system admin filtered users = %#v", got)
	}
}

func TestCommandHelpers(t *testing.T) {
	if commandRelativePath("default", "pump", "default.pump.controls.start") != "controls.start" {
		t.Fatal("full commandRelativePath mismatch")
	}
	if commandRelativePath("default", "pump", "pump/controls/start") != "controls.start" {
		t.Fatal("device commandRelativePath mismatch")
	}
	for _, value := range []string{"", "bad value", "bad*", "bad/name", `bad\name`} {
		if validCommandSubjectComponent(value) {
			t.Fatalf("%q should be invalid", value)
		}
	}
	if !validCommandSubjectComponent("pump-1") || newCommandID() == "" {
		t.Fatal("valid command helper mismatch")
	}
}

func TestPluginHandlersListAndServe(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "widgets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "widgets", "big-number.js"), []byte("export default {};"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "widgets", "README.md"), []byte("ignore"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Server{pluginDir: root}

	rr := httptest.NewRecorder()
	s.handleListWidgetPlugins(rr, httptest.NewRequest(http.MethodGet, "/plugins/widgets", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", rr.Code, rr.Body.String())
	}
	var metas []widgetPluginMeta
	if err := json.NewDecoder(rr.Body).Decode(&metas); err != nil {
		t.Fatalf("decode metas: %v", err)
	}
	if len(metas) != 1 || metas[0].Name != "big-number" || metas[0].URL != "/plugins/widgets/big-number.js" {
		t.Fatalf("metas = %#v", metas)
	}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("filename", "big-number.js")
	req := httptest.NewRequest(http.MethodGet, "/plugins/widgets/big-number.js", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr = httptest.NewRecorder()
	s.handleServeWidgetPlugin(rr, req)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "export default") {
		t.Fatalf("serve status=%d body=%s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "application/javascript") {
		t.Fatalf("content type = %q", ct)
	}

	rctx = chi.NewRouteContext()
	rctx.URLParams.Add("filename", "../secret.js")
	req = httptest.NewRequest(http.MethodGet, "/plugins/widgets/../secret.js", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rr = httptest.NewRecorder()
	s.handleServeWidgetPlugin(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("path traversal status = %d", rr.Code)
	}
}

func TestWidgetCatalogHandler(t *testing.T) {
	s := &Server{}
	rr := httptest.NewRecorder()
	s.handleWidgetCatalog(rr, httptest.NewRequest(http.MethodGet, "/api/v1/widgets/catalog", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("catalog status = %d body=%s", rr.Code, rr.Body.String())
	}
	var catalog struct {
		Version string `json:"version"`
		Widgets []struct {
			Type        string         `json:"type"`
			DefaultW    int            `json:"defaultW"`
			ConfigHints map[string]any `json:"configHints"`
			Properties  []struct {
				Name string `json:"name"`
				Type string `json:"type"`
			} `json:"properties"`
		} `json:"widgets"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&catalog); err != nil {
		t.Fatalf("decode catalog: %v", err)
	}
	if catalog.Version == "" || len(catalog.Widgets) == 0 {
		t.Fatalf("catalog = %#v", catalog)
	}
	foundBigNumber := false
	foundAreaMap := false
	for _, widget := range catalog.Widgets {
		if widget.Type == "big-number-widget" {
			foundBigNumber = true
			if widget.DefaultW != 6 {
				t.Fatalf("big-number defaultW = %d", widget.DefaultW)
			}
			hasTagPath := false
			for _, prop := range widget.Properties {
				if prop.Name == "tagPath" && prop.Type == "path" {
					hasTagPath = true
				}
			}
			if !hasTagPath {
				t.Fatalf("big-number properties missing tagPath: %#v", widget.Properties)
			}
		}
		if widget.Type == "area-map-widget" {
			foundAreaMap = true
			hasLayers := false
			for _, prop := range widget.Properties {
				if prop.Name == "layers" && prop.Type == "array" {
					hasLayers = true
				}
			}
			if !hasLayers {
				t.Fatalf("area-map properties missing layers: %#v", widget.Properties)
			}
			coords, _ := widget.ConfigHints["coordinates"].(map[string]any)
			if coords["latitudeTag"] != "meta.lat" || coords["longitudeTag"] != "meta.lon" {
				t.Fatalf("area-map coordinates hint = %#v", coords)
			}
			if widget.ConfigHints["canonicalDeviceLayerExample"] == nil || widget.ConfigHints["iconRuleSchema"] == nil {
				t.Fatalf("area-map hints missing example/rule schema: %#v", widget.ConfigHints)
			}
			rules, _ := widget.ConfigHints["pathPatternRules"].([]any)
			hasWholeSegmentRule := false
			hasAirQualityExample := false
			for _, raw := range rules {
				rule, _ := raw.(string)
				if strings.Contains(rule, "whole segment") {
					hasWholeSegmentRule = true
				}
				if strings.Contains(rule, "LA_LongBeach.AirQuality.*") && strings.Contains(rule, "not LA_LongBeach.AirQuality.AQ-B-*") {
					hasAirQualityExample = true
				}
			}
			if !hasWholeSegmentRule || !hasAirQualityExample {
				t.Fatalf("area-map pathPatternRules missing wildcard guidance: %#v", rules)
			}
			clickThrough, _ := widget.ConfigHints["clickThrough"].(map[string]any)
			variationHint := clickThrough["dashboardVariationSelection"]
			if !strings.Contains(fmt.Sprint(variationHint), "meta.deviceSubtype") ||
				!strings.Contains(fmt.Sprint(variationHint), "meta.subtype") ||
				!strings.Contains(fmt.Sprint(variationHint), "meta.variation") {
				t.Fatalf("area-map clickThrough missing dashboard variation hint: %#v", clickThrough)
			}
		}
	}
	if !foundBigNumber {
		t.Fatalf("big-number-widget missing from catalog")
	}
	if !foundAreaMap {
		t.Fatalf("area-map-widget missing from catalog")
	}
}

func TestHardeningHelpers(t *testing.T) {
	for _, value := range []string{"1", "true", "yes", "on"} {
		if !parseAPIEnvBool(value, false) {
			t.Fatalf("%q should parse true", value)
		}
	}
	for _, value := range []string{"0", "false", "no", "off"} {
		if parseAPIEnvBool(value, true) {
			t.Fatalf("%q should parse false", value)
		}
	}
	if !parseAPIEnvBool("unknown", true) || parseAPIEnvBool("unknown", false) {
		t.Fatal("fallback bool parsing mismatch")
	}
	t.Setenv("ENABLE_AUTH_PLUGIN", "true")
	if !authPluginExecutionEnabled() {
		t.Fatal("auth plugin env should be enabled")
	}

	root := t.TempDir()
	if trustedPluginDir(root) != root {
		t.Fatal("trustedPluginDir should accept safe dir")
	}
	filePath := filepath.Join(t.TempDir(), "plugins")
	if err := os.WriteFile(filePath, []byte("not dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	if trustedPluginDir(filePath) != "" {
		t.Fatal("trustedPluginDir should reject file")
	}

	handler := securityHeaders(limitRequestBody(4)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body == nil {
			t.Fatal("body should be present")
		}
		w.WriteHeader(http.StatusNoContent)
	})))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/x", strings.NewReader("1234")))
	if rr.Code != http.StatusNoContent || rr.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatalf("hardening response status=%d headers=%#v", rr.Code, rr.Header())
	}
}
