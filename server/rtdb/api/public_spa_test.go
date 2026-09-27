package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/xact-iot/xact/rtdb/tree"
)

func TestPublicSPAPathsDoNotRequireLogin(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>public app</html>"), 0600); err != nil {
		t.Fatal(err)
	}
	s := NewServer(ServerConfig{ProxyPath: "/xact", StaticDir: dir}, tree.NewTreeWithOperations(nil), nil, nil, "test-secret", nil, "")
	for _, path := range []string{"/xact/public", "/xact/public/", "/xact/public/7", "/xact/public/7/", "/xact/public/default/7", "/xact/public/default/7/"} {
		rr := httptest.NewRecorder()
		s.Router().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
		if rr.Code != http.StatusOK || rr.Body.String() != "<html>public app</html>" {
			t.Fatalf("%s: %d %q", path, rr.Code, rr.Body.String())
		}
	}
}
