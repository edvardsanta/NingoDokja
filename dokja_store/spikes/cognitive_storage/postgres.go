package spike

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ApproxIterative is pgvector's HNSW search with iterative scans, which keeps reading the
// index until the filters let K rows through (pgvector 0.8).
const ApproxIterative Mode = "approx_iterative"

// EfSearch is the HNSW candidate list for approximate searches in every engine that has
// one, so their recall is compared at the same breadth.
const EfSearch = 100

// Postgres keeps documents, mentions and chunks in tables and searches vectors with
// pgvector, exactly or through an HNSW index.
type Postgres struct {
	dsn  string
	pool *pgxpool.Pool
	dim  int
}

func NewPostgres(dsn string) *Postgres { return &Postgres{dsn: dsn} }

func (p *Postgres) Name() string  { return "postgres" }
func (p *Postgres) Modes() []Mode { return []Mode{Exact, Approx, ApproxIterative} }

func (p *Postgres) Reset(ctx context.Context, dim int) error {
	if p.pool == nil {
		pool, err := pgxpool.New(ctx, p.dsn)
		if err != nil {
			return err
		}
		p.pool = pool
	}
	p.dim = dim
	_, err := p.pool.Exec(ctx, fmt.Sprintf(`
CREATE EXTENSION IF NOT EXISTS vector;
DROP TABLE IF EXISTS chunks, mentions, docs;
CREATE TABLE docs (
  id text NOT NULL, version int NOT NULL, current boolean NOT NULL,
  scope text NOT NULL, form text NOT NULL, kind text NOT NULL, date_anchor date,
  PRIMARY KEY (id, version)
);
CREATE TABLE mentions (doc_id text NOT NULL, version int NOT NULL, entity text NOT NULL);
CREATE INDEX mentions_entity ON mentions (entity, doc_id, version);
CREATE TABLE chunks (id text PRIMARY KEY, doc_id text NOT NULL, version int NOT NULL, vec vector(%d) NOT NULL);
CREATE INDEX chunks_doc ON chunks (doc_id, version);`, dim))
	return err
}

