package spike

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Each engine must return the gold exactly in Exact mode and never leak another scope, a
// superseded version or a deleted document in any mode. PostgreSQL and Neo4j are tested
// only when their address is given, so the module's tests pass without them:
//
//	SPIKE_POSTGRES_DSN=postgres://postgres@127.0.0.1:55432/postgres
//	SPIKE_NEO4J_URI=bolt://127.0.0.1:57687
func engines(t *testing.T) []Store {
	stores := []Store{NewSQLite(filepath.Join(t.TempDir(), "w1.db"))}
	if dsn := os.Getenv("SPIKE_POSTGRES_DSN"); dsn != "" {
		stores = append(stores, NewPostgres(dsn))
	} else {
		t.Log("SPIKE_POSTGRES_DSN is not set: PostgreSQL is not tested")
	}
	if uri := os.Getenv("SPIKE_NEO4J_URI"); uri != "" {
		stores = append(stores, NewNeo4j(uri))
	} else {
		t.Log("SPIKE_NEO4J_URI is not set: Neo4j is not tested")
	}
	return stores
}

func TestEveryEngineAnswersW1ExactlyAndNeverLeaks(t *testing.T) {
	f := smallFixture()
	gold := ComputeGold(f)
	for _, s := range engines(t) {
		t.Run(s.Name(), func(t *testing.T) {
			defer s.Close()
			result, err := Run(context.Background(), s, f, gold)
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range result.Modes {
				if m.Foreign != 0 || m.Ineligible != 0 {
					t.Errorf("%s: %d foreign-scope and %d ineligible rows", m.Mode, m.Foreign, m.Ineligible)
				}
				if m.Mode == Exact && (m.ExactMatch != m.Queries || m.Underfilled != 0) {
					t.Errorf("exact: %d of %d queries match the gold, %d underfilled", m.ExactMatch, m.Queries, m.Underfilled)
				}
			}
		})
	}
}
