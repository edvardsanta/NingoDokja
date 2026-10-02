// Package memory stores experiences ("in this context the bot took this action, and this was
// the outcome") and finds the closest earlier ones by meaning.
//
// It is storage and similarity only. What a prediction is, and how it is scored, are business
// rules and live in dokja_domain/dokja_memory. A context is the user's own text: nothing in
// this package logs it.
package memory

import (
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// ExpiredOutcome is the one outcome the service assigns itself: an experience nobody resolved
// within the expiry window. It is a view over unresolved rows, not a write, so a late verdict
// can still replace it.
const ExpiredOutcome = "expired"

const timeLayout = time.RFC3339

var migrations = []string{
	`CREATE TABLE experiences (
		id          INTEGER PRIMARY KEY,
		ref         TEXT NOT NULL UNIQUE,
		action      TEXT NOT NULL,
		context     TEXT NOT NULL,
		detail      TEXT NOT NULL DEFAULT '',
		embedding   BLOB,
		embed_model TEXT,
		predicted_p REAL,
		baseline_p  REAL,
		outcome     TEXT,
		created_at  TEXT NOT NULL,
		resolved_at TEXT
	);
	CREATE INDEX experiences_by_action ON experiences(action, outcome);`,
	// What the bot's choice rested on: how close the earlier example was, and the start of its text.
	// Both stay empty for an experience recorded without them, so older rows read as before.
	`ALTER TABLE experiences ADD COLUMN matched_score REAL;
	ALTER TABLE experiences ADD COLUMN matched_context TEXT;`,
}

// Store is the experiences database. It runs on one connection, so every call is serial.
type Store struct {
	db          *sql.DB
	now         func() time.Time
	expireAfter time.Duration
}

// OpenStore opens (creating if needed) the database at path and applies pending migrations.
// The file is created readable by its owner only, since it holds the user's messages.
// expireAfter is how long an unresolved experience waits for a verdict; zero never expires.
func OpenStore(path string, expireAfter time.Duration) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("database path is empty (set MEMORY_DB_FILE)")
	}
	dsn := path
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, fmt.Errorf("create database dir: %w", err)
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			return nil, fmt.Errorf("open database file: %w", err)
		}
		file.Close()
		dsn = "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(1)

	store := &Store{db: db, now: time.Now, expireAfter: expireAfter}
	if err := store.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("read database version: %w", err)
	}
	if version > len(migrations) {
		return fmt.Errorf("database is version %d, newer than this build understands (%d)", version, len(migrations))
	}
	for i := version; i < len(migrations); i++ {
		tx, err := s.db.Begin()
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

func (s *Store) stamp() string { return s.now().UTC().Format(timeLayout) }

// cutoff is the creation time before which an unresolved experience counts as expired. With
// no expiry it is empty, which no timestamp is earlier than.
func (s *Store) cutoff() string {
	if s.expireAfter <= 0 {
		return ""
	}
	return s.now().UTC().Add(-s.expireAfter).Format(timeLayout)
}

// verdictSQL is the outcome of a row as readers see it: its own, or "expired" when it has
// none and is older than the cutoff (the parameter), or NULL while it is still pending.
const verdictSQL = "COALESCE(outcome, CASE WHEN created_at < ? THEN '" + ExpiredOutcome + "' END)"

// ---- writing --------------------------------------------------------------------------

// NewExperience is what the bot knew when it acted. Predicted and Baseline are the chances it
// stated at that moment; they are stored once and never recomputed, so a score reflects what
// was known at the time.
type NewExperience struct {
	Ref     string
	Action  string
	Context string
	// Detail is what exactly the bot did (the label it suggested, say), so a later
	// observation can be compared with it. The context is the situation; this is the answer.
	Detail    string
	Predicted *float64
	Baseline  *float64
	// MatchedScore and MatchedContext are what the choice rested on: how close the earlier example
	// was (0 to 1) and the start of its text. They are the user's own text, so the context follows
	// the same rule as Context: it only leaves the store as a snippet when a listing asks for it.
	MatchedScore   *float64
	MatchedContext string
}

// Exists reports whether an experience with this ref is already stored.
func (s *Store) Exists(ref string) (bool, error) {
	var found int
	err := s.db.QueryRow("SELECT 1 FROM experiences WHERE ref = ?", ref).Scan(&found)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

// Insert stores a new experience, with its embedding when there is one. An experience is
// immutable: a ref that is already stored is left exactly as it was, and created is false.
func (s *Store) Insert(e NewExperience, vector []float32, model string) (created bool, err error) {
	var blob any
	var embedModel any
	if vector != nil {
		blob, embedModel = encodeVector(vector), model
	}
	var matchedContext any
	if e.MatchedContext != "" {
		matchedContext = e.MatchedContext
	}
	result, err := s.db.Exec(
		`INSERT INTO experiences(ref, action, context, detail, embedding, embed_model, predicted_p, baseline_p,
		                         matched_score, matched_context, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(ref) DO NOTHING`,
		e.Ref, e.Action, e.Context, e.Detail, blob, embedModel, nullFloat(e.Predicted), nullFloat(e.Baseline),
		nullFloat(e.MatchedScore), matchedContext, s.stamp())
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected > 0, err
}

// Resolve records the outcome of an experience. The first verdict wins: resolving again
// reports what is already there and changes nothing. found is false for an unknown ref.
func (s *Store) Resolve(ref, outcome string) (found, resolved bool, current string, err error) {
	var existing sql.NullString
	err = s.db.QueryRow("SELECT outcome FROM experiences WHERE ref = ?", ref).Scan(&existing)
	if err == sql.ErrNoRows {
		return false, false, "", nil
	}
	if err != nil {
		return false, false, "", err
	}
	if existing.Valid {
		return true, false, existing.String, nil
	}
	_, err = s.db.Exec("UPDATE experiences SET outcome = ?, resolved_at = ? WHERE ref = ? AND outcome IS NULL",
		outcome, s.stamp(), ref)
	return true, err == nil, outcome, err
}

// Stored is one experience as it can be read back. The context is left out on purpose: it is
// the user's own text and nothing that reads an experience back needs it.
type Stored struct {
	Ref       string
	Action    string
	Detail    string
	Outcome   string
	Predicted *float64
	Baseline  *float64
	// MatchedScore is how close the earlier example the bot's choice rested on was, when it was
	// recorded. Its text is not part of this: see MatchedSnippets.
	MatchedScore *float64
	CreatedAt    string
	ResolvedAt   string
}

// Get reads one experience. Outcome is empty while it is pending and "expired" once it has
// waited past the expiry window with no verdict. ok is false for an unknown ref.
func (s *Store) Get(ref string) (stored Stored, ok bool, err error) {
	var predicted, baseline, matched sql.NullFloat64
	var outcome, resolvedAt sql.NullString
	err = s.db.QueryRow(
		"SELECT ref, action, detail, "+verdictSQL+", predicted_p, baseline_p, matched_score, created_at, resolved_at FROM experiences WHERE ref = ?",
		s.cutoff(), ref).Scan(&stored.Ref, &stored.Action, &stored.Detail, &outcome, &predicted, &baseline, &matched, &stored.CreatedAt, &resolvedAt)
	if err == sql.ErrNoRows {
		return Stored{}, false, nil
	}
	if err != nil {
		return Stored{}, false, err
	}
	stored.Outcome, stored.ResolvedAt = outcome.String, resolvedAt.String
	if predicted.Valid {
		stored.Predicted = &predicted.Float64
	}
	if baseline.Valid {
		stored.Baseline = &baseline.Float64
	}
	if matched.Valid {
		stored.MatchedScore = &matched.Float64
	}
	return stored, true, nil
}

// Forget deletes an experience.
func (s *Store) Forget(ref string) (bool, error) {
	result, err := s.db.Exec("DELETE FROM experiences WHERE ref = ?", ref)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected > 0, err
}

// ---- reading --------------------------------------------------------------------------

// OutcomeCounts counts experiences by outcome, for one action or (when empty) all of them.
// Pending experiences are not counted: they have no outcome yet.
func (s *Store) OutcomeCounts(action string) (map[string]int, error) {
	rows, err := s.db.Query(
		"SELECT v, COUNT(*) FROM (SELECT "+verdictSQL+" AS v FROM experiences WHERE ? = '' OR action = ?) WHERE v IS NOT NULL GROUP BY v",
		s.cutoff(), action, action)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := map[string]int{}
	for rows.Next() {
		var outcome string
		var count int
		if err := rows.Scan(&outcome, &count); err != nil {
			return nil, err
		}
		counts[outcome] = count
	}
	return counts, rows.Err()
}

// Neighbor is an earlier experience that resembles a query.
type Neighbor struct {
	Ref        string
	Action     string
	Detail     string
	Outcome    string
	Similarity float64
	CreatedAt  string
}

// Nearest returns the k embedded experiences closest to the query vector (cosine similarity,
// vectors are unit length), best first. Only experiences with an outcome, possibly "expired",
// count: a pending one has taught nothing yet. Rows embedded by another model or with another
// dimension are skipped until a reindex. It scans every candidate, which is fine into the
// tens of thousands of experiences.
func (s *Store) Nearest(query []float32, action, model string, k int) ([]Neighbor, error) {
	cutoff := s.cutoff()
	rows, err := s.db.Query(
		`SELECT ref, action, detail, `+verdictSQL+`, created_at, embedding FROM experiences
		 WHERE embedding IS NOT NULL AND embed_model = ? AND (? = '' OR action = ?)
		   AND (outcome IS NOT NULL OR created_at < ?)`,
		cutoff, model, action, action, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	top := make([]Neighbor, 0, k+1)
	vector := make([]float32, len(query))
	for rows.Next() {
		var n Neighbor
		var blob []byte
		if err := rows.Scan(&n.Ref, &n.Action, &n.Detail, &n.Outcome, &n.CreatedAt, &blob); err != nil {
			return nil, err
		}
		if len(blob) != 4*len(query) {
			continue
		}
		decodeVector(blob, vector)
		n.Similarity = float64(dot(query, vector))
		top = insertTop(top, n, k)
	}
	return top, rows.Err()
}

func insertTop(top []Neighbor, n Neighbor, k int) []Neighbor {
	at := sort.Search(len(top), func(i int) bool { return top[i].Similarity < n.Similarity })
	if at >= k {
		return top
	}
	top = append(top, Neighbor{})
	copy(top[at+1:], top[at:])
	top[at] = n
	if len(top) > k {
		top = top[:k]
	}
	return top
}

// SnippetRunes is how much of a context a listing or a recall may hand back: enough to recognize
// the experience, never the whole text. The cut is made here, so the rest never leaves the store.
const SnippetRunes = 160

// Listed is one experience in a listing. Snippet is empty unless it was asked for.
type Listed struct {
	Stored
	Snippet string
}

// List returns one page of experiences, newest first, and how many match in all. state is "" (every
// experience), "pending" (no outcome yet) or "resolved" (an outcome, possibly "expired"): the same
// reading of expiry as everywhere else decides which is which. snippetRunes above zero adds the start
// of each context.
func (s *Store) List(action, state string, limit, offset, snippetRunes int) ([]Listed, int, error) {
	cutoff := s.cutoff()
	// With a snippet the select list carries one more placeholder, which sits between the cutoff and
	// the action filter.
	snippetExpr, snippetArgs := "''", []any(nil)
	if snippetRunes > 0 {
		snippetExpr, snippetArgs = "substr(context, 1, ?)", []any{snippetRunes}
	}
	base := "SELECT id, ref, action, detail, " + verdictSQL + " AS v, predicted_p, baseline_p, matched_score, created_at, resolved_at, " +
		snippetExpr + " AS snippet FROM experiences WHERE ? = '' OR action = ?"
	baseArgs := append(append([]any{cutoff}, snippetArgs...), action, action)

	outer := ""
	switch state {
	case "pending":
		outer = " WHERE v IS NULL"
	case "resolved":
		outer = " WHERE v IS NOT NULL"
	}

	var total int
	countBase := strings.Replace(base, snippetExpr+" AS snippet", "'' AS snippet", 1)
	countArgs := append([]any{cutoff}, action, action)
	if err := s.db.QueryRow("SELECT COUNT(*) FROM ("+countBase+")"+outer, countArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := s.db.Query(
		"SELECT ref, action, detail, v, predicted_p, baseline_p, matched_score, created_at, resolved_at, snippet FROM ("+base+")"+outer+
			" ORDER BY id DESC LIMIT ? OFFSET ?",
		append(baseArgs, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []Listed
	for rows.Next() {
		var row Listed
		var outcome, resolvedAt sql.NullString
		var predicted, baseline, matched sql.NullFloat64
		if err := rows.Scan(&row.Ref, &row.Action, &row.Detail, &outcome, &predicted, &baseline, &matched, &row.CreatedAt, &resolvedAt, &row.Snippet); err != nil {
			return nil, 0, err
		}
		row.Outcome, row.ResolvedAt = outcome.String, resolvedAt.String
		if predicted.Valid {
			row.Predicted = &predicted.Float64
		}
		if baseline.Valid {
			row.Baseline = &baseline.Float64
		}
		if matched.Valid {
			row.MatchedScore = &matched.Float64
		}
		row.Snippet = tidySnippet(row.Snippet)
		out = append(out, row)
	}
	return out, total, rows.Err()
}

// Snippets returns the start of the context of each given experience, keyed by ref.
func (s *Store) Snippets(refs []string, runes int) (map[string]string, error) {
	snippets := map[string]string{}
	if len(refs) == 0 || runes <= 0 {
		return snippets, nil
	}
	args := []any{runes}
	for _, ref := range refs {
		args = append(args, ref)
	}
	rows, err := s.db.Query(
		"SELECT ref, substr(context, 1, ?) FROM experiences WHERE ref IN (?"+strings.Repeat(",?", len(refs)-1)+")", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ref, snippet string
		if err := rows.Scan(&ref, &snippet); err != nil {
			return nil, err
		}
		snippets[ref] = tidySnippet(snippet)
	}
	return snippets, rows.Err()
}

// MatchedSnippets returns the start of the text of the earlier example each given experience rested
// on, keyed by ref. An experience recorded without one has no entry.
func (s *Store) MatchedSnippets(refs []string, runes int) (map[string]string, error) {
	snippets := map[string]string{}
	if len(refs) == 0 || runes <= 0 {
		return snippets, nil
	}
	args := []any{runes}
	for _, ref := range refs {
		args = append(args, ref)
	}
	rows, err := s.db.Query(
		"SELECT ref, substr(matched_context, 1, ?) FROM experiences WHERE matched_context IS NOT NULL AND ref IN (?"+
			strings.Repeat(",?", len(refs)-1)+")", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ref, snippet string
		if err := rows.Scan(&ref, &snippet); err != nil {
			return nil, err
		}
		snippets[ref] = tidySnippet(snippet)
	}
	return snippets, rows.Err()
}

// tidySnippet puts a snippet on one line, so a context with line breaks cannot spoil a listing.
func tidySnippet(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// ResolvedExperience is an experience with an outcome as the scorecard reads it. The chances
// are nil when the experience was recorded without a prediction.
type ResolvedExperience struct {
	Ref        string
	Action     string
	Outcome    string
	Predicted  *float64
	Baseline   *float64
	ResolvedAt string
}

// Resolved returns the latest `limit` experiences that have an outcome (possibly "expired"),
// oldest first, for one action or (when empty) all of them.
func (s *Store) Resolved(action string, limit int) ([]ResolvedExperience, error) {
	cutoff := s.cutoff()
	rows, err := s.db.Query(
		`SELECT ref, action, v, predicted_p, baseline_p, COALESCE(resolved_at, '') FROM (
		   SELECT id, ref, action, `+verdictSQL+` AS v, predicted_p, baseline_p, resolved_at FROM experiences
		   WHERE ? = '' OR action = ?
		 ) WHERE v IS NOT NULL ORDER BY id DESC LIMIT ?`,
		cutoff, action, action, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ResolvedExperience
	for rows.Next() {
		var row ResolvedExperience
		var predicted, baseline sql.NullFloat64
		if err := rows.Scan(&row.Ref, &row.Action, &row.Outcome, &predicted, &baseline, &row.ResolvedAt); err != nil {
			return nil, err
		}
		if predicted.Valid {
			row.Predicted = &predicted.Float64
		}
		if baseline.Valid {
			row.Baseline = &baseline.Float64
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// The query takes the newest rows; hand them back oldest first.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// ---- embeddings -----------------------------------------------------------------------

type PendingEmbedding struct {
	ID      int64
	Context string
}

// NeedingEmbedding returns experiences with no embedding or one made by another model.
func (s *Store) NeedingEmbedding(model string, limit int) ([]PendingEmbedding, error) {
	rows, err := s.db.Query(
		"SELECT id, context FROM experiences WHERE embedding IS NULL OR embed_model IS NOT ? ORDER BY id LIMIT ?",
		model, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PendingEmbedding
	for rows.Next() {
		var row PendingEmbedding
		if err := rows.Scan(&row.ID, &row.Context); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// SetEmbeddings stores vectors for the given experiences, all in one transaction.
func (s *Store) SetEmbeddings(ids []int64, vectors [][]float32, model string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	for i, id := range ids {
		if _, err := tx.Exec("UPDATE experiences SET embedding = ?, embed_model = ? WHERE id = ?",
			encodeVector(vectors[i]), model, id); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// Stats summarizes what is stored.
type Stats struct {
	Experiences int
	Pending     int
	Resolved    int
	Expired     int
	Embedded    int
	// NeedsReindex counts experiences with no embedding or one from another model.
	NeedsReindex int
}

func (s *Store) Stats(model string) (Stats, error) {
	var stats Stats
	err := s.db.QueryRow(
		`SELECT COUNT(*),
		        COALESCE(SUM(CASE WHEN v IS NULL THEN 1 ELSE 0 END), 0),
		        COALESCE(SUM(CASE WHEN v IS NOT NULL AND v <> '`+ExpiredOutcome+`' THEN 1 ELSE 0 END), 0),
		        COALESCE(SUM(CASE WHEN v = '`+ExpiredOutcome+`' THEN 1 ELSE 0 END), 0),
		        COALESCE(SUM(CASE WHEN embedding IS NOT NULL AND embed_model = ? THEN 1 ELSE 0 END), 0)
		 FROM (SELECT `+verdictSQL+` AS v, embedding, embed_model FROM experiences)`,
		model, s.cutoff()).Scan(&stats.Experiences, &stats.Pending, &stats.Resolved, &stats.Expired, &stats.Embedded)
	stats.NeedsReindex = stats.Experiences - stats.Embedded
	return stats, err
}

// ---- vectors --------------------------------------------------------------------------

func nullFloat(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func encodeVector(vector []float32) []byte {
	blob := make([]byte, 4*len(vector))
	for i, value := range vector {
		binary.LittleEndian.PutUint32(blob[4*i:], math.Float32bits(value))
	}
	return blob
}

func decodeVector(blob []byte, into []float32) {
	for i := range into {
		into[i] = math.Float32frombits(binary.LittleEndian.Uint32(blob[4*i:]))
	}
}

func dot(a, b []float32) float32 {
	var sum float32
	for i := range a {
		sum += a[i] * b[i]
	}
	return sum
}
