// restore - restores an XACT database from a backup archive produced by the
// Scheduler's backup task.
//
// Usage:
//
//	./bin/restore --confirm [--sha256 <hex>] <backup.tar.gz>
//
// The database connection is read from the same .env file used by the server.
// If DATABASE_URL is set PostgreSQL is used; otherwise SQLITE_PATH is used.
package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/joho/godotenv"
	"github.com/xact-iot/xact/backups"
	_ "modernc.org/sqlite"
)

type restoreOptions struct {
	ArchivePath string
	Confirm     bool
	SHA256      string
}

func main() {
	opts, err := parseRestoreArgs(os.Args[1:], os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		printUsage(os.Stderr, filepath.Base(os.Args[0]))
		os.Exit(1)
	}

	// Load .env from the directory containing the binary (same as the server).
	envPath := filepath.Join(filepath.Dir(os.Args[0]), "..", ".env")
	if err := godotenv.Load(envPath); err != nil {
		// Fallback: try the current working directory.
		_ = godotenv.Load(".env")
	}

	dbURL := os.Getenv("DATABASE_URL")
	sqlitePath := os.Getenv("SQLITE_PATH")
	safetyDir := restoreSafetyDir(os.Getenv)

	var adapter backups.Adapter
	var clearTarget func(context.Context) error
	var db *sql.DB

	ctx := context.Background()
	timestamp := time.Now().Format("20060102-150405")

	switch {
	case dbURL != "":
		log.Printf("Using PostgreSQL: %s", dbURL)
		var err error
		db, err = sql.Open("pgx", dbURL)
		if err != nil {
			log.Fatalf("opening postgres connection: %v", err)
		}
		if err := db.Ping(); err != nil {
			log.Fatalf("connecting to postgres: %v", err)
		}
		pgAdapter := &backups.PostgresAdapter{DB: db}
		adapter = pgAdapter
		safetyPath := filepath.Join(safetyDir, fmt.Sprintf("pre-restore-postgres-%s.tar.gz", timestamp))
		if err := writeSafetyBackup(ctx, pgAdapter, safetyPath); err != nil {
			log.Fatalf("saving existing PostgreSQL database before restore: %v", err)
		}
		log.Printf("Saved existing PostgreSQL database to %s", safetyPath)
		clearTarget = pgAdapter.DropPublicTables

	case sqlitePath != "":
		log.Printf("Using SQLite: %s", sqlitePath)
		var err error
		db, err = sql.Open("sqlite", sqlitePath)
		if err != nil {
			log.Fatalf("opening sqlite database: %v", err)
		}
		if err := db.Ping(); err != nil {
			log.Fatalf("connecting to sqlite database: %v", err)
		}
		if err := checkpointSQLite(ctx, db); err != nil {
			log.Fatalf("checkpointing sqlite database before safety copy: %v", err)
		}
		safetyPath := filepath.Join(safetyDir, fmt.Sprintf("pre-restore-sqlite-%s-%s", timestamp, filepath.Base(sqlitePath)))
		if err := copySQLiteDatabase(sqlitePath, safetyPath); err != nil {
			log.Fatalf("saving existing SQLite database before restore: %v", err)
		}
		log.Printf("Saved existing SQLite database to %s", safetyPath)
		sqliteAdapter := &backups.SQLiteAdapter{DB: db}
		adapter = sqliteAdapter
		clearTarget = sqliteAdapter.DropUserTables

	default:
		log.Fatal("Neither DATABASE_URL nor SQLITE_PATH is set in .env")
	}
	defer db.Close()

	f, err := os.Open(opts.ArchivePath)
	if err != nil {
		log.Fatalf("opening archive: %v", err)
	}
	defer f.Close()

	if opts.SHA256 != "" {
		if err := verifySHA256(f, opts.SHA256); err != nil {
			log.Fatalf("archive verification failed: %v", err)
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			log.Fatalf("rewinding archive after verification: %v", err)
		}
	}

	log.Printf("Restoring from %s", opts.ArchivePath)
	if err := backups.RestoreReplacing(ctx, adapter, f, clearTarget); err != nil {
		log.Fatalf("restore failed: %v", err)
	}
	log.Println("Restore complete.")
}

func parseRestoreArgs(args []string, getenv func(string) string) (restoreOptions, error) {
	var opts restoreOptions
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&opts.Confirm, "confirm", false, "confirm the operator intentionally wants to restore")
	fs.StringVar(&opts.SHA256, "sha256", "", "expected SHA-256 checksum of the archive")
	if err := fs.Parse(args); err != nil {
		return opts, err
	}
	if parseBoolEnv(getenv("XACT_RESTORE_CONFIRM")) {
		opts.Confirm = true
	}
	if opts.SHA256 == "" {
		opts.SHA256 = strings.TrimSpace(getenv("XACT_RESTORE_SHA256"))
	}
	if !opts.Confirm {
		return opts, errors.New("restore requires --confirm or XACT_RESTORE_CONFIRM=yes")
	}
	remaining := fs.Args()
	if len(remaining) != 1 {
		return opts, errors.New("restore requires exactly one backup archive path")
	}
	opts.ArchivePath = remaining[0]
	if opts.SHA256 != "" {
		if _, err := parseExpectedSHA256(opts.SHA256); err != nil {
			return opts, err
		}
	}
	return opts, nil
}

func verifySHA256(r io.Reader, expectedHex string) error {
	expected, err := parseExpectedSHA256(expectedHex)
	if err != nil {
		return err
	}
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return err
	}
	actual := h.Sum(nil)
	if !equalBytes(actual, expected) {
		return fmt.Errorf("sha256 mismatch: got %s", hex.EncodeToString(actual))
	}
	return nil
}

func parseExpectedSHA256(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if len(value) != sha256.Size*2 {
		return nil, fmt.Errorf("sha256 must be %d hex characters", sha256.Size*2)
	}
	decoded, err := hex.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("sha256 must be hex: %w", err)
	}
	return decoded, nil
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

func parseBoolEnv(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func restoreSafetyDir(getenv func(string) string) string {
	if dir := strings.TrimSpace(getenv("XACT_RESTORE_SAFETY_DIR")); dir != "" {
		return dir
	}
	return "./backups"
}

func writeSafetyBackup(ctx context.Context, adapter backups.Adapter, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := backups.Backup(ctx, adapter, f); err != nil {
		return err
	}
	return nil
}

func checkpointSQLite(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`)
	return err
}

func copySQLiteDatabase(src, dst string) error {
	if strings.TrimSpace(src) == "" || src == ":memory:" {
		return fmt.Errorf("sqlite database path is not a filesystem path")
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("sqlite database %s does not exist", src)
		}
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := out.Close(); cerr != nil {
			log.Printf("Warning: closing SQLite safety copy failed: %v", cerr)
		}
	}()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

func printUsage(w io.Writer, name string) {
	fmt.Fprintf(w, "Usage: %s --confirm [--sha256 <hex>] <backup.tar.gz>\n", name)
	fmt.Fprintln(w, "Set XACT_RESTORE_CONFIRM=yes, XACT_RESTORE_SHA256=<hex>, and optionally XACT_RESTORE_SAFETY_DIR=<dir> for non-interactive restores.")
}