func (p *Postgres) Load(ctx context.Context, f Fixture) error {
	docs := make([][]any, 0, len(f.Docs))
	var mentions [][]any
	for _, d := range f.Docs {
		var anchor any
		if d.DateAnchor != "" {
			anchor = d.DateAnchor
		}
		docs = append(docs, []any{d.ID, d.Version, d.Current, d.Scope, d.Form, d.Kind, anchor})
		for _, e := range d.Mentions {
			mentions = append(mentions, []any{d.ID, d.Version, e})
		}
	}
	if _, err := p.pool.CopyFrom(ctx, pgx.Identifier{"docs"},
		[]string{"id", "version", "current", "scope", "form", "kind", "date_anchor"}, pgx.CopyFromRows(docs)); err != nil {
		return fmt.Errorf("copy docs: %w", err)
	}
	if _, err := p.pool.CopyFrom(ctx, pgx.Identifier{"mentions"}, []string{"doc_id", "version", "entity"}, pgx.CopyFromRows(mentions)); err != nil {
		return fmt.Errorf("copy mentions: %w", err)
	}
	// Vectors go through a text staging column, cast once, so no custom type codec is needed.
	// A temporary table belongs to one connection, so the whole load holds one.
	conn, err := p.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `CREATE TEMP TABLE chunk_stage (id text, doc_id text, version int, vec text)`); err != nil {
		return err
	}
	rows := make([][]any, 0, len(f.Chunks))
	for _, c := range f.Chunks {
		rows = append(rows, []any{c.ID, c.DocID, c.Version, vectorLiteral(c.Vec)})
	}
	if _, err := conn.CopyFrom(ctx, pgx.Identifier{"chunk_stage"}, []string{"id", "doc_id", "version", "vec"}, pgx.CopyFromRows(rows)); err != nil {
		return fmt.Errorf("copy chunks: %w", err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO chunks SELECT id, doc_id, version, vec::vector FROM chunk_stage; DROP TABLE chunk_stage; ANALYZE`); err != nil {
		return err
	}
	return nil
}

func (p *Postgres) Delete(ctx context.Context, docID string) error {
	return pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		for _, stmt := range []string{`DELETE FROM chunks WHERE doc_id = $1`, `DELETE FROM mentions WHERE doc_id = $1`, `DELETE FROM docs WHERE id = $1`} {
			if _, err := tx.Exec(ctx, stmt, docID); err != nil {
				return err
			}
		}
		return nil
	})
}

func (p *Postgres) BuildIndex(ctx context.Context) error {
	_, err := p.pool.Exec(ctx, `CREATE INDEX chunks_vec ON chunks USING hnsw (vec vector_cosine_ops) WITH (m = 16, ef_construction = 64); ANALYZE chunks`)
	return err
}

// w1SQL builds the query; only fixed condition strings are joined, and every value is bound.
func (p *Postgres) w1SQL(q Query, mode Mode) (string, []any) {
	args := []any{vectorLiteral(q.Vec), q.Entity, q.Scope, q.K}
	where := []string{"d.scope = $3", "d.current"}
	if q.Form != "" {
		args = append(args, q.Form)
		where = append(where, "d.form = $"+strconv.Itoa(len(args)))
	}
	if q.Kind != "" {
		args = append(args, q.Kind)
		where = append(where, "d.kind = $"+strconv.Itoa(len(args)))
	}
	if q.DateFrom != "" {
		args = append(args, q.DateFrom, q.DateTo)
		where = append(where, fmt.Sprintf("d.date_anchor BETWEEN $%d::date AND $%d::date", len(args)-1, len(args)))
	}
	// Exact orders by an expression the HNSW index cannot serve, so every eligible chunk is
	// scanned, with the chunk ID breaking ties. Approximate modes order by the indexed
	// operator alone: any other sort key would keep the planner from using the index.
	order := "(c.vec <=> $1::vector) + 0, c.id"
	if mode != Exact {
		order = "c.vec <=> $1::vector"
	}
	return fmt.Sprintf(`
SELECT c.id, c.doc_id, c.version, d.scope, (c.vec <=> $1::vector)::float8 AS distance
FROM chunks c
JOIN docs d ON d.id = c.doc_id AND d.version = c.version
WHERE EXISTS (SELECT 1 FROM mentions m WHERE m.doc_id = d.id AND m.version = d.version AND m.entity = $2)
  AND %s
ORDER BY %s
LIMIT $4`, strings.Join(where, " AND "), order), args
}

func (p *Postgres) settings(mode Mode) string {
	switch mode {
	case Approx:
		return fmt.Sprintf("SET LOCAL hnsw.ef_search = %d; SET LOCAL hnsw.iterative_scan = off", EfSearch)
	case ApproxIterative:
		return fmt.Sprintf("SET LOCAL hnsw.ef_search = %d; SET LOCAL hnsw.iterative_scan = strict_order", EfSearch)
	}
	return "SET LOCAL hnsw.iterative_scan = off"
}

func (p *Postgres) Query(ctx context.Context, q Query, mode Mode) ([]Hit, error) {
	var hits []Hit
	err := pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, p.settings(mode)); err != nil {
			return err
		}
		query, args := p.w1SQL(q, mode)
		rows, err := tx.Query(ctx, query, args...)
		if err != nil {
			return err
		}
		hits, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Hit, error) {
			var h Hit
			err := row.Scan(&h.ChunkID, &h.DocID, &h.Version, &h.Scope, &h.Distance)
			return h, err
		})
		return err
	})
	return hits, err
}

// Plan returns EXPLAIN for one query, to show which modes really use the vector index.
func (p *Postgres) Plan(ctx context.Context, q Query, mode Mode) (string, error) {
	var lines []string
	err := pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, p.settings(mode)); err != nil {
			return err
		}
		query, args := p.w1SQL(q, mode)
		rows, err := tx.Query(ctx, "EXPLAIN "+query, args...)
		if err != nil {
			return err
		}
		lines, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	})
	// The query vector is a long literal; the plan's shape is what matters.
	return vectorInPlan.ReplaceAllString(strings.Join(lines, "\n"), "'$$vec'::vector"), err
}

var vectorInPlan = regexp.MustCompile(`'\[[^\]]*\]'::vector`)

func (p *Postgres) Describe(ctx context.Context) map[string]string {
	out := map[string]string{}
	var server, pgvector string
	var size int64
	_ = p.pool.QueryRow(ctx, `SHOW server_version`).Scan(&server)
	_ = p.pool.QueryRow(ctx, `SELECT extversion FROM pg_extension WHERE extname = 'vector'`).Scan(&pgvector)
	_ = p.pool.QueryRow(ctx, `SELECT pg_database_size(current_database())`).Scan(&size)
	out["engine"] = "PostgreSQL " + server + " + pgvector " + pgvector
	out["vector_index"] = fmt.Sprintf("hnsw vector_cosine_ops m=16 ef_construction=64; ef_search=%d", EfSearch)
	out["disk_bytes"] = fmt.Sprint(size)
	return out
}

func (p *Postgres) Close() error {
	if p.pool != nil {
		p.pool.Close()
	}
	return nil
}

func vectorLiteral(v []float32) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, x := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(x), 'g', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}
