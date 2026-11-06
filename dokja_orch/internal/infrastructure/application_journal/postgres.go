// Package applicationjournal implements an optional PostgreSQL journal port.
// Construction is explicit; no runtime configuration or public writer is added.
package applicationjournal

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	application "read_books/dokja_domain/dokja_memory/application"
)

var (
	ErrStorage       = errors.New("application journal storage failed")
	ErrSchema        = errors.New("application journal schema is unsupported or uninitialized")
	ErrForgotten     = errors.New("application journal record was forgotten")
	ErrCommitUnknown = errors.New("application journal commit outcome is unknown; replay the original operation")
)

const schemaVersion = 1

type Store struct{ db *sql.DB }

var _ application.JournalStore = (*Store)(nil)
var _ application.UnsettledReader = (*Store)(nil)

func safeError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var state interface{ SQLState() string }
	if errors.As(err, &state) && state.SQLState() == "23505" {
		return application.ErrJournalConflict
	}
	return ErrStorage // Never expose native SQL diagnostics, payloads or credentials.
}
func lockKey(scope string) int64 {
	sum := sha256.Sum256([]byte("dokja.application.journal.v1\x00" + scope))
	return int64(binary.BigEndian.Uint64(sum[:8]))
}
func validRef(s string) bool {
	if s == "" || len(s) > 256 || strings.TrimSpace(s) != s || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func checkVersion(ctx context.Context, tx *sql.Tx) error {
	var version int
	if err := tx.QueryRowContext(ctx, `SELECT version FROM dokja_application_journal.schema_version WHERE id=1 FOR SHARE`).Scan(&version); err != nil {
		return ErrSchema
	}
	if version != schemaVersion {
		return ErrSchema
	}
	return nil
}

// Open checks an existing namespace. It never creates or upgrades tables.
// The caller owns connection configuration, TLS, authentication and DB lifetime.
func Open(ctx context.Context, db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, ErrStorage
	}
	var version int
	if err := db.QueryRowContext(ctx, `SELECT version FROM dokja_application_journal.schema_version WHERE id=1`).Scan(&version); err != nil {
		return nil, ErrSchema
	}
	if version != schemaVersion {
		return nil, ErrSchema
	}
	return &Store{db: db}, nil
}

// Initialize installs only this namespace in the explicitly supplied database.
// It is not called by the application entry point or by Open.
func Initialize(ctx context.Context, db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, ErrStorage
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, safeError(ctx, err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, lockKey("schema")); err != nil {
		return nil, safeError(ctx, err)
	}
	if _, err = tx.ExecContext(ctx, `CREATE SCHEMA IF NOT EXISTS dokja_application_journal;
 CREATE TABLE IF NOT EXISTS dokja_application_journal.schema_version(id SMALLINT PRIMARY KEY CHECK(id=1),version INTEGER NOT NULL);`); err != nil {
		return nil, safeError(ctx, err)
	}
	var version int
	err = tx.QueryRowContext(ctx, `SELECT version FROM dokja_application_journal.schema_version WHERE id=1 FOR UPDATE`).Scan(&version)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, safeError(ctx, err)
	}
	if err == nil && version != schemaVersion {
		return nil, ErrSchema
	}
	if errors.Is(err, sql.ErrNoRows) {
		if _, err = tx.ExecContext(ctx, `CREATE TABLE dokja_application_journal.episodes(
   scope_ref TEXT NOT NULL,episode_ref TEXT NOT NULL,attempt_ref TEXT NOT NULL,
   revision BIGINT NOT NULL CHECK(revision>0),forgotten BOOLEAN NOT NULL DEFAULT FALSE,
   PRIMARY KEY(scope_ref,episode_ref),UNIQUE(scope_ref,attempt_ref));
  CREATE TABLE dokja_application_journal.revisions(
   scope_ref TEXT NOT NULL,episode_ref TEXT NOT NULL,revision BIGINT NOT NULL CHECK(revision>0),
   snapshot BYTEA NOT NULL CHECK(octet_length(snapshot)<=65536),
   PRIMARY KEY(scope_ref,episode_ref,revision),
   FOREIGN KEY(scope_ref,episode_ref) REFERENCES dokja_application_journal.episodes(scope_ref,episode_ref));
  CREATE TABLE dokja_application_journal.operations(
   scope_ref TEXT NOT NULL,operation_key TEXT NOT NULL,episode_ref TEXT NOT NULL,
   request_digest TEXT,result_revision BIGINT,forgotten BOOLEAN NOT NULL DEFAULT FALSE,
   PRIMARY KEY(scope_ref,operation_key),
   CHECK((forgotten AND request_digest IS NULL AND result_revision IS NULL) OR
    (NOT forgotten AND request_digest IS NOT NULL AND result_revision IS NOT NULL)),
   FOREIGN KEY(scope_ref,episode_ref) REFERENCES dokja_application_journal.episodes(scope_ref,episode_ref),
   FOREIGN KEY(scope_ref,episode_ref,result_revision) REFERENCES dokja_application_journal.revisions(scope_ref,episode_ref,revision));
  INSERT INTO dokja_application_journal.schema_version(id,version) VALUES(1,1);`); err != nil {
			return nil, safeError(ctx, err)
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, ErrCommitUnknown
	}
	return &Store{db: db}, nil
}

