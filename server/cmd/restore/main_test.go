package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseRestoreArgsRequiresConfirmation(t *testing.T) {
	_, err := parseRestoreArgs([]string{"backup.tar.gz"}, emptyEnv)
	if err == nil || !strings.Contains(err.Error(), "requires --confirm") {
		t.Fatalf("parseRestoreArgs error = %v, want confirmation error", err)
	}
}

func TestParseRestoreArgsAcceptsFlagConfirmation(t *testing.T) {
	opts, err := parseRestoreArgs([]string{"--confirm", "backup.tar.gz"}, emptyEnv)
	if err != nil {
		t.Fatalf("parseRestoreArgs returned error: %v", err)
	}
	if !opts.Confirm || opts.ArchivePath != "backup.tar.gz" {
		t.Fatalf("opts = %+v, want confirmed backup.tar.gz", opts)
	}
}

func TestParseRestoreArgsAcceptsEnvConfirmationAndChecksum(t *testing.T) {
	sum := sha256.Sum256([]byte("archive"))
	opts, err := parseRestoreArgs([]string{"backup.tar.gz"}, func(name string) string {
		switch name {
		case "XACT_RESTORE_CONFIRM":
			return "yes"
		case "XACT_RESTORE_SHA256":
			return hex.EncodeToString(sum[:])
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatalf("parseRestoreArgs returned error: %v", err)
	}
	if !opts.Confirm || opts.SHA256 == "" || opts.ArchivePath != "backup.tar.gz" {
		t.Fatalf("opts = %+v, want env-confirmed checksum restore", opts)
	}
}

func TestVerifySHA256RejectsMismatch(t *testing.T) {
	sum := sha256.Sum256([]byte("expected"))
	err := verifySHA256(bytes.NewReader([]byte("actual")), hex.EncodeToString(sum[:]))
	if err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("verifySHA256 error = %v, want mismatch", err)
	}
}

func TestVerifySHA256AcceptsMatch(t *testing.T) {
	data := []byte("archive")
	sum := sha256.Sum256(data)
	if err := verifySHA256(bytes.NewReader(data), hex.EncodeToString(sum[:])); err != nil {
		t.Fatalf("verifySHA256 returned error: %v", err)
	}
}

func TestRestoreSafetyDirDefaultAndEnvOverride(t *testing.T) {
	if got := restoreSafetyDir(emptyEnv); got != "./backups" {
		t.Fatalf("restoreSafetyDir default = %q, want ./backups", got)
	}
	if got := restoreSafetyDir(func(name string) string {
		if name == "XACT_RESTORE_SAFETY_DIR" {
			return "/tmp/xact-safety"
		}
		return ""
	}); got != "/tmp/xact-safety" {
		t.Fatalf("restoreSafetyDir override = %q", got)
	}
}

func TestCopySQLiteDatabase(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "xact.db")
	dst := filepath.Join(dir, "backups", "pre-restore.db")
	if err := os.WriteFile(src, []byte("sqlite-data"), 0o600); err != nil {
		t.Fatalf("os.WriteFile returned error: %v", err)
	}

	if err := copySQLiteDatabase(src, dst); err != nil {
		t.Fatalf("copySQLiteDatabase returned error: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("os.ReadFile returned error: %v", err)
	}
	if string(got) != "sqlite-data" {
		t.Fatalf("copied data = %q", got)
	}
}

func emptyEnv(string) string { return "" }
