package spike

import (
	"context"
	"fmt"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// Neo4j keeps documents, entities and chunks as nodes: (:Doc)-[:MENTIONS]->(:Entity) and
// (:Doc)-[:HAS_CHUNK]->(:Chunk). Exact search computes similarity over the traversal;
// approximate search starts from the vector index and filters what it returns (its EXPLAIN
// puts ProcedureCall db.index.vector.queryNodes first; the driver's plan tree omitted that
// operator, so no plan is recorded from here).
type Neo4j struct {
	uri    string
	driver neo4j.DriverWithContext
	dim    int
}

func NewNeo4j(uri string) *Neo4j { return &Neo4j{uri: uri} }

func (n *Neo4j) Name() string  { return "neo4j" }
func (n *Neo4j) Modes() []Mode { return []Mode{Exact, Approx} }

const neo4jBatch = 2000

func (n *Neo4j) run(ctx context.Context, cypher string, params map[string]any) (*neo4j.EagerResult, error) {
	return neo4j.ExecuteQuery(ctx, n.driver, cypher, params, neo4j.EagerResultTransformer)
}

func (n *Neo4j) Reset(ctx context.Context, dim int) error {
	if n.driver == nil {
		driver, err := neo4j.NewDriverWithContext(n.uri, neo4j.NoAuth())
		if err != nil {
			return err
		}
		if err := driver.VerifyConnectivity(ctx); err != nil {
			return err
		}
		n.driver = driver
	}
	n.dim = dim
	session := n.driver.NewSession(ctx, neo4j.SessionConfig{})
	defer session.Close(ctx)
	// Batched deletion runs in an implicit transaction, which ExecuteQuery cannot open.
	result, err := session.Run(ctx, `MATCH (x) CALL (x) { DETACH DELETE x } IN TRANSACTIONS OF 10000 ROWS`, nil)
	if err == nil {
		_, err = result.Consume(ctx)
	}
	if err != nil {
		return fmt.Errorf("clear graph: %w", err)
	}
	for _, stmt := range []string{
		`DROP INDEX chunk_vec IF EXISTS`,
		`CREATE CONSTRAINT doc_key IF NOT EXISTS FOR (d:Doc) REQUIRE d.key IS UNIQUE`,
		`CREATE INDEX doc_id IF NOT EXISTS FOR (d:Doc) ON (d.id)`,
		`CREATE CONSTRAINT entity_id IF NOT EXISTS FOR (e:Entity) REQUIRE e.id IS UNIQUE`,
		`CREATE CONSTRAINT chunk_id IF NOT EXISTS FOR (c:Chunk) REQUIRE c.id IS UNIQUE`,
		`CALL db.awaitIndexes(300)`,
	} {
		if _, err := n.run(ctx, stmt, nil); err != nil {
			return fmt.Errorf("%s: %w", stmt, err)
		}
	}
	return nil
}

func (n *Neo4j) Load(ctx context.Context, f Fixture) error {
	docs := make([]any, 0, len(f.Docs))
	for _, d := range f.Docs {
		mentions := make([]any, len(d.Mentions))
		for i, e := range d.Mentions {
			mentions[i] = e
		}
		var anchor any
		if d.DateAnchor != "" {
			anchor = d.DateAnchor
		}
		docs = append(docs, map[string]any{"key": d.Key(), "id": d.ID, "version": d.Version, "current": d.Current,
			"scope": d.Scope, "form": d.Form, "kind": d.Kind, "date": anchor, "mentions": mentions})
	}
	for start := 0; start < len(docs); start += neo4jBatch {
		if _, err := n.run(ctx, `
UNWIND $rows AS r
CREATE (d:Doc {key: r.key, id: r.id, version: r.version, current: r.current, scope: r.scope,
               form: r.form, kind: r.kind, date_anchor: r.date})
WITH d, r
UNWIND r.mentions AS entity
MERGE (e:Entity {id: entity})
CREATE (d)-[:MENTIONS]->(e)`, map[string]any{"rows": docs[start:min(start+neo4jBatch, len(docs))]}); err != nil {
			return fmt.Errorf("load docs: %w", err)
		}
	}
	for start := 0; start < len(f.Chunks); start += neo4jBatch {
		batch := f.Chunks[start:min(start+neo4jBatch, len(f.Chunks))]
		rows := make([]any, len(batch))
		for i, c := range batch {
			rows[i] = map[string]any{"id": c.ID, "doc": docKey(c.DocID, c.Version), "vec": float64s(c.Vec)}
		}
		if _, err := n.run(ctx, `
UNWIND $rows AS r
MATCH (d:Doc {key: r.doc})
CREATE (d)-[:HAS_CHUNK]->(:Chunk {id: r.id, vec: r.vec})`, map[string]any{"rows": rows}); err != nil {
			return fmt.Errorf("load chunks: %w", err)
		}
	}
	return nil
}

func (n *Neo4j) Delete(ctx context.Context, docID string) error {
	_, err := n.run(ctx, `
MATCH (d:Doc {id: $id})
OPTIONAL MATCH (d)-[:HAS_CHUNK]->(c:Chunk)
DETACH DELETE c, d`, map[string]any{"id": docID})
	return err
}

func (n *Neo4j) BuildIndex(ctx context.Context) error {
	// The dimension is an int this program chose, not input; index options take no parameters.
	if _, err := n.run(ctx, fmt.Sprintf(`
CREATE VECTOR INDEX chunk_vec IF NOT EXISTS FOR (c:Chunk) ON (c.vec)
OPTIONS {indexConfig: {`+"`vector.dimensions`"+`: %d, `+"`vector.similarity_function`"+`: 'cosine'}}`, n.dim), nil); err != nil {
		return err
	}
	_, err := n.run(ctx, `CALL db.awaitIndexes(3600)`, nil)
	return err
}

const neo4jFilters = `d.scope = $scope AND d.current
  AND ($form IS NULL OR d.form = $form)
  AND ($kind IS NULL OR d.kind = $kind)
  AND ($from IS NULL OR (d.date_anchor >= $from AND d.date_anchor <= $to))`

func (n *Neo4j) w1Cypher(mode Mode) string {
	if mode == Exact {
		return `
MATCH (:Entity {id: $entity})<-[:MENTIONS]-(d:Doc)
WHERE ` + neo4jFilters + `
MATCH (d)-[:HAS_CHUNK]->(c:Chunk)
WITH c, d, vector.similarity.cosine(c.vec, $vec) AS score
RETURN c.id AS chunk, d.id AS doc, d.version AS version, d.scope AS scope, score
ORDER BY score DESC, chunk
LIMIT $k`
	}
	return `
CALL db.index.vector.queryNodes('chunk_vec', $ef, $vec) YIELD node AS c, score
MATCH (d:Doc)-[:HAS_CHUNK]->(c)
WHERE ` + neo4jFilters + `
  AND EXISTS { (d)-[:MENTIONS]->(:Entity {id: $entity}) }
RETURN c.id AS chunk, d.id AS doc, d.version AS version, d.scope AS scope, score
ORDER BY score DESC, chunk
LIMIT $k`
}

func (n *Neo4j) params(q Query) map[string]any {
	params := map[string]any{"entity": q.Entity, "scope": q.Scope, "vec": float64s(q.Vec), "k": q.K, "ef": EfSearch,
		"form": nil, "kind": nil, "from": nil, "to": nil}
	if q.Form != "" {
		params["form"] = q.Form
	}
	if q.Kind != "" {
		params["kind"] = q.Kind
	}
	if q.DateFrom != "" {
		params["from"], params["to"] = q.DateFrom, q.DateTo
	}
	return params
}

func (n *Neo4j) Query(ctx context.Context, q Query, mode Mode) ([]Hit, error) {
	result, err := n.run(ctx, n.w1Cypher(mode), n.params(q))
	if err != nil {
		return nil, err
	}
	hits := make([]Hit, 0, len(result.Records))
	for _, record := range result.Records {
		values := record.AsMap()
		// Neo4j's cosine score is (1 + cosine) / 2, so the distance 1 - cosine is 2 - 2*score.
		score, _ := values["score"].(float64)
		version, _ := values["version"].(int64)
		hits = append(hits, Hit{ChunkID: values["chunk"].(string), DocID: values["doc"].(string), Version: int(version),
			Scope: values["scope"].(string), Distance: 2 - 2*score})
	}
	return hits, nil
}

func (n *Neo4j) Describe(ctx context.Context) map[string]string {
	out := map[string]string{"vector_index": fmt.Sprintf("vector index, cosine; queryNodes candidates=%d", EfSearch)}
	result, err := n.run(ctx, `CALL dbms.components() YIELD name, versions, edition RETURN name, versions[0] AS version, edition`, nil)
	if err == nil && len(result.Records) > 0 {
		values := result.Records[0].AsMap()
		out["engine"] = fmt.Sprintf("%v %v (%v)", values["name"], values["version"], values["edition"])
	}
	return out
}

func (n *Neo4j) Close() error {
	if n.driver == nil {
		return nil
	}
	return n.driver.Close(context.Background())
}

func float64s(v []float32) []float64 {
	out := make([]float64, len(v))
	for i, x := range v {
		out[i] = float64(x)
	}
	return out
}
