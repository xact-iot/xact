package api

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xact-iot/xact/rtdb/tree"
)

func TestUIAssetsUseCSPAndDoNotServeTestRunner(t *testing.T) {
	dir := t.TempDir()
	for path, body := range map[string]string{
		"index.html":           "application",
		"assets/theme-init.js": "window.themeLoaded = true;",
		"test/index.html":      "test-runner-must-not-ship",
	} {
		file := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	s := NewServer(ServerConfig{ProxyPath: "/xact", StaticDir: dir}, tree.NewTreeWithOperations(nil), nil, nil, "test-secret", nil, "")
	rr := httptest.NewRecorder()
	s.Router().ServeHTTP(rr, httptest.NewRequest("GET", "/xact/assets/theme-init.js", nil))
	if rr.Code != 200 || rr.Body.String() != "window.themeLoaded = true;" {
		t.Fatalf("theme asset: %d %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Header().Get("Content-Security-Policy"), "script-src 'self';") {
		t.Fatal("asset is missing script CSP")
	}
	rr = httptest.NewRecorder()
	s.Router().ServeHTTP(rr, httptest.NewRequest("GET", "/xact/test/index.html", nil))
	if strings.Contains(rr.Body.String(), "test-runner-must-not-ship") {
		t.Fatal("production server exposed the browser test runner")
	}
}
