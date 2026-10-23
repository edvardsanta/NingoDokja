package multihop

import (
	"context"
	"database/sql"
	"github.com/jackc/pgx/v5"
	_ "modernc.org/sqlite"
)

type SQLite struct{ db *sql.DB }

func NewSQLite(ctx context.Context, g Graph) (*SQLite, error) {
	g, err := normalized(g)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &SQLite{db: db}
	fail := func(err error) (*SQLite, error) { _ = db.Close(); return nil, err }
	_, err = db.ExecContext(ctx, `CREATE TABLE w2_nodes(id INTEGER PRIMARY KEY, scope TEXT NOT NULL, eligible INTEGER NOT NULL);
CREATE TABLE w2_edges(source INTEGER NOT NULL,target INTEGER NOT NULL,kind TEXT NOT NULL,PRIMARY KEY(source,target,kind)) WITHOUT ROWID;`)
	if err != nil {
		return fail(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fail(err)
	}
	defer tx.Rollback()
	for _, n := range g.Nodes {
		if _, err = tx.ExecContext(ctx, `INSERT INTO w2_nodes VALUES(?,?,?)`, n.ID, n.Scope, n.Current && !n.Deleted); err != nil {
			return fail(err)
		}
	}
	for _, e := range g.Edges {
		if _, err = tx.ExecContext(ctx, `INSERT INTO w2_edges VALUES(?,?,?)`, e.From, e.To, e.Kind); err != nil {
			return fail(err)
		}
	}
	if err = tx.Commit(); err != nil {
		return fail(err)
	}
	return s, nil
}
func (s *SQLite) Root(ctx context.Context, id int, scope string) (bool, error) {
	var yes bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM w2_nodes WHERE id=? AND scope=? AND eligible)`, id, scope).Scan(&yes)
	return yes, err
}
func (s *SQLite) Expand(ctx context.Context, id int, scope string, c Cursor, limit int) ([]Candidate, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT e.target,e.kind,(n.scope=? AND n.eligible) FROM w2_edges e JOIN w2_nodes n ON n.id=e.target
WHERE e.source=? AND (NOT ? OR e.target>? OR (e.target=? AND e.kind>?)) ORDER BY e.target,e.kind LIMIT ?`, scope, id, c.Set, c.To, c.To, c.Kind, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Candidate{}
	for rows.Next() {
		var e Candidate
		if err = rows.Scan(&e.To, &e.Kind, &e.Eligible); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
func (s *SQLite) Close() error { return s.db.Close() }

type Postgres struct{ conn *pgx.Conn }

// NewPostgres creates only connection-local temporary tables, never application
// tables or extensions. Closing the connection discards the entire fixture.
func NewPostgres(ctx context.Context, dsn string, g Graph) (*Postgres, error) {
	g, err := normalized(g)
	if err != nil {
		return nil, err
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, err
	}
	p := &Postgres{conn: conn}
	fail := func(err error) (*Postgres, error) { _ = conn.Close(context.Background()); return nil, err }
	_, err = conn.Exec(ctx, `CREATE TEMP TABLE w2_nodes(id bigint PRIMARY KEY,scope text NOT NULL,eligible boolean NOT NULL);
CREATE TEMP TABLE w2_edges(source bigint NOT NULL,target bigint NOT NULL,kind text NOT NULL,PRIMARY KEY(source,target,kind));`)
	if err != nil {
		return fail(err)
	}
	nodes := make([][]any, 0, len(g.Nodes))
	for _, n := range g.Nodes {
		nodes = append(nodes, []any{int64(n.ID), n.Scope, n.Current && !n.Deleted})
	}
	if _, err = conn.CopyFrom(ctx, pgx.Identifier{"w2_nodes"}, []string{"id", "scope", "eligible"}, pgx.CopyFromRows(nodes)); err != nil {
		return fail(err)
	}
	edges := make([][]any, 0, len(g.Edges))
	for _, e := range g.Edges {
		edges = append(edges, []any{int64(e.From), int64(e.To), e.Kind})
	}
	if _, err = conn.CopyFrom(ctx, pgx.Identifier{"w2_edges"}, []string{"source", "target", "kind"}, pgx.CopyFromRows(edges)); err != nil {
		return fail(err)
	}
	if _, err = conn.Exec(ctx, `ANALYZE w2_nodes; ANALYZE w2_edges`); err != nil {
		return fail(err)
	}
	return p, nil
}
func (p *Postgres) Root(ctx context.Context, id int, scope string) (bool, error) {
	var yes bool
	err := p.conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM w2_nodes WHERE id=$1 AND scope=$2 AND eligible)`, id, scope).Scan(&yes)
	return yes, err
}
func (p *Postgres) Expand(ctx context.Context, id int, scope string, c Cursor, limit int) ([]Candidate, error) {
	rows, err := p.conn.Query(ctx, `SELECT e.target,e.kind,(n.scope=$1 AND n.eligible) FROM w2_edges e JOIN w2_nodes n ON n.id=e.target
WHERE e.source=$2 AND (NOT $3::boolean OR e.target>$4 OR (e.target=$4 AND e.kind>$5)) ORDER BY e.target,e.kind LIMIT $6`, scope, id, c.Set, c.To, c.Kind, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Candidate{}
	for rows.Next() {
		var e Candidate
		if err = rows.Scan(&e.To, &e.Kind, &e.Eligible); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
func (p *Postgres) Close() error { return p.conn.Close(context.Background()) }
