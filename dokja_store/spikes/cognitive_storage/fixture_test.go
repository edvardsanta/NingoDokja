package spike

import (
	"reflect"
	"testing"
)

func smallFixture() Fixture {
	return Generate(Config{Seed: 7, Chunks: 4000, Dim: 16, Queries: 120})
}

func TestTheFixtureIsDeterministicAndHoldsEveryW1Case(t *testing.T) {
	a, b := smallFixture(), smallFixture()
	if !reflect.DeepEqual(a, b) {
		t.Fatal("the same config must give the same fixture")
	}
	cases := map[string]int{}
	for _, d := range a.Docs {
		cases[d.Case]++
	}
	for _, name := range []string{"twin", "superseded", "deleted"} {
		if cases[name] == 0 {
			t.Errorf("the fixture has no %s documents: %v", name, cases)
		}
	}
	queries := map[string]int{}
	for _, q := range a.Queries {
		queries[q.Name]++
	}
	for _, name := range []string{"high_degree", "rare_form", "form", "kind", "news_dates", "entity"} {
		if queries[name] == 0 {
			t.Errorf("no %s query: %v", name, queries)
		}
	}
}

func TestTheGoldHonoursScopeVersionsDeletionAndFilters(t *testing.T) {
	f := smallFixture()
	gold := ComputeGold(f)
	nonEmpty := 0
	for qi, hits := range gold.Hits {
		q := f.Queries[qi]
		if check := Compare(f, hits, gold.Eligible[qi], q, hits); !check.ExactMatch || check.Recall != 1 || check.Underfilled {
			t.Fatalf("query %d: the gold must match itself: %+v", qi, check)
		}
		for i := 1; i < len(hits); i++ {
			if hits[i].Distance < hits[i-1].Distance {
				t.Fatalf("query %d: the gold must be ordered by distance", qi)
			}
		}
		if len(hits) > 0 {
			nonEmpty++
		}
	}
	if nonEmpty < len(f.Queries)/2 {
		t.Fatalf("most queries must have eligible evidence, got %d of %d", nonEmpty, len(f.Queries))
	}
}

func TestCompareCountsLeaksShortAnswersAndMisses(t *testing.T) {
	f := smallFixture()
	gold := ComputeGold(f)
	var twin, superseded Doc
	for _, d := range f.Docs {
		if d.Case == "twin" && twin.ID == "" {
			twin = d
		}
		if d.Case == "superseded" && superseded.ID == "" {
			superseded = d
		}
	}
	q := Query{Scope: ScopeOwner, Entity: twin.Mentions[0], K: 10}
	leaks := []Hit{{ChunkID: "x", DocID: twin.ID, Version: twin.Version}, {ChunkID: "y", DocID: superseded.ID, Version: superseded.Version}}
	check := Compare(f, nil, 2, q, leaks)
	if check.Foreign != 1 || check.Ineligible != 1 || check.ExactMatch {
		t.Fatalf("a guest twin and a superseded version must both count: %+v", check)
	}
	for qi, hits := range gold.Hits {
		if len(hits) >= 2 {
			check := Compare(f, hits, gold.Eligible[qi], f.Queries[qi], hits[:1])
			if !check.Underfilled || check.Recall >= 1 || check.ExactMatch {
				t.Fatalf("a short answer is underfilled and misses gold: %+v", check)
			}
			return
		}
	}
}
