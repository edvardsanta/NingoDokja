// Package chat_sessions stores chat snapshots without logging their contents.
package chatsessions

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
	"unicode/utf8"

	_ "modernc.org/sqlite"
	chat "read_books/dokja_domain/dokja_chat"
)

const FileName = "dokja_chat_sessions.db"

func PathBeside(sharedDB string) string { return filepath.Join(filepath.Dir(sharedDB), FileName) }

type Store struct{ db *sql.DB }

var _ chat.SessionStore = (*Store)(nil)

func Open(path string) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("chat session database path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("create chat session directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open chat session database: %w", err)
	}
	file.Close()
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("open chat session storage: %w", err)
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
		return fmt.Errorf("chat session schema is newer than this build")
	}
	if version == 0 {
		_, err = tx.Exec(`CREATE TABLE chat_sessions (
			scope_key TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			last_seen_at INTEGER NOT NULL,
			summary TEXT NOT NULL,
			messages_json TEXT NOT NULL
		); PRAGMA user_version=1;`)
		if err != nil {
			return fmt.Errorf("initialize chat session schema: %w", err)
		}
	}
	return tx.Commit()
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Load(ctx context.Context, key string) (chat.SessionSnapshot, bool, error) {
	var snapshot chat.SessionSnapshot
	var seen int64
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT session_id,last_seen_at,summary,messages_json FROM chat_sessions WHERE scope_key=?`, key).Scan(&snapshot.ID, &seen, &snapshot.Summary, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return snapshot, false, nil
	}
	if err != nil {
		return snapshot, false, fmt.Errorf("read chat session: %w", err)
	}
	if len(raw) > chat.MaxSessionBytes || !utf8.ValidString(raw) || json.Unmarshal([]byte(raw), &snapshot.Messages) != nil {
		return snapshot, false, fmt.Errorf("invalid persisted chat session")
	}
	snapshot.LastSeenAt = time.Unix(0, seen).UTC()
	if err = chat.ValidateSession(snapshot); err != nil {
		return snapshot, false, err
	}
	return snapshot, true, nil
}

func (s *Store) Save(ctx context.Context, key string, snapshot chat.SessionSnapshot) error {
	if key == "" || len(key) > 1024 {
		return fmt.Errorf("invalid chat session scope")
	}
	if err := chat.ValidateSession(snapshot); err != nil {
		return err
	}
	raw, err := json.Marshal(snapshot.Messages)
	if err != nil || len(raw) > chat.MaxSessionBytes {
		return fmt.Errorf("chat session exceeds storage bounds")
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO chat_sessions(scope_key,session_id,last_seen_at,summary,messages_json)
		VALUES(?,?,?,?,?) ON CONFLICT(scope_key) DO UPDATE SET session_id=excluded.session_id,
		last_seen_at=excluded.last_seen_at,summary=excluded.summary,messages_json=excluded.messages_json`, key, snapshot.ID, snapshot.LastSeenAt.UnixNano(), snapshot.Summary, string(raw))
	if err != nil {
		return fmt.Errorf("persist chat session: %w", err)
	}
	return nil
}