func (s *Store) begin(ctx context.Context, scope string) (*sql.Tx, error) {
	if s == nil || s.db == nil || !validRef(scope) {
		return nil, ErrStorage
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, safeError(ctx, err)
	}
	if err = checkVersion(ctx, tx); err != nil {
		tx.Rollback()
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, lockKey("scope:"+scope)); err != nil {
		tx.Rollback()
		return nil, safeError(ctx, err)
	}
	return tx, nil
}
func decode(raw []byte) (application.Episode, error) {
	var e application.Episode
	if len(raw) == 0 || len(raw) > application.MaxRecordBytes {
		return e, ErrStorage
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&e); err != nil {
		return e, ErrStorage
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return e, ErrStorage
	}
	if application.ValidateEpisode(e) != nil {
		return e, ErrStorage
	}
	return e, nil
}
func snapshot(ctx context.Context, tx *sql.Tx, scope, episode string, revision int64) (application.Episode, error) {
	var raw []byte
	if err := tx.QueryRowContext(ctx, `SELECT snapshot FROM dokja_application_journal.revisions WHERE scope_ref=$1 AND episode_ref=$2 AND revision=$3`, scope, episode, revision).Scan(&raw); err != nil {
		return application.Episode{}, safeError(ctx, err)
	}
	e, err := decode(raw)
	if err != nil {
		return e, err
	}
	if e.Scope != scope || e.Ref != episode || e.Revision != uint64(revision) {
		return application.Episode{}, ErrStorage
	}
	return e, nil
}
func same(a, b application.Episode) bool {
	left, err := json.Marshal(a)
	if err != nil {
		return false
	}
	right, err := json.Marshal(b)
	return err == nil && bytes.Equal(left, right)
}

