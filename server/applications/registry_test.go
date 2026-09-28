package applications

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManifestRegistrationAndServiceBoundaries(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "applications"), 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "applications", "demo.json")
	base := `{"id":"demo","name":"Demo","widgets":["demo-config"],"resource":{"id":"demo-config","permissions":[{"name":"read"}]},"operations":["get_status"],"services":[{"username":"app:demo","password_env":"DEMO_SECRET","publish":["xact.app.v1.alpha.demo.default.ingest.phone","xact.internal.ingest_request.alpha.DEMO.*"],"subscribe":["xact.app.v1.alpha.demo.default.request.*","_INBOX.app_demo.>"]}]}`
	write := func(s string) {
		t.Helper()
		if err := os.WriteFile(file, []byte(s), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(base)
	all, err := Load(root)
	if err != nil || len(all) != 1 {
		t.Fatalf("valid manifest: %v %#v", err, all)
	}
	for _, tc := range []struct{ name, body string }{
		{"cross app", strings.Replace(base, "xact.app.v1.alpha.demo.default.ingest.phone", "xact.app.v1.alpha.other.default.ingest.phone", 1)},
		{"cross tenant wildcard", strings.Replace(base, "xact.internal.ingest_request.alpha.DEMO.*", "xact.internal.ingest_request.*.DEMO.*", 1)},
		{"broad inbox", strings.Replace(base, "_INBOX.app_demo.>", "_INBOX.>", 1)},
		{"broad publish", strings.Replace(base, "xact.internal.ingest_request.alpha.DEMO.*", ">", 1)},
		{"trailing json", base + ` {}`},
		{"duplicate op", strings.Replace(base, `"operations":["get_status"]`, `"operations":["get_status","get_status"]`, 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			write(tc.body)
			if _, err := Load(root); err == nil {
				t.Fatal("invalid manifest accepted")
			}
		})
	}
}
