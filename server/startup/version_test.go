package main

import (
	"os"
	"strings"
	"testing"
)

func TestAppVersionUsesEmbeddedVersionFile(t *testing.T) {
	wantBytes, err := os.ReadFile("VERSION.txt")
	if err != nil {
		t.Fatalf("reading VERSION.txt: %v", err)
	}
	if got, want := appVersion(), strings.TrimSpace(string(wantBytes)); got != want {
		t.Fatalf("appVersion() = %q, want VERSION.txt value %q", got, want)
	}
}
