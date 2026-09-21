// Package store is the SQLite database shared by the orchestrator, the chat service
// and the local admin tools (CLI and TUI).
//
// Secrets never leave through this package's read API: profiles are listed with a
// masked hint only. Only the chat service, which needs the key to call the provider,
// reads it (from Python, straight from the file).
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// DB wraps the shared database. Several processes open the same file at once, so it
// runs in WAL mode with a busy timeout.
type DB struct {
	db   *sql.DB
	path string
}

var migrations = []string{
	// 1: chat provider profiles and the settings table.
	`CREATE TABLE chat_profiles (
		name       TEXT PRIMARY KEY,
		base_url   TEXT NOT NULL,
		model      TEXT NOT NULL,
		api_key    TEXT NOT NULL,
		key_hint   TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);
	CREATE TABLE settings (
		key        TEXT PRIMARY KEY,
		value      TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);`,
}

// Open creates the database if needed (readable by its owner only, since it holds API
// keys), applies pending migrations and returns it ready to use.
func Open(path string) (*DB, error) {
	if path == "" {
		return nil, fmt.Errorf("database path is empty (set DOKJA_DB_FILE)")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create database dir: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open database file: %w", err)
	}
	file.Close()

	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	// One connection: writes are rare and this keeps transactions simple across goroutines.
	sqlDB.SetMaxOpenConns(1)

	d := &DB{db: sqlDB, path: path}
	if err := d.migrate(); err != nil {
		sqlDB.Close()
		return nil, err
	}
	return d, nil
}

func (d *DB) Close() error { return d.db.Close() }

func (d *DB) Path() string { return d.path }

func (d *DB) migrate() error {
	var version int
	if err := d.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read database version: %w", err)
	}
	if version > len(migrations) {
		return fmt.Errorf("database is version %d, newer than this build understands (%d)", version, len(migrations))
	}
	for i := version; i < len(migrations); i++ {
		tx, err := d.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func (d *DB) setting(key string) (string, error) {
	var value string
	err := d.db.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

func (d *DB) setSetting(key, value string) error {
	_, err := d.db.Exec(
		`INSERT INTO settings(key, value, updated_at) VALUES(?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, now())
	return err
}
