package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func mobileRequest(method, target string, body []byte, roles ...string) *http.Request {
	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	claims := &JWTClaims{TenantID: "default", Roles: roles}
	return req.WithContext(context.WithValue(req.Context(), claimsContextKey, claims))
}

func TestMobileConfigOrganisationSelection(t *testing.T) {
	org, ok := mobileConfigOrg(mobileRequest(http.MethodGet, "/?org=second", nil, "SystemAdmin"))
	if !ok || org != "second" {
		t.Fatalf("SystemAdmin org = %q, %v; want second, true", org, ok)
	}
	if _, ok := mobileConfigOrg(mobileRequest(http.MethodGet, "/?org=second", nil, "Admin")); ok {
		t.Fatal("Admin must not override the active organisation")
	}
	org, ok = mobileConfigOrg(mobileRequest(http.MethodGet, "/", nil, "Operator"))
	if !ok || org != "default" {
		t.Fatalf("active org = %q, %v; want default, true", org, ok)
	}
}

func TestUpdateAndLoadMobileAppConfig(t *testing.T) {
	db := newTestDB("admin", "password")
	server := &Server{db: db}
	body := []byte(`{"deviceParentNodes":["default.Pumps","Pumps","Meters"],"defaultDashboardName":" Overview "}`)
	recorder := httptest.NewRecorder()
	server.handleUpdateMobileAppConfig(recorder, mobileRequest(http.MethodPut, "/api/v1/mobile/config", body, "Admin"))
	if recorder.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if db.configOrg != "default" || db.configName != mobileAppConfigName {
		t.Fatalf("saved config target = %q/%q", db.configOrg, db.configName)
	}
	var saved mobileAppConfig
	if err := json.Unmarshal(db.configValue, &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.DeviceParentNodes) != 2 || saved.DeviceParentNodes[0] != "Pumps" || saved.DefaultDashboardName != "Overview" {
		t.Fatalf("saved config = %#v", saved)
	}

	recorder = httptest.NewRecorder()
	server.handleMobileAppConfig(recorder, mobileRequest(http.MethodGet, "/api/v1/mobile/config", nil, "Operator"))
	if recorder.Code != http.StatusOK || !bytes.Contains(recorder.Body.Bytes(), []byte(`"defaultDashboardName":"Overview"`)) {
		t.Fatalf("GET status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestMobileReleaseUsesAuthenticatedLocalAPKRoute(t *testing.T) {
	t.Setenv("MOBILE_APP_VERSION", "1.0.1")
	t.Setenv("MOBILE_APK_URL", "")
	t.Setenv("MOBILE_APK_PATH", "../mobile/build/app-debug.apk")
	recorder := httptest.NewRecorder()

	new(Server).handleMobileRelease(recorder, mobileRequest(http.MethodGet, "/api/v1/mobile/release", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var release mobileReleaseResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &release); err != nil {
		t.Fatal(err)
	}
	if release.DownloadURL != "/api/v1/mobile/apk" {
		t.Fatalf("download URL = %q", release.DownloadURL)
	}
}

func TestMobileAPKDownload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "xact-mobile.apk")
	if err := os.WriteFile(path, []byte("test-apk"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MOBILE_APK_PATH", path)
	recorder := httptest.NewRecorder()

	new(Server).handleMobileAPK(recorder, mobileRequest(http.MethodGet, "/api/v1/mobile/apk", nil))

	if recorder.Code != http.StatusOK || recorder.Body.String() != "test-apk" {
		t.Fatalf("status = %d, body = %q", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/vnd.android.package-archive" {
		t.Fatalf("content type = %q", got)
	}
}
