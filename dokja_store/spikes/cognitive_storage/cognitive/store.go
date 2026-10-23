package cognitive

import (
	"context"
	"encoding/json"
	"errors"
	inference "read_books/dokja_domain/dokja_inference"
	"reflect"
	"sync"

	"github.com/jackc/pgx/v5"
)

// Fixture is intentionally small and semantic labels are synthetic. Only the
// root is a vector seed; subsequent keys must be discovered in prior evidence.
func Fixture() []Node {
	return []Node{
		{Ref: Ref{"document", "1", "fixture:document"}, Scope: "owner", Kind: "document", Current: true, Requires: []string{"format"}},
		{Ref: Ref{"format", "1", "fixture:format"}, Scope: "owner", Kind: "concept", Current: true, Requires: []string{"rule"}},
		{Ref: Ref{"rule", "1", "fixture:policy"}, Scope: "owner", Kind: "rule", Current: true, Facts: map[string]bool{"sandbox_allowed": true}, Question: "sample"},
		{Ref: Ref{"sample", "1", "fixture:sample"}, Scope: "owner", Kind: "observation", Current: true, Facts: map[string]bool{"sample_verified": true}},
	}
}

type Memory struct {
	mu       sync.Mutex
	Nodes    []Node
	Episodes []Episode
}

func NewMemory(nodes []Node) *Memory { return &Memory{Nodes: clone(nodes)} }
func (m *Memory) Search(ctx context.Context, scope string) (Node, error) {
	return m.Get(ctx, scope, "document")
}
func (m *Memory) Get(ctx context.Context, scope, id string) (Node, error) {
	if err := ctx.Err(); err != nil {
		return Node{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, n := range m.Nodes {
		if n.ID == id && valid(n, scope) {
			return clone(n), nil
		}
	}
	return Node{}, missing(id)
}
func (m *Memory) Recall(ctx context.Context, scope, key string) ([]Episode, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Episode{}
	for _, e := range m.Episodes {
		if e.Scope == scope && e.Context == key && e.Simulation {
			out = append(out, clone(e))
		}
	}
	return out, nil
}
func (m *Memory) Record(ctx context.Context, e Episode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !recordable(e) {
		return errors.New("invalid observation")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, old := range m.Episodes {
		if old.ID == e.ID {
			if reflect.DeepEqual(old, e) {
				return nil
			}
			return errors.New("attempt already recorded differently")
		}
	}
	m.Episodes = append(m.Episodes, clone(e))
	return nil
}
func recordable(e Episode) bool {
	return inference.ValidDiagnostic(e.Outcome, e.Diagnostic) && e.ID != "" && e.Scope != "" && e.Context != "" && e.Simulation && (e.Outcome == "success" || e.Outcome == "failed") && (e.Action == "fast" || e.Action == "normalize") && len(e.Evidence) > 0
}

// Postgres uses connection-local temporary tables. Existing application tables
// are never reset or consulted. Episodes survive tasks, not connection closure.
type Postgres struct{ conn *pgx.Conn }

func NewPostgres(ctx context.Context, dsn string, nodes []Node) (*Postgres, error) {
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, err
	}
	p := &Postgres{conn: conn}
	fail := func(err error) (*Postgres, error) { _ = conn.Close(ctx); return nil, err }
	// The experiment requires an already installed vector extension; no persistent
	// schema or extension mutation is performed by the harness.
	_, err = conn.Exec(ctx, `CREATE TEMP TABLE cognitive_nodes(id text PRIMARY KEY, scope text NOT NULL, eligible boolean NOT NULL, body jsonb NOT NULL, embedding vector(3));
CREATE TEMP TABLE cognitive_episodes(id text PRIMARY KEY, scope text NOT NULL, context text NOT NULL, body jsonb NOT NULL);`)
	if err != nil {
		return fail(err)
	}
	for _, n := range nodes {
		body, err := json.Marshal(n)
		if err != nil {
			return fail(err)
		}
		vec := "[0,1,0]"
		if n.ID == "document" {
			vec = "[1,0,0]"
		}
		_, err = conn.Exec(ctx, `INSERT INTO cognitive_nodes VALUES($1,$2,$3,$4::jsonb,$5::vector)`, n.ID, n.Scope, n.Current && !n.Deleted, body, vec)
		if err != nil {
			return fail(err)
		}
	}
	return p, nil
}
func (p *Postgres) Close(ctx context.Context) error { return p.conn.Close(ctx) }
func (p *Postgres) Search(ctx context.Context, scope string) (Node, error) {
	var body []byte
	err := p.conn.QueryRow(ctx, `SELECT body FROM cognitive_nodes WHERE scope=$1 AND eligible ORDER BY embedding <=> '[1,0,0]'::vector,id LIMIT 1`, scope).Scan(&body)
	var n Node
	if err == nil {
		err = json.Unmarshal(body, &n)
	}
	return n, err
}
func (p *Postgres) Get(ctx context.Context, scope, id string) (Node, error) {
	var body []byte
	err := p.conn.QueryRow(ctx, `SELECT body FROM cognitive_nodes WHERE scope=$1 AND id=$2 AND eligible`, scope, id).Scan(&body)
	var n Node
	if err == nil {
		err = json.Unmarshal(body, &n)
	}
	return n, err
}
func (p *Postgres) Recall(ctx context.Context, scope, key string) ([]Episode, error) {
	rows, err := p.conn.Query(ctx, `SELECT body FROM cognitive_episodes WHERE scope=$1 AND context=$2 ORDER BY id`, scope, key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Episode{}
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			return nil, err
		}
		var e Episode
		if err := json.Unmarshal(b, &e); err != nil {
			return nil, err
		}
		if e.Simulation {
			out = append(out, e)
		}
	}
	return out, rows.Err()
}
func (p *Postgres) Record(ctx context.Context, e Episode) error {
	if !recordable(e) {
		return errors.New("invalid observation")
	}
	body, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = p.conn.Exec(ctx, `INSERT INTO cognitive_episodes VALUES($1,$2,$3,$4::jsonb) ON CONFLICT(id) DO NOTHING`, e.ID, e.Scope, e.Context, body)
	if err != nil {
		return err
	}
	var same bool
	err = p.conn.QueryRow(ctx, `SELECT body=$2::jsonb FROM cognitive_episodes WHERE id=$1`, e.ID, body).Scan(&same)
	if err != nil {
		return err
	}
	if !same {
		return errors.New("attempt already recorded differently")
	}
	return nil
}
