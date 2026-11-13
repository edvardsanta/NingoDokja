// Package profiles keeps the latest inferred profile of each person, and which stored sessions
// belong to a person the application recognized. It never logs their contents.
package profiles

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"unicode/utf8"

	_ "modernc.org/sqlite"
	conversation "read_books/dokja_domain/dokja_conversation"
)

const FileName = "dokja_profiles.db"

const (
	maxProfileBytes = 64 << 10
	maxKeyBytes     = 1024
)

func PathBeside(sharedDB string) string { return filepath.Join(filepath.Dir(sharedDB), FileName) }

type Store struct{ db *sql.DB }

var _ conversation.ProfileStore = (*Store)(nil)

// Open creates the file if needed, readable by its owner only. A file from a newer build is refused
// rather than read wrongly.
func Open(path string) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("profile database path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("create profile directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open profile database: %w", err)
	}
	file.Close()
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("open profile storage: %w", err)
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
		return fmt.Errorf("profile schema is newer than this build")
	}
	if version == 0 {
		_, err = tx.Exec(`CREATE TABLE profiles (
			person TEXT PRIMARY KEY,
			generated_at INTEGER NOT NULL,
			profile_json TEXT NOT NULL
		);
		CREATE TABLE person_sessions (
			scope_key TEXT PRIMARY KEY,
			person TEXT NOT NULL
		);
		PRAGMA user_version=1;`)
		if err != nil {
			return fmt.Errorf("initialize profile schema: %w", err)
		}
	}
	return tx.Commit()
}

func (s *Store) Close() error { return s.db.Close() }

// SaveProfile replaces the person's profile: only the latest reading is kept.
func (s *Store) SaveProfile(ctx context.Context, profile conversation.Profile) error {
	raw, err := json.Marshal(profile)
	if err != nil || profile.Person == "" || len(profile.Person) > maxKeyBytes || len(raw) > maxProfileBytes {
		return fmt.Errorf("profile exceeds storage bounds")
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO profiles(person, generated_at, profile_json) VALUES(?, ?, ?)
		ON CONFLICT(person) DO UPDATE SET generated_at=excluded.generated_at, profile_json=excluded.profile_json`,
		profile.Person, profile.GeneratedAt.UnixNano(), string(raw))
	if err != nil {
		return fmt.Errorf("persist profile: %w", err)
	}
	return nil
}

// AllProfiles gives every kept profile. A row that no longer reads is skipped, not fatal.
func (s *Store) AllProfiles(ctx context.Context) ([]conversation.Profile, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT profile_json FROM profiles ORDER BY person`)
	if err != nil {
		return nil, fmt.Errorf("read profiles: %w", err)
	}
	defer rows.Close()
	found := []conversation.Profile{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("read profiles: %w", err)
		}
		var profile conversation.Profile
		if utf8.ValidString(raw) && json.Unmarshal([]byte(raw), &profile) == nil && profile.Person != "" {
			found = append(found, profile)
		}
	}
	return found, rows.Err()
}

// NoteSession remembers that the stored session under key is the person's. The latest note wins.
func (s *Store) NoteSession(ctx context.Context, key, person string) error {
	if key == "" || person == "" || len(key) > maxKeyBytes || len(person) > maxKeyBytes {
		return fmt.Errorf("invalid person session")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO person_sessions(scope_key, person) VALUES(?, ?)
		ON CONFLICT(scope_key) DO UPDATE SET person=excluded.person`, key, person)
	if err != nil {
		return fmt.Errorf("persist person session: %w", err)
	}
	return nil
}

// PersonSessions maps each noted session key to its person.
func (s *Store) PersonSessions(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT scope_key, person FROM person_sessions`)
	if err != nil {
		return nil, fmt.Errorf("read person sessions: %w", err)
	}
	defer rows.Close()
	found := map[string]string{}
	for rows.Next() {
		var key, person string
		if err := rows.Scan(&key, &person); err != nil {
			return nil, fmt.Errorf("read person sessions: %w", err)
		}
		found[key] = person
	}
	return found, rows.Err()
}
