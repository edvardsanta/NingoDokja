package profiles

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"

	conversation "read_books/dokja_domain/dokja_conversation"
)

var (
	ErrStorage = errors.New("profile storage failed")
	ErrSchema  = errors.New("profile schema is unsupported or uninitialized")
)

const schemaVersion = 1

// schemaLockKey serializes concurrent initializations of this namespace.
const schemaLockKey = int64(0x0d0c_1a00_0016_0001)

// PostgresStore keeps profiles and person sessions in the dokja_profiles schema. It is the same
// contract as Store; the owner picks it by setting a DSN.
type PostgresStore struct{ db *sql.DB }

var _ conversation.ProfileStore = (*PostgresStore)(nil)

// safeError never exposes native SQL diagnostics, values or credentials.
func safeError(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return ErrStorage
}

// OpenPostgres checks an existing namespace. It never creates or upgrades tables. The caller owns
// the connection configuration and the database's lifetime.
func OpenPostgres(ctx context.Context, db *sql.DB) (*PostgresStore, error) {
	if db == nil {
		return nil, ErrStorage
	}
	var version int
	if err := db.QueryRowContext(ctx, `SELECT version FROM dokja_profiles.schema_version WHERE id = 1`).Scan(&version); err != nil || version != schemaVersion {
		return nil, ErrSchema
	}
	return &PostgresStore{db: db}, nil
}

// InitializePostgres installs only this namespace in the supplied database. It is idempotent and is
// run by the explicit profiles-init command, never by the orchestrator's startup.
func InitializePostgres(ctx context.Context, db *sql.DB) (*PostgresStore, error) {
	if db == nil {
		return nil, ErrStorage
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, safeError(ctx)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, schemaLockKey); err != nil {
		return nil, safeError(ctx)
	}
	if _, err := tx.ExecContext(ctx, `
CREATE SCHEMA IF NOT EXISTS dokja_profiles;
CREATE TABLE IF NOT EXISTS dokja_profiles.schema_version (
	id      SMALLINT PRIMARY KEY CHECK (id = 1),
	version INTEGER NOT NULL
);`); err != nil {
		return nil, safeError(ctx)
	}
	var version int
	err = tx.QueryRowContext(ctx, `SELECT version FROM dokja_profiles.schema_version WHERE id = 1 FOR UPDATE`).Scan(&version)
	switch {
	case err == nil && version != schemaVersion:
		return nil, ErrSchema
	case errors.Is(err, sql.ErrNoRows):
		if _, err := tx.ExecContext(ctx, `
CREATE TABLE dokja_profiles.profiles (
	person       TEXT PRIMARY KEY CHECK (person <> '' AND length(person) <= 1024),
	generated_at TIMESTAMPTZ NOT NULL,
	profile      JSONB NOT NULL
);
CREATE TABLE dokja_profiles.person_sessions (
	scope_key TEXT PRIMARY KEY CHECK (scope_key <> '' AND length(scope_key) <= 1024),
	person    TEXT NOT NULL CHECK (person <> '' AND length(person) <= 1024)
);
INSERT INTO dokja_profiles.schema_version (id, version) VALUES (1, 1);`); err != nil {
			return nil, safeError(ctx)
		}
	case err != nil:
		return nil, safeError(ctx)
	}
	if err := tx.Commit(); err != nil {
		return nil, safeError(ctx)
	}
	return &PostgresStore{db: db}, nil
}

// SaveProfile replaces the person's profile: only the latest reading is kept.
func (s *PostgresStore) SaveProfile(ctx context.Context, profile conversation.Profile) error {
	raw, err := json.Marshal(profile)
	if err != nil || profile.Person == "" || len(profile.Person) > maxKeyBytes || len(raw) > maxProfileBytes {
		return fmt.Errorf("profile exceeds storage bounds")
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO dokja_profiles.profiles(person, generated_at, profile) VALUES($1, $2, $3)
		ON CONFLICT (person) DO UPDATE SET generated_at = EXCLUDED.generated_at, profile = EXCLUDED.profile`,
		profile.Person, profile.GeneratedAt.UTC(), string(raw)); err != nil {
		return safeError(ctx)
	}
	return nil
}

// AllProfiles gives every kept profile. A row that no longer reads is skipped, not fatal.
func (s *PostgresStore) AllProfiles(ctx context.Context) ([]conversation.Profile, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT profile::text FROM dokja_profiles.profiles ORDER BY person`)
	if err != nil {
		return nil, safeError(ctx)
	}
	defer rows.Close()
	found := []conversation.Profile{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, safeError(ctx)
		}
		var profile conversation.Profile
		if utf8.ValidString(raw) && json.Unmarshal([]byte(raw), &profile) == nil && profile.Person != "" {
			found = append(found, profile)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, safeError(ctx)
	}
	return found, nil
}

// NoteSession remembers that the stored session under key is the person's. The latest note wins.
func (s *PostgresStore) NoteSession(ctx context.Context, key, person string) error {
	if key == "" || person == "" || len(key) > maxKeyBytes || len(person) > maxKeyBytes {
		return fmt.Errorf("invalid person session")
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO dokja_profiles.person_sessions(scope_key, person) VALUES($1, $2)
		ON CONFLICT (scope_key) DO UPDATE SET person = EXCLUDED.person`, key, person); err != nil {
		return safeError(ctx)
	}
	return nil
}

// PersonSessions maps each noted session key to its person.
func (s *PostgresStore) PersonSessions(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT scope_key, person FROM dokja_profiles.person_sessions`)
	if err != nil {
		return nil, safeError(ctx)
	}
	defer rows.Close()
	found := map[string]string{}
	for rows.Next() {
		var key, person string
		if err := rows.Scan(&key, &person); err != nil {
			return nil, safeError(ctx)
		}
		found[key] = person
	}
	if err := rows.Err(); err != nil {
		return nil, safeError(ctx)
	}
	return found, nil
}
