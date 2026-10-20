package spike

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"strings"

	_ "modernc.org/sqlite"
)

// SQLite is the baseline: the filters and the mention join run in SQL, and the vector scan
// over the eligible chunks runs in Go, as Knowledge scans in application code today. It
// has no vector index, so it serves Exact only.
type SQLite struct {
	path string
	db   *sql.DB
}

func NewSQLite(path string) *SQLite { return &SQLite{path: path} }

func (s *SQLite) Name() string  { return "sqlite" }
func (s *SQLite) Modes() []Mode { return []Mode{Exact} }

const sqliteSchema = `
DROP TABLE IF EXISTS chunks;
DROP TABLE IF EXISTS mentions;
DROP TABLE IF EXISTS docs;
CREATE TABLE docs (
  id TEXT NOT NULL, version INTEGER NOT NULL, current INTEGER NOT NULL,
  scope TEXT NOT NULL, form TEXT NOT NULL, kind TEXT NOT NULL, date_anchor TEXT,
  PRIMARY KEY (id, version)
);
CREATE TABLE mentions (doc_id TEXT NOT NULL, version INTEGER NOT NULL, entity TEXT NOT NULL);
CREATE INDEX mentions_entity ON mentions (entity, doc_id, version);
CREATE TABLE chunks (id TEXT PRIMARY KEY, doc_id TEXT NOT NULL, version INTEGER NOT NULL, vec BLOB NOT NULL);
CREATE INDEX chunks_doc ON chunks (doc_id, version);
`

func (s *SQLite) Reset(ctx context.Context, _ int) error {
	if s.db == nil {
		db, err := sql.Open("sqlite", s.path+"?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)")
		if err != nil {
			return err
		}
		db.SetMaxOpenConns(1)
		s.db = db
	}
	_, err := s.db.ExecContext(ctx, sqliteSchema)
	return err
}

func (s *SQLite) Load(ctx context.Context, f Fixture) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, d := range f.Docs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO docs VALUES (?, ?, ?, ?, ?, ?, NULLIF(?, ''))`,
			d.ID, d.Version, d.Current, d.Scope, d.Form, d.Kind, d.DateAnchor); err != nil {
			return err
		}
		for _, e := range d.Mentions {
			if _, err := tx.ExecContext(ctx, `INSERT INTO mentions VALUES (?, ?, ?)`, d.ID, d.Version, e); err != nil {
				return err
			}
		}
	}
	for _, c := range f.Chunks {
		if _, err := tx.ExecContext(ctx, `INSERT INTO chunks VALUES (?, ?, ?, ?)`, c.ID, c.DocID, c.Version, encodeVec(c.Vec)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLite) Delete(ctx context.Context, docID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, stmt := range []string{`DELETE FROM chunks WHERE doc_id = ?`, `DELETE FROM mentions WHERE doc_id = ?`, `DELETE FROM docs WHERE id = ?`} {
		if _, err := tx.ExecContext(ctx, stmt, docID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLite) BuildIndex(context.Context) error { return nil }

func (s *SQLite) Query(ctx context.Context, q Query, _ Mode) ([]Hit, error) {
	where, args := []string{"m.entity = ?", "d.scope = ?", "d.current = 1"}, []any{q.Entity, q.Scope}
	if q.Form != "" {
		where, args = append(where, "d.form = ?"), append(args, q.Form)
	}
	if q.Kind != "" {
		where, args = append(where, "d.kind = ?"), append(args, q.Kind)
	}
	if q.DateFrom != "" {
		where, args = append(where, "d.date_anchor BETWEEN ? AND ?"), append(args, q.DateFrom, q.DateTo)
	}
	// The conditions are fixed strings; every value is a bound parameter.
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
SELECT c.id, c.doc_id, c.version, d.scope, c.vec
FROM mentions m
JOIN docs d ON d.id = m.doc_id AND d.version = m.version
JOIN chunks c ON c.doc_id = d.id AND c.version = d.version
WHERE %s`, strings.Join(where, " AND ")), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var hits []Hit
	for rows.Next() {
		var h Hit
		var blob []byte
		if err := rows.Scan(&h.ChunkID, &h.DocID, &h.Version, &h.Scope, &blob); err != nil {
			return nil, err
		}
		h.Distance = CosineDistance(q.Vec, decodeVec(blob))
		hits = append(hits, h)
	}
	SortHits(hits)
	if len(hits) > q.K {
		hits = hits[:q.K]
	}
	return hits, rows.Err()
}

func (s *SQLite) Describe(ctx context.Context) map[string]string {
	var version string
	_ = s.db.QueryRowContext(ctx, `SELECT sqlite_version()`).Scan(&version)
	var pages, size int64
	_ = s.db.QueryRowContext(ctx, `PRAGMA page_count`).Scan(&pages)
	_ = s.db.QueryRowContext(ctx, `PRAGMA page_size`).Scan(&size)
	return map[string]string{"engine": "SQLite " + version + " (modernc.org/sqlite, in process)",
		"vector_search": "exact scan in Go over SQL-filtered chunks", "disk_bytes": fmt.Sprint(pages * size)}
}

func (s *SQLite) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

func encodeVec(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(x))
	}
	return b
}

func decodeVec(b []byte) []float32 {
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return v
}
