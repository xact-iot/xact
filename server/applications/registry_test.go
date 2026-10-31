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

func TestManifestHTTPProxyConfiguration(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "applications"), 0700); err != nil {
		t.Fatal(err)
	}
	manifest := func(id, http string) string {
		return `{"id":"` + id + `","resource":{"id":"` + id + `-res","permissions":[{"name":"view"}]},"operations":["get_status"],"http":` + http + `}`
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, "applications", name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("traffic.json", manifest("traffic", `{"prefix":"traffic","upstream":"http://127.0.0.1:8093","max_body_mb":200,"websocket_paths":["/ws"]}`))
	all, err := Load(root)
	if err != nil || len(all) != 1 || all[0].HTTP == nil {
		t.Fatalf("valid http block rejected: %v %#v", err, all)
	}
	if h := all[0].HTTP; h.Prefix != "traffic" || h.MaxBodyMB != 200 || h.TimeoutSeconds != DefaultTimeoutSeconds || len(h.WebSocketPaths) != 1 {
		t.Fatalf("http block not loaded with defaults: %#v", h)
	}
	write("traffic.json", manifest("traffic", `{"prefix":"traffic","upstream":"https://traffic.internal:8443/"}`))
	if all, err = Load(root); err != nil || all[0].HTTP.MaxBodyMB != DefaultMaxBodyMB {
		t.Fatalf("minimal http block: %v %#v", err, all)
	}

	for _, tc := range []struct{ name, http string }{
		{"missing prefix", `{"upstream":"http://127.0.0.1:8093"}`},
		{"uppercase prefix", `{"prefix":"Traffic","upstream":"http://127.0.0.1:8093"}`},
		{"slash in prefix", `{"prefix":"a/b","upstream":"http://127.0.0.1:8093"}`},
		{"reserved api", `{"prefix":"api","upstream":"http://127.0.0.1:8093"}`},
		{"reserved plugins", `{"prefix":"plugins","upstream":"http://127.0.0.1:8093"}`},
		{"nats websocket route", `{"prefix":"ws","upstream":"http://127.0.0.1:8093"}`},
		{"nats websocket route prefix", `{"prefix":"wsapp","upstream":"http://127.0.0.1:8093"}`},
		{"missing upstream", `{"prefix":"traffic"}`},
		{"relative upstream", `{"prefix":"traffic","upstream":"127.0.0.1:8093"}`},
		{"ftp upstream", `{"prefix":"traffic","upstream":"ftp://127.0.0.1:8093"}`},
		{"upstream path", `{"prefix":"traffic","upstream":"http://127.0.0.1:8093/svc"}`},
		{"upstream query", `{"prefix":"traffic","upstream":"http://127.0.0.1:8093?x=1"}`},
		{"upstream credentials", `{"prefix":"traffic","upstream":"http://user:pw@127.0.0.1:8093"}`},
		{"negative body", `{"prefix":"traffic","upstream":"http://127.0.0.1:8093","max_body_mb":-1}`},
		{"huge body", `{"prefix":"traffic","upstream":"http://127.0.0.1:8093","max_body_mb":100000}`},
		{"huge timeout", `{"prefix":"traffic","upstream":"http://127.0.0.1:8093","timeout_seconds":86400}`},
		{"relative websocket path", `{"prefix":"traffic","upstream":"http://127.0.0.1:8093","websocket_paths":["ws"]}`},
		{"dot websocket path", `{"prefix":"traffic","upstream":"http://127.0.0.1:8093","websocket_paths":["/ws/../api"]}`},
		{"query websocket path", `{"prefix":"traffic","upstream":"http://127.0.0.1:8093","websocket_paths":["/ws?x=1"]}`},
		{"duplicate websocket path", `{"prefix":"traffic","upstream":"http://127.0.0.1:8093","websocket_paths":["/ws","/ws"]}`},
		{"unknown field", `{"prefix":"traffic","upstream":"http://127.0.0.1:8093","cookies":true}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			write("traffic.json", manifest("traffic", tc.http))
			if _, err := Load(root); err == nil {
				t.Fatal("invalid http block accepted")
			}
		})
	}

	write("traffic.json", manifest("traffic", `{"prefix":"shared","upstream":"http://127.0.0.1:8093"}`))
	write("fleet.json", manifest("fleet", `{"prefix":"shared","upstream":"http://127.0.0.1:8094"}`))
	if _, err := Load(root); err == nil || !strings.Contains(err.Error(), "already used") {
		t.Fatalf("duplicate prefix accepted: %v", err)
	}
	write("fleet.json", manifest("fleet", `{"prefix":"fleet","upstream":"http://127.0.0.1:8094"}`))
	if all, err := Load(root); err != nil || len(all) != 2 {
		t.Fatalf("distinct prefixes rejected: %v", err)
	}
}

func TestServiceEventSubjectBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, subject string
		publish, want bool
	}{
		{"own application", "xact.internal.event_request.default.public_bus", true, true},
		{"other application", "xact.internal.event_request.default.traffic", true, false},
		{"wildcard application", "xact.internal.event_request.default.*", true, false},
		{"wildcard tenant", "xact.internal.event_request.*.public_bus", true, false},
		{"subscribe", "xact.internal.event_request.default.public_bus", false, false},
		{"extra token", "xact.internal.event_request.default.public_bus.x", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := validServiceSubject(tc.subject, "public_bus", "app:public_bus", tc.publish); got != tc.want {
				t.Fatalf("validServiceSubject(%q, publish=%v) = %v, want %v", tc.subject, tc.publish, got, tc.want)
			}
		})
	}
}
