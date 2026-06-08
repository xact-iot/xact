package sqldb

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveBootstrapAdminPasswordFromEnv(t *testing.T) {
	t.Setenv(BootstrapAdminPasswordEnv, "from-env")
	t.Setenv(BootstrapAdminPasswordFileEnv, "")

	cred, err := ResolveBootstrapAdminPassword("")
	if err != nil {
		t.Fatalf("ResolveBootstrapAdminPassword returned error: %v", err)
	}
	if !cred.Set || cred.Password != "from-env" || cred.Source != BootstrapAdminPasswordEnv {
		t.Fatalf("unexpected credential: %+v", cred)
	}
}

func TestResolveBootstrapAdminPasswordFromFile(t *testing.T) {
	t.Setenv(BootstrapAdminPasswordEnv, "")
	path := filepath.Join(t.TempDir(), "bootstrap.txt")
	if err := os.WriteFile(path, []byte("from-file\n"), 0o600); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	cred, err := ResolveBootstrapAdminPassword(path)
	if err != nil {
		t.Fatalf("ResolveBootstrapAdminPassword returned error: %v", err)
	}
	if !cred.Set || cred.Password != "from-file" || cred.Source != path {
		t.Fatalf("unexpected credential: %+v", cred)
	}
}

func TestResolveBootstrapAdminPasswordDefaultsToUnset(t *testing.T) {
	t.Setenv(BootstrapAdminPasswordEnv, "")
	t.Setenv(BootstrapAdminPasswordFileEnv, "")

	cred, err := ResolveBootstrapAdminPassword("")
	if err != nil {
		t.Fatalf("ResolveBootstrapAdminPassword returned error: %v", err)
	}
	if cred.Set || cred.Password != "" {
		t.Fatalf("unexpected credential: %+v", cred)
	}
	if !IsBootstrapAdminPasswordUnset(UnsetBootstrapAdminHash) {
		t.Fatalf("unset bootstrap hash was not recognised")
	}
}
