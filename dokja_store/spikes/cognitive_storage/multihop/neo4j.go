package multihop

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"time"
)

type Neo4j struct {
	driver neo4j.DriverWithContext
	run    string
}

// NewNeo4j namespaces every node to a random run. Only that run is removed on
// Close. One named composite index is retained; use a disposable database.
func NewNeo4j(ctx context.Context, uri string, g Graph) (*Neo4j, error) {
	g, err := normalized(g)
	if err != nil {
		return nil, err
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	driver, err := neo4j.NewDriverWithContext(uri, neo4j.NoAuth())
	if err != nil {
		return nil, err
	}
	n := &Neo4j{driver: driver, run: hex.EncodeToString(nonce[:])}
	fail := func(err error) (*Neo4j, error) { _ = n.Close(); return nil, err }
	if _, err = n.query(ctx, `CREATE INDEX dokja_w2_run_id IF NOT EXISTS FOR (n:DokjaW2) ON (n.run,n.id)`, nil); err != nil {
		return fail(err)
	}
	if _, err = n.query(ctx, `CALL db.awaitIndexes(60)`, nil); err != nil {
		return fail(err)
	}
	nodes := []any{}
	for _, v := range g.Nodes {
		nodes = append(nodes, map[string]any{"id": v.ID, "scope": v.Scope, "eligible": v.Current && !v.Deleted})
	}
	for i := 0; i < len(nodes); i += 2000 {
		if _, err = n.query(ctx, `UNWIND $rows AS r CREATE (:DokjaW2 {run:$run,id:r.id,scope:r.scope,eligible:r.eligible})`, map[string]any{"run": n.run, "rows": nodes[i:min(i+2000, len(nodes))]}); err != nil {
			return fail(err)
		}
	}
	edges := []any{}
	for _, e := range g.Edges {
		edges = append(edges, map[string]any{"source": e.From, "target": e.To, "kind": e.Kind})
	}
	for i := 0; i < len(edges); i += 2000 {
		if _, err = n.query(ctx, `UNWIND $rows AS r MATCH (s:DokjaW2 {run:$run,id:r.source}),(t:DokjaW2 {run:$run,id:r.target}) CREATE (s)-[:W2_EDGE {kind:r.kind}]->(t)`, map[string]any{"run": n.run, "rows": edges[i:min(i+2000, len(edges))]}); err != nil {
			return fail(err)
		}
	}
	return n, nil
}
func (n *Neo4j) query(ctx context.Context, q string, args map[string]any) (*neo4j.EagerResult, error) {
	return neo4j.ExecuteQuery(ctx, n.driver, q, args, neo4j.EagerResultTransformer)
}
func (n *Neo4j) Root(ctx context.Context, id int, scope string) (bool, error) {
	result, err := n.query(ctx, `MATCH (n:DokjaW2 {run:$run,id:$id}) WHERE n.scope=$scope AND n.eligible RETURN n.id LIMIT 1`, map[string]any{"run": n.run, "id": id, "scope": scope})
	if err != nil {
		return false, err
	}
	return len(result.Records) > 0, nil
}
func (n *Neo4j) Expand(ctx context.Context, id int, scope string, c Cursor, limit int) ([]Candidate, error) {
	result, err := n.query(ctx, `MATCH (s:DokjaW2 {run:$run,id:$id})-[e:W2_EDGE]->(t:DokjaW2 {run:$run})
WHERE NOT $after OR t.id>$to OR (t.id=$to AND e.kind>$kind)
RETURN t.id AS id,e.kind AS kind,(t.scope=$scope AND t.eligible) AS eligible ORDER BY id,kind LIMIT $limit`, map[string]any{"run": n.run, "id": id, "scope": scope, "after": c.Set, "to": c.To, "kind": c.Kind, "limit": limit})
	if err != nil {
		return nil, err
	}
	out := []Candidate{}
	for _, r := range result.Records {
		m := r.AsMap()
		out = append(out, Candidate{To: int(m["id"].(int64)), Kind: m["kind"].(string), Eligible: m["eligible"].(bool)})
	}
	return out, nil
}
func (n *Neo4j) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var cleanupErr error
	for {
		result, err := n.query(ctx, `MATCH (n:DokjaW2 {run:$run}) WITH n LIMIT 2000 DETACH DELETE n RETURN count(*) AS removed`, map[string]any{"run": n.run})
		if err != nil {
			cleanupErr = err
			break
		}
		if len(result.Records) == 0 || result.Records[0].AsMap()["removed"].(int64) == 0 {
			break
		}
	}
	closeErr := n.driver.Close(ctx)
	if cleanupErr != nil {
		return cleanupErr
	}
	return closeErr
}
