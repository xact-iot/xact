package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDemoEnvFindsDemoEnvFromRepoRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "demo"), 0o755); err != nil {
		t.Fatalf("create demo dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "demo", ".env"), []byte("MQTT_BROKER=mqtts://127.0.0.1:1883\n"), 0o600); err != nil {
		t.Fatalf("write demo env: %v", err)
	}

	previousDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousDir); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir repo root: %v", err)
	}
	if err := os.Unsetenv("MQTT_BROKER"); err != nil {
		t.Fatalf("unset MQTT_BROKER: %v", err)
	}

	if err := loadDemoEnv(); err != nil {
		t.Fatalf("loadDemoEnv() error = %v", err)
	}
	if got := os.Getenv("MQTT_BROKER"); got != "mqtts://127.0.0.1:1883" {
		t.Fatalf("MQTT_BROKER = %q, want mqtts://127.0.0.1:1883", got)
	}
}
