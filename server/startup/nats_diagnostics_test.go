package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTailFileReturnsLastLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nats.log")
	mustWriteFile(t, path, "one\ntwo\nthree\n")

	tail, err := tailFile(path, 2)
	if err != nil {
		t.Fatalf("tailFile returned error: %v", err)
	}
	if tail != "two\nthree" {
		t.Fatalf("unexpected tail: %q", tail)
	}
}

func TestTailFileEmptyWhenMissingPath(t *testing.T) {
	tail, err := tailFile("", 20)
	if err != nil {
		t.Fatalf("tailFile returned error: %v", err)
	}
	if tail != "" {
		t.Fatalf("expected empty tail, got %q", tail)
	}
}

func mustWriteFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := ensureParentDir(path); err != nil {
		t.Fatalf("ensureParentDir returned error: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}
}
