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

func TestServiceDeleteSubjectBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, subject string
		publish, want bool
	}{
		{"device wildcard", "xact.internal.delete_request.default.PUBLIC_BUS.BUSES.*", true, true},
		{"exact device", "xact.internal.delete_request.default.PUBLIC_BUS.BUSES.bus_1", true, true},
		{"path wildcard", "xact.internal.delete_request.default.PUBLIC_BUS.>", true, true},
		{"subscribe", "xact.internal.delete_request.default.PUBLIC_BUS.BUSES.*", false, false},
		{"wildcard tenant", "xact.internal.delete_request.*.PUBLIC_BUS.BUSES.*", true, false},
		{"all tenants", "xact.internal.delete_request.>", true, false},
		{"missing path", "xact.internal.delete_request.default", true, false},
		{"empty token", "xact.internal.delete_request.default..BUSES.*", true, false},
		{"nonterminal wildcard", "xact.internal.delete_request.default.PUBLIC_BUS.>.bus_1", true, false},
		{"whitespace", "xact.internal.delete_request.default.PUBLIC_BUS.BUSES.bus 1", true, false},
		{"other internal operation", "xact.internal.delete.default.PUBLIC_BUS.BUSES.*", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := validServiceSubject(tc.subject, "public_bus", "app:public_bus", tc.publish); got != tc.want {
				t.Fatalf("validServiceSubject(%q, publish=%v) = %v, want %v", tc.subject, tc.publish, got, tc.want)
			}
		})
	}
}

func TestBundledApplicationManifests(t *testing.T) {
	manifests, err := Load(filepath.Join("..", "..", "plugins"))
	if err != nil {
		t.Fatalf("bundled application manifests failed validation: %v", err)
	}
	if len(manifests) == 0 {
		t.Fatal("no bundled application manifests found")
	}
}

func TestInvalidServiceSubjectErrorIdentifiesManifestAndService(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "applications"), 0700); err != nil {
		t.Fatal(err)
	}
	const subject = "xact.internal.delete_request.*.DEMO.*"
	manifest := `{"id":"demo","resource":{"id":"demo","permissions":[{"name":"read"}]},"operations":["get_status"],"services":[{"username":"app:demo","password_env":"DEMO_SECRET","publish":["` + subject + `"]}]}`
	if err := os.WriteFile(filepath.Join(root, "applications", "demo.json"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(root)
	if err == nil {
		t.Fatal("invalid subject accepted")
	}
	for _, detail := range []string{"demo.json", "app:demo", subject, "invalid service publish subject"} {
		if !strings.Contains(err.Error(), detail) {
			t.Fatalf("error %q missing %q", err, detail)
		}
	}
}
