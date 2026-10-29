// Package conversations keeps the owner's conversations with Ningo in a SQLite file of its own,
// beside the shared database and opened by the orchestrator alone. It is storage only: what may be
// stored, read or used as context is decided in dokja_domain/dokja_conversation.
//
// A transcript is the owner's own text: nothing in this package logs it.
package conversations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	conversation "read_books/dokja_domain/dokja_conversation"

	_ "modernc.org/sqlite"
)

// FileName is the archive's file, kept in the same directory as the shared database.
const FileName = "dokja_conversations.db"

// PathBeside gives the archive's path next to the shared database file.
func PathBeside(sharedDB string) string {
	return filepath.Join(filepath.Dir(sharedDB), FileName)
}

// Times are kept as Unix nanoseconds, which order as numbers do.
var migrations = []string{
	`CREATE TABLE conversations (
		id         TEXT PRIMARY KEY,
		scope      TEXT NOT NULL,
		kind       TEXT NOT NULL,
		reading_id TEXT NOT NULL DEFAULT '',
		title      TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL,
		revision   INTEGER NOT NULL,
		turns      INTEGER NOT NULL DEFAULT 0
	);
	CREATE INDEX conversations_recent ON conversations(scope, updated_at DESC, id DESC);
	CREATE TABLE turns (
		id                TEXT PRIMARY KEY,
		conversation_id   TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
		seq               INTEGER NOT NULL,
		send_id           TEXT NOT NULL,
		content           TEXT NOT NULL,
		reply             TEXT NOT NULL DEFAULT '',
		state             TEXT NOT NULL,
		reason            TEXT NOT NULL DEFAULT '',
		attempts          INTEGER NOT NULL DEFAULT 1,
		selected_provider TEXT NOT NULL DEFAULT '',
		selected_model    TEXT NOT NULL DEFAULT '',
		observed_provider TEXT NOT NULL DEFAULT '',
		observed_model    TEXT NOT NULL DEFAULT '',
		created_at        INTEGER NOT NULL,
		finished_at       INTEGER NOT NULL DEFAULT 0,
		UNIQUE(conversation_id, seq),
		UNIQUE(conversation_id, send_id)
	);
	CREATE INDEX turns_pending ON turns(state, created_at);`,
}

type Store struct{ db *sql.DB }

var _ conversation.Store = (*Store)(nil)

// Open creates the archive if needed, readable by its owner only, and applies pending migrations. A
// file from a newer build is refused rather than read wrongly.
func Open(path string) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("conversation archive path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create conversation archive dir: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open conversation archive: %w", err)
	}
	file.Close()
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("open conversation archive: %w", err)
	}
	// One connection: appends to a conversation are serialized, so turn order is decided once.
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read conversation archive version: %w", err)
	}
	if version > len(migrations) {
		return fmt.Errorf("conversation archive is version %d, newer than this build understands (%d)", version, len(migrations))
	}
	for i := version; i < len(migrations); i++ {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("conversation archive migration %d: %w", i+1, err)
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

func scanConversation(row interface{ Scan(...any) error }) (conversation.Conversation, error) {
	var c conversation.Conversation
	var created, updated int64
	var kind string
	if err := row.Scan(&c.ID, &kind, &c.ReadingID, &c.Title, &created, &updated, &c.Revision, &c.Turns); err != nil {
		return c, err
	}
	c.Kind, c.CreatedAt, c.UpdatedAt = conversation.Kind(kind), time.Unix(0, created).UTC(), time.Unix(0, updated).UTC()
	return c, nil
}

func (s *Store) Create(ctx context.Context, scope conversation.Scope, c conversation.Conversation) (conversation.Conversation, error) {
	_, err := s.db.ExecContext(ctx, `INSERT INTO conversations (id, scope, kind, reading_id, title, created_at, updated_at, revision, turns)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		c.ID, string(scope), string(c.Kind), c.ReadingID, c.Title, c.CreatedAt.UnixNano(), c.UpdatedAt.UnixNano(), c.Revision)
	if err != nil {
		return conversation.Conversation{}, fmt.Errorf("store conversation: %w", err)
	}
	c.Turns = 0
	return c, nil
}

func (s *Store) Get(ctx context.Context, scope conversation.Scope, id string) (conversation.Conversation, error) {
	c, err := scanConversation(s.db.QueryRowContext(ctx, `SELECT id, kind, reading_id, title, created_at, updated_at, revision, turns FROM conversations WHERE id = ? AND scope = ?`, id, string(scope)))
	if errors.Is(err, sql.ErrNoRows) {
		return conversation.Conversation{}, conversation.ErrNotFound
	}
	return c, err
}

// The cursor is the last item's change time and id, so a page never repeats or skips an item that
// did not change meanwhile.
func (s *Store) List(ctx context.Context, scope conversation.Scope, cursor string, limit int) ([]conversation.Conversation, string, error) {
	// Without a cursor the page starts above every possible key.
	nanos, id := int64(math.MaxInt64), "g"
	if cursor != "" {
		at, last, ok := strings.Cut(cursor, ".")
		parsed, err := strconv.ParseInt(at, 10, 64)
		if !ok || err != nil {
			return nil, "", conversation.ErrInvalid
		}
		nanos, id = parsed, last
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, kind, reading_id, title, created_at, updated_at, revision, turns FROM conversations
		WHERE scope = ? AND (updated_at < ? OR (updated_at = ? AND id < ?))
		ORDER BY updated_at DESC, id DESC LIMIT ?`, string(scope), nanos, nanos, id, limit+1)
	if err != nil {
		return nil, "", fmt.Errorf("list conversations: %w", err)
	}
	defer rows.Close()
	items := []conversation.Conversation{}
	for rows.Next() {
		c, err := scanConversation(rows)
		if err != nil {
			return nil, "", err
		}
		items = append(items, c)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		last := items[limit-1]
		next = fmt.Sprintf("%d.%s", last.UpdatedAt.UnixNano(), last.ID)
	}
	return items, next, nil
}

func (s *Store) Rename(ctx context.Context, scope conversation.Scope, id string, revision uint64, title string, at time.Time) (conversation.Conversation, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE conversations SET title = ?, revision = revision + 1, updated_at = ?
		WHERE id = ? AND scope = ? AND revision = ?`, title, at.UnixNano(), id, string(scope), revision)
	if err != nil {
		return conversation.Conversation{}, fmt.Errorf("rename conversation: %w", err)
	}
	if err := s.changed(ctx, result, scope, id); err != nil {
		return conversation.Conversation{}, err
	}
	return s.Get(ctx, scope, id)
}

func (s *Store) Delete(ctx context.Context, scope conversation.Scope, id string, revision uint64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM conversations WHERE id = ? AND scope = ? AND revision = ?`, id, string(scope), revision)
	if err != nil {
		return fmt.Errorf("delete conversation: %w", err)
	}
	return s.changed(ctx, result, scope, id)
}