func (s *Store) Transact(ctx context.Context, k application.JournalKey, fn func(application.JournalState) (application.JournalWrite, error)) (application.JournalResult, error) {
	empty := application.JournalResult{}
	if !validRef(k.EpisodeRef) || !validRef(k.AttemptRef) || !validRef(k.OperationKey) || fn == nil {
		return empty, ErrStorage
	}
	tx, err := s.begin(ctx, k.Scope)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	state := application.JournalState{}
	var revision int64
	var forgotten bool
	var attempt string
	err = tx.QueryRowContext(ctx, `SELECT revision,forgotten,attempt_ref FROM dokja_application_journal.episodes WHERE scope_ref=$1 AND episode_ref=$2`, k.Scope, k.EpisodeRef).Scan(&revision, &forgotten, &attempt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return empty, safeError(ctx, err)
	}
	if err == nil {
		if forgotten {
			return empty, ErrForgotten
		}
		e, err := snapshot(ctx, tx, k.Scope, k.EpisodeRef, revision)
		if err != nil {
			return empty, err
		}
		if e.Attempt.Ref != attempt {
			return empty, ErrStorage
		}
		state.Current = &e
	}
	err = tx.QueryRowContext(ctx, `SELECT episode_ref FROM dokja_application_journal.episodes WHERE scope_ref=$1 AND attempt_ref=$2`, k.Scope, k.AttemptRef).Scan(&state.AttemptOwner)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return empty, safeError(ctx, err)
	}
	var operationEpisode string
	var operationDigest sql.NullString
	var operationRevision sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT episode_ref,request_digest,result_revision,forgotten FROM dokja_application_journal.operations WHERE scope_ref=$1 AND operation_key=$2`, k.Scope, k.OperationKey).Scan(&operationEpisode, &operationDigest, &operationRevision, &forgotten)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return empty, safeError(ctx, err)
	}
	if err == nil {
		if forgotten {
			return empty, ErrForgotten
		}
		if !operationDigest.Valid || !operationRevision.Valid {
			return empty, ErrStorage
		}
		result, err := snapshot(ctx, tx, k.Scope, operationEpisode, operationRevision.Int64)
		if err != nil {
			return empty, err
		}
		state.Operation = &application.JournalOperation{Digest: operationDigest.String, Result: result}
	}
	// Callback inputs are detached from these copies used for adapter validation.
	original := copyState(state)
	write, err := fn(state)
	if err != nil {
		return empty, err
	}
	if err = validateWrite(k, original, write); err != nil {
		return empty, err
	}
	if write.NextSnapshot != nil {
		e := *write.NextSnapshot
		if original.Current == nil {
			_, err = tx.ExecContext(ctx, `INSERT INTO dokja_application_journal.episodes(scope_ref,episode_ref,attempt_ref,revision) VALUES($1,$2,$3,$4)`, k.Scope, k.EpisodeRef, k.AttemptRef, int64(e.Revision))
		} else {
			var result sql.Result
			result, err = tx.ExecContext(ctx, `UPDATE dokja_application_journal.episodes SET revision=$1 WHERE scope_ref=$2 AND episode_ref=$3 AND revision=$4 AND NOT forgotten`, int64(e.Revision), k.Scope, k.EpisodeRef, int64(original.Current.Revision))
			if err == nil {
				count, e := result.RowsAffected()
				if e != nil || count != 1 {
					return empty, application.ErrJournalConflict
				}
			}
		}
		if err != nil {
			return empty, safeError(ctx, err)
		}
		raw, _ := json.Marshal(e)
		if _, err = tx.ExecContext(ctx, `INSERT INTO dokja_application_journal.revisions(scope_ref,episode_ref,revision,snapshot) VALUES($1,$2,$3,$4)`, k.Scope, k.EpisodeRef, int64(e.Revision), raw); err != nil {
			return empty, safeError(ctx, err)
		}
	}
	if write.Operation != nil {
		op := write.Operation
		if _, err = tx.ExecContext(ctx, `INSERT INTO dokja_application_journal.operations(scope_ref,operation_key,episode_ref,request_digest,result_revision) VALUES($1,$2,$3,$4,$5)`, k.Scope, k.OperationKey, k.EpisodeRef, op.Digest, int64(op.Result.Revision)); err != nil {
			return empty, safeError(ctx, err)
		}
	}
	if err = ctx.Err(); err != nil {
		return empty, err
	}
	if err = tx.Commit(); err != nil {
		return empty, ErrCommitUnknown
	}
	return copyResult(write.Result), nil
}

func copyState(s application.JournalState) application.JournalState {
	raw, _ := json.Marshal(s)
	var result application.JournalState
	_ = json.Unmarshal(raw, &result)
	return result
}
func copyResult(r application.JournalResult) application.JournalResult {
	raw, _ := json.Marshal(r)
	var result application.JournalResult
	_ = json.Unmarshal(raw, &result)
	return result
}
func validateWrite(k application.JournalKey, s application.JournalState, w application.JournalWrite) error {
	if s.Operation != nil {
		if w.NextSnapshot != nil || w.Operation != nil || !w.Result.Replayed || w.Result.Changed || !same(w.Result.Episode, s.Operation.Result) {
			return ErrStorage
		}
		return nil
	}
	if w.Operation == nil || w.Result.Replayed || w.Result.Changed != (w.NextSnapshot != nil) {
		return ErrStorage
	}
	e := s.Current
	if w.NextSnapshot != nil {
		e = w.NextSnapshot
		expected := uint64(1)
		if s.Current != nil {
			expected = s.Current.Revision + 1
		}
		if e.Revision != expected {
			return ErrStorage
		}
	}
	if e == nil || application.ValidateEpisode(*e) != nil || e.Revision > math.MaxInt64 || e.Scope != k.Scope || e.Ref != k.EpisodeRef || e.Attempt.Ref != k.AttemptRef {
		return ErrStorage
	}
	if !same(w.Operation.Result, *e) || !same(w.Result.Episode, *e) {
		return ErrStorage
	}
	raw, _ := json.Marshal(e)
	if len(raw) > application.MaxRecordBytes {
		return ErrStorage
	}
	digest := w.Operation.Digest
	if !strings.HasPrefix(digest, "sha256:") || len(digest) != 71 {
		return ErrStorage
	}
	if _, err := hex.DecodeString(digest[7:]); err != nil {
		return ErrStorage
	}
	return nil
}

// Read is an internal storage operation, not an authorization or public read API.
func (s *Store) Read(ctx context.Context, scope, episode string) (application.Episode, bool, error) {
	if !validRef(episode) {
		return application.Episode{}, false, ErrStorage
	}
	tx, err := s.begin(ctx, scope)
	if err != nil {
		return application.Episode{}, false, err
	}
	defer tx.Rollback()
	var revision int64
	var forgotten bool
	err = tx.QueryRowContext(ctx, `SELECT revision,forgotten FROM dokja_application_journal.episodes WHERE scope_ref=$1 AND episode_ref=$2`, scope, episode).Scan(&revision, &forgotten)
	if errors.Is(err, sql.ErrNoRows) {
		return application.Episode{}, false, nil
	}
	if err != nil {
		return application.Episode{}, false, safeError(ctx, err)
	}
	if forgotten {
		return application.Episode{}, false, ErrForgotten
	}
	e, err := snapshot(ctx, tx, scope, episode, revision)
	return e, err == nil, err
}

// Forget removes every content-bearing revision and replay result reference.
// Minimal opaque fences remain: old operation/episode/attempt identities cannot
// resurrect deleted content. The application must authorize deletion separately.
func (s *Store) Forget(ctx context.Context, scope, episode string) error {
	if !validRef(episode) {
		return ErrStorage
	}
	tx, err := s.begin(ctx, scope)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE dokja_application_journal.operations SET forgotten=TRUE,request_digest=NULL,result_revision=NULL WHERE scope_ref=$1 AND episode_ref=$2`, scope, episode); err != nil {
		return safeError(ctx, err)
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM dokja_application_journal.revisions WHERE scope_ref=$1 AND episode_ref=$2`, scope, episode); err != nil {
		return safeError(ctx, err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE dokja_application_journal.episodes SET forgotten=TRUE WHERE scope_ref=$1 AND episode_ref=$2`, scope, episode); err != nil {
		return safeError(ctx, err)
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return ErrCommitUnknown
	}
	return nil
}

