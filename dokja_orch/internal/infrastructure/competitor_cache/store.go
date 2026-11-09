// Package competitorcache stores provider-generated competitor search plans.
package competitorcache

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const (
	FileName = "dokja_competitor_cache.db"
	ttl      = 30 * 24 * time.Hour
	maxJSON  = 64 << 10
)

type Store struct{ db *sql.DB }

func PathBeside(sharedDB string) string {
	return filepath.Join(filepath.Dir(sharedDB), FileName)
}

func Open(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("competitor cache database path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("create competitor cache directory: %w", err)
	}
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("open competitor cache: %w", err)
	}
	db.SetMaxOpenConns(1)
	store := &Store{db: db}
	if err := store.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS competitor_queries (
	cache_key TEXT PRIMARY KEY,
	queries_json TEXT NOT NULL,
	expires_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS competitor_queries_expires_at ON competitor_queries(expires_at);`)
	return err
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func Key(facts any) (string, error) {
	raw, err := json.Marshal(facts)
	if err != nil {
		return "", err
	}
	if len(raw) > maxJSON {
		return "", fmt.Errorf("competitor cache facts exceed storage bounds")
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func (s *Store) Get(ctx context.Context, key string) ([]string, bool, error) {
	var raw string
	var expires int64
	err := s.db.QueryRowContext(ctx, `SELECT queries_json, expires_at FROM competitor_queries WHERE cache_key = ?`, key).Scan(&raw, &expires)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if expires <= time.Now().UnixNano() {
		_, _ = s.db.ExecContext(ctx, `DELETE FROM competitor_queries WHERE cache_key = ?`, key)
		return nil, false, nil
	}
	var queries []string
	if err := json.Unmarshal([]byte(raw), &queries); err != nil {
		return nil, false, err
	}
	return queries, true, nil
}

func (s *Store) Put(ctx context.Context, key string, queries []string) error {
	raw, err := json.Marshal(queries)
	if err != nil || len(raw) > maxJSON {
		return fmt.Errorf("competitor cache result exceeds storage bounds")
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO competitor_queries(cache_key, queries_json, expires_at) VALUES(?, ?, ?)
ON CONFLICT(cache_key) DO UPDATE SET queries_json=excluded.queries_json, expires_at=excluded.expires_at`,
		key, string(raw), time.Now().Add(ttl).UnixNano())
	return err
}
