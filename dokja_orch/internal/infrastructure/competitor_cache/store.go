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

	_ "modernc.org/sqlite"
)

const (
	FileName = "dokja_competitor_cache.db"
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
	queries_json TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS product_equivalences (
	source_key TEXT NOT NULL,
	candidate_key TEXT NOT NULL,
	equivalence_json TEXT NOT NULL,
	PRIMARY KEY (source_key, candidate_key)
);`)
	if err != nil {
		return err
	}
	// Earlier versions cached model opinions alongside discovery records.
	_, err = s.db.Exec(`DELETE FROM product_equivalences
WHERE json_valid(equivalence_json)
AND json_extract(equivalence_json, '$.estimated') = 1
AND json_extract(equivalence_json, '$.relationship') IS NOT NULL`)
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
	err := s.db.QueryRowContext(ctx, `SELECT queries_json FROM competitor_queries WHERE cache_key = ?`, key).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
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
INSERT INTO competitor_queries(cache_key, queries_json) VALUES(?, ?)
ON CONFLICT(cache_key) DO UPDATE SET queries_json=excluded.queries_json`,
		key, string(raw))
	return err
}

// Delete removes one plan after a profile or policy change.
func (s *Store) Delete(ctx context.Context, key string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM competitor_queries WHERE cache_key = ?`, key); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM product_equivalences WHERE source_key = ?`, key)
	return err
}

// Clear removes all durable plans. It is intended for explicit operator invalidation.
func (s *Store) Clear(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM competitor_queries`); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM product_equivalences`)
	return err
}

func (s *Store) GetEquivalences(ctx context.Context, key string) ([]map[string]any, bool, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT equivalence_json FROM product_equivalences
WHERE source_key = ? ORDER BY candidate_key`, key)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	var result []map[string]any
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, false, err
		}
		var item map[string]any
		if err := json.Unmarshal([]byte(raw), &item); err != nil {
			return nil, false, err
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	return result, len(result) > 0, nil
}

func (s *Store) PutEquivalences(ctx context.Context, key string, items []map[string]any) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, item := range items {
		raw, err := json.Marshal(item)
		if err != nil || len(raw) > maxJSON {
			return fmt.Errorf("competitor equivalence exceeds storage bounds")
		}
		candidateKey, _ := item["product_key"].(string)
		if candidateKey == "" {
			return fmt.Errorf("competitor equivalence product key is empty")
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO product_equivalences(source_key, candidate_key, equivalence_json)
VALUES(?, ?, ?)
ON CONFLICT(source_key, candidate_key) DO UPDATE SET equivalence_json=excluded.equivalence_json`,
			key, candidateKey, string(raw)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) DeleteEquivalences(ctx context.Context, key string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM product_equivalences WHERE source_key = ?`, key)
	return err
}