// unsettledPage is how many current revisions recovery reads at a time.
const unsettledPage = 200

// Unsettled lists the current snapshot of every attempt still prepared or dispatched and accepted
// before the instant, across scopes, for startup recovery, at most limit of them. It only reads,
// without scope locks: the journal then changes each one by compare-and-swap at the revision read
// here, so one settled meanwhile is left alone. Forgotten episodes are never listed. The phase is
// read from each decoded snapshot, never by casting stored bytes in SQL, so a snapshot that cannot
// be read is counted in unreadable and skipped instead of stopping recovery of the others.
func (s *Store) Unsettled(ctx context.Context, before time.Time, limit int) ([]application.Episode, int, error) {
	var out []application.Episode
	unreadable := 0
	afterScope, afterEpisode := "", ""
	for len(out) < limit {
		rows, err := s.db.QueryContext(ctx, `SELECT r.scope_ref, r.episode_ref, r.revision, r.snapshot
 FROM dokja_application_journal.episodes e
 JOIN dokja_application_journal.revisions r
  ON r.scope_ref=e.scope_ref AND r.episode_ref=e.episode_ref AND r.revision=e.revision
 WHERE NOT e.forgotten AND (e.scope_ref, e.episode_ref) > ($1, $2)
 ORDER BY e.scope_ref, e.episode_ref
 LIMIT $3`, afterScope, afterEpisode, unsettledPage)
		if err != nil {
			return nil, unreadable, safeError(ctx, err)
		}
		read := 0
		for rows.Next() {
			var scope, episode string
			var revision int64
			var raw []byte
			if err := rows.Scan(&scope, &episode, &revision, &raw); err != nil {
				rows.Close()
				return nil, unreadable, safeError(ctx, err)
			}
			read++
			afterScope, afterEpisode = scope, episode
			e, err := decode(raw)
			if err != nil || e.Scope != scope || e.Ref != episode || e.Revision != uint64(revision) {
				unreadable++
				continue
			}
			if (e.Attempt.Phase == "prepared" || e.Attempt.Phase == "dispatched") && e.Attempt.AcceptedAt.Before(before) && len(out) < limit {
				out = append(out, e)
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, unreadable, safeError(ctx, err)
		}
		if read < unsettledPage {
			break
		}
	}
	return out, unreadable, nil
}