// changed tells a write that touched nothing apart: a missing conversation, or one at another
// revision.
func (s *Store) changed(ctx context.Context, result sql.Result, scope conversation.Scope, id string) error {
	if n, err := result.RowsAffected(); err != nil || n == 1 {
		return err
	}
	if _, err := s.Get(ctx, scope, id); err != nil {
		return err
	}
	return conversation.ErrConflict
}

func scanTurn(row interface{ Scan(...any) error }) (conversation.Turn, error) {
	var t conversation.Turn
	var state string
	var created, finished int64
	err := row.Scan(&t.ID, &t.ConversationID, &t.Seq, &t.SendID, &t.Content, &t.Reply, &state, &t.Reason, &t.Attempts,
		&t.Provenance.SelectedProvider, &t.Provenance.SelectedModel, &t.Provenance.ObservedProvider, &t.Provenance.ObservedModel, &created, &finished)
	if err != nil {
		return t, err
	}
	t.State, t.CreatedAt = conversation.TurnState(state), time.Unix(0, created).UTC()
	if finished > 0 {
		t.FinishedAt = time.Unix(0, finished).UTC()
	}
	return t, nil
}

// BeginTurn appends in one transaction: the send ID is looked up, the next position taken and the
// conversation's revision moved, so a retried send finds its turn and two sends never share a place.
func (s *Store) BeginTurn(ctx context.Context, scope conversation.Scope, t conversation.Turn) (conversation.Turn, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return conversation.Turn{}, false, err
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM conversations WHERE id = ? AND scope = ?`, t.ConversationID, string(scope)).Scan(&exists); err != nil {
		return conversation.Turn{}, false, err
	}
	if exists == 0 {
		return conversation.Turn{}, false, conversation.ErrNotFound
	}
	earlier, err := scanTurn(tx.QueryRowContext(ctx, `SELECT id, conversation_id, seq, send_id, content, reply, state, reason, attempts, selected_provider, selected_model, observed_provider, observed_model, created_at, finished_at FROM turns WHERE conversation_id = ? AND send_id = ?`, t.ConversationID, t.SendID))
	if err == nil {
		// One send ID names one message; other text under it is an interface's mistake, not a retry.
		if earlier.Content != t.Content {
			return conversation.Turn{}, false, conversation.ErrConflict
		}
		return earlier, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return conversation.Turn{}, false, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) + 1 FROM turns WHERE conversation_id = ?`, t.ConversationID).Scan(&t.Seq); err != nil {
		return conversation.Turn{}, false, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO turns (id, conversation_id, seq, send_id, content, state, selected_provider, selected_model, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.ConversationID, t.Seq, t.SendID, t.Content, string(t.State), t.Provenance.SelectedProvider, t.Provenance.SelectedModel, t.CreatedAt.UnixNano()); err != nil {
		return conversation.Turn{}, false, fmt.Errorf("store turn: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE conversations SET turns = turns + 1, revision = revision + 1, updated_at = ? WHERE id = ?`, t.CreatedAt.UnixNano(), t.ConversationID); err != nil {
		return conversation.Turn{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return conversation.Turn{}, false, err
	}
	return t, false, nil
}

// FinishTurn ends only the named pending attempt. The attempt comparison is in
// the update, so an old executor cannot settle a turn reopened by RetryTurn.
func (s *Store) FinishTurn(ctx context.Context, scope conversation.Scope, t conversation.Turn) (conversation.Turn, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE turns SET reply = ?, state = ?, reason = ?, observed_provider = ?, observed_model = ?, finished_at = ?
		WHERE id = ? AND attempts = ? AND state = 'pending' AND conversation_id IN (SELECT id FROM conversations WHERE scope = ?)`,
		t.Reply, string(t.State), t.Reason, t.Provenance.ObservedProvider, t.Provenance.ObservedModel, t.FinishedAt.UnixNano(), t.ID, t.Attempts, string(scope))
	if err != nil {
		return conversation.Turn{}, fmt.Errorf("finish turn: %w", err)
	}
	return s.reread(ctx, scope, t.ID, result)
}

// RetryTurn reopens the conversation's latest turn when it failed or was interrupted. The condition
// is in the update itself, so two retries of the same turn cannot both win.
func (s *Store) RetryTurn(ctx context.Context, scope conversation.Scope, turnID string, at time.Time) (conversation.Turn, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE turns SET state = 'pending', reply = '', reason = '', observed_provider = '', observed_model = '',
			finished_at = 0, attempts = attempts + 1
		WHERE id = ? AND state IN ('failed', 'interrupted')
			AND conversation_id IN (SELECT id FROM conversations WHERE scope = ?)
			AND seq = (SELECT MAX(seq) FROM turns AS latest WHERE latest.conversation_id = turns.conversation_id)`,
		turnID, string(scope))
	if err != nil {
		return conversation.Turn{}, fmt.Errorf("retry turn: %w", err)
	}
	if n, _ := result.RowsAffected(); n == 1 {
		if _, err := s.db.ExecContext(ctx, `UPDATE conversations SET revision = revision + 1, updated_at = ?
			WHERE id = (SELECT conversation_id FROM turns WHERE id = ?)`, at.UnixNano(), turnID); err != nil {
			return conversation.Turn{}, err
		}
	}
	return s.reread(ctx, scope, turnID, result)
}

// reread gives back the turn after a conditional update: ErrNotFound when the scope has no such
// turn, and the stored turn with ErrConflict when the update's condition did not hold.
func (s *Store) reread(ctx context.Context, scope conversation.Scope, turnID string, result sql.Result) (conversation.Turn, error) {
	stored, err := scanTurn(s.db.QueryRowContext(ctx, `SELECT id, conversation_id, seq, send_id, content, reply, state, reason, attempts, selected_provider, selected_model, observed_provider, observed_model, created_at, finished_at FROM turns
		WHERE id = ? AND conversation_id IN (SELECT id FROM conversations WHERE scope = ?)`, turnID, string(scope)))
	if errors.Is(err, sql.ErrNoRows) {
		return conversation.Turn{}, conversation.ErrNotFound
	}
	if err != nil {
		return conversation.Turn{}, err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return stored, conversation.ErrConflict
	}
	return stored, nil
}

func (s *Store) Turns(ctx context.Context, scope conversation.Scope, conversationID string, beforeSeq, limit int) ([]conversation.Turn, error) {
	if _, err := s.Get(ctx, scope, conversationID); err != nil {
		return nil, err
	}
	// Without a position the page starts after the last possible place.
	if beforeSeq <= 0 {
		beforeSeq = math.MaxInt
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, conversation_id, seq, send_id, content, reply, state, reason, attempts, selected_provider, selected_model, observed_provider, observed_model, created_at, finished_at FROM turns
		WHERE conversation_id = ? AND seq < ? ORDER BY seq DESC LIMIT ?`, conversationID, beforeSeq, limit)
	if err != nil {
		return nil, fmt.Errorf("read turns: %w", err)
	}
	defer rows.Close()
	turns := []conversation.Turn{}
	for rows.Next() {
		t, err := scanTurn(rows)
		if err != nil {
			return nil, err
		}
		turns = append(turns, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, j := 0, len(turns)-1; i < j; i, j = i+1, j-1 {
		turns[i], turns[j] = turns[j], turns[i]
	}
	return turns, nil
}

func (s *Store) InterruptPending(ctx context.Context, before time.Time) (int, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE turns SET state = 'interrupted', reason = ?, finished_at = ? WHERE state = 'pending' AND created_at < ?`,
		"the orchestrator stopped before the reply", before.UnixNano(), before.UnixNano())
	if err != nil {
		return 0, fmt.Errorf("recover pending turns: %w", err)
	}
	n, err := result.RowsAffected()
	return int(n), err
}
