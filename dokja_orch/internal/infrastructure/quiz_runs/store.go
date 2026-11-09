// Package quizruns keeps the questionnaires Ningo took, so later conversations remember them. It
// keeps the newest runs only and never logs their contents.
package quizruns

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"unicode/utf8"

	_ "modernc.org/sqlite"
	quiz "read_books/dokja_domain/dokja_quiz"
)

const FileName = "dokja_quiz_runs.db"

// kept is how many runs the file holds; conversations see quiz.RememberedRuns of them.
const kept = 20

const maxRunBytes = 256 << 10

func PathBeside(sharedDB string) string { return filepath.Join(filepath.Dir(sharedDB), FileName) }

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("quiz run database path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("create quiz run directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open quiz run database: %w", err)
	}
	file.Close()
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("open quiz run storage: %w", err)
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err = s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var version int
	if err = tx.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version > 1 {
		return fmt.Errorf("quiz run schema is newer than this build")
	}
	if version == 0 {
		_, err = tx.Exec(`CREATE TABLE quiz_runs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			finished_at INTEGER NOT NULL,
			run_json TEXT NOT NULL
		); PRAGMA user_version=1;`)
		if err != nil {
			return fmt.Errorf("initialize quiz run schema: %w", err)
		}
	}
	return tx.Commit()
}

func (s *Store) Close() error { return s.db.Close() }

// Save keeps a finished run and drops all but the newest ones.
func (s *Store) Save(ctx context.Context, run quiz.Run) error {
	if err := quiz.ValidateRun(run); err != nil {
		return err
	}
	raw, err := json.Marshal(run)
	if err != nil || len(raw) > maxRunBytes {
		return fmt.Errorf("quiz run exceeds storage bounds")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO quiz_runs(finished_at, run_json) VALUES(?, ?)`, run.FinishedAt.UnixNano(), string(raw)); err != nil {
		return fmt.Errorf("persist quiz run: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM quiz_runs WHERE id NOT IN (SELECT id FROM quiz_runs ORDER BY finished_at DESC, id DESC LIMIT ?)`, kept); err != nil {
		return fmt.Errorf("trim quiz runs: %w", err)
	}
	return tx.Commit()
}

// Recent returns the newest runs first. A stored run that no longer validates is skipped.
func (s *Store) Recent(ctx context.Context, limit int) ([]quiz.Run, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT run_json FROM quiz_runs ORDER BY finished_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("read quiz runs: %w", err)
	}
	defer rows.Close()
	var runs []quiz.Run
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("read quiz run: %w", err)
		}
		var run quiz.Run
		if len(raw) > maxRunBytes || !utf8.ValidString(raw) || json.Unmarshal([]byte(raw), &run) != nil || quiz.ValidateRun(run) != nil {
			continue
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}
