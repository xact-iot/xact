package auth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMissingPluginReturnsNil(t *testing.T) {
	p, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load missing plugin: %v", err)
	}
	if p != nil {
		t.Fatalf("plugin = %#v, want nil", p)
	}
}

func TestLoadAuthenticatePlugin(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "authentication")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := `package authenticate
func Authenticate(user, password string) bool {
	return user == "admin" && password == "secret"
}`
	if err := os.WriteFile(filepath.Join(dir, pluginFile), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := Load(root)
	if err != nil {
		t.Fatalf("Load plugin: %v", err)
	}
	if p == nil || !p.Authenticate("admin", "secret") || p.Authenticate("admin", "wrong") {
		t.Fatalf("plugin auth behavior mismatch: %#v", p)
	}
	if !strings.HasSuffix(p.Path(), filepath.Join("authentication", pluginFile)) {
		t.Fatalf("plugin path = %q", p.Path())
	}
}

func TestLoadRejectsBadPlugin(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "authentication")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, pluginFile), []byte(`package authenticate
func Authenticate(user string) bool { return true }`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root); err == nil || !strings.Contains(err.Error(), "wrong type") {
		t.Fatalf("Load bad plugin error = %v", err)
	}
}
