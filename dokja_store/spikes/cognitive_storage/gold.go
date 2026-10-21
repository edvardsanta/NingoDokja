package spike

import (
	"context"
	"math"
	"sort"
)

// Hit is one chunk a W1 query returned, with its cosine distance (1 - cosine similarity).
type Hit struct {
	ChunkID  string
	DocID    string
	Version  int
	Scope    string
	Distance float64
}

// Mode chooses how an engine searches: Exact must scan every eligible chunk; Approx uses
// the engine's vector index the way an application would.
type Mode string

const (
	Exact  Mode = "exact"
	Approx Mode = "approx"
)

// Store is one engine under test. An adapter only translates the query; eligibility is the
// same rule for every engine, written once in Eligible.
type Store interface {
	Name() string
	Reset(ctx context.Context, dim int) error
	Load(ctx context.Context, f Fixture) error
	Delete(ctx context.Context, docID string) error
	BuildIndex(ctx context.Context) error
	Modes() []Mode
	Query(ctx context.Context, q Query, mode Mode) ([]Hit, error)
	// Describe reports what the running engine says about itself, for the results.
	Describe(ctx context.Context) map[string]string
	Close() error
}

// Eligible is W1's rule: the current version of a document that was not deleted, in the
// query's scope, mentioning its entity, and matching its filters.
func Eligible(d Doc, deleted map[string]bool, q Query) bool {
	if !d.Current || deleted[d.ID] || d.Scope != q.Scope || !mentionsEntity(d, q.Entity) {
		return false
	}
	if q.Form != "" && d.Form != q.Form || q.Kind != "" && d.Kind != q.Kind {
		return false
	}
	if q.DateFrom != "" && (d.DateAnchor == "" || d.DateAnchor < q.DateFrom || d.DateAnchor > q.DateTo) {
		return false
	}
	return true
}

func mentionsEntity(d Doc, entity string) bool {
	for _, e := range d.Mentions {
		if e == entity {
			return true
		}
	}
	return false
}

// Gold computes each query's answer by scanning the fixture itself, so no engine under
// test is the reference. It also counts the eligible chunks, to tell a short answer that
// is correct from one an index cut short.
type Gold struct {
	Hits     [][]Hit
	Eligible []int
}

func ComputeGold(f Fixture) Gold {
	deleted := map[string]bool{}
	for _, id := range f.Deleted {
		deleted[id] = true
	}
	docs := map[string]Doc{}
	for _, d := range f.Docs {
		docs[d.Key()] = d
	}
	gold := Gold{Hits: make([][]Hit, len(f.Queries)), Eligible: make([]int, len(f.Queries))}
	for qi, q := range f.Queries {
		var hits []Hit
		for _, c := range f.Chunks {
			d := docs[docKey(c.DocID, c.Version)]
			if !Eligible(d, deleted, q) {
				continue
			}
			hits = append(hits, Hit{ChunkID: c.ID, DocID: c.DocID, Version: c.Version, Scope: d.Scope, Distance: CosineDistance(q.Vec, c.Vec)})
		}
		gold.Eligible[qi] = len(hits)
		SortHits(hits)
		if len(hits) > q.K {
			hits = hits[:q.K]
		}
		gold.Hits[qi] = hits
	}
	return gold
}

func docKey(id string, version int) string { return Doc{ID: id, Version: version}.Key() }

// CosineDistance of two unit vectors, accumulated in float64.
func CosineDistance(a, b []float32) float64 {
	dot := 0.0
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
	}
	return 1 - dot
}

// SortHits orders by distance, then chunk ID, the order every engine is asked for.
func SortHits(hits []Hit) {
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Distance != hits[j].Distance {
			return hits[i].Distance < hits[j].Distance
		}
		return hits[i].ChunkID < hits[j].ChunkID
	})
}

// Check is how one answer compares with the gold.
type Check struct {
	Recall      float64 // share of the gold IDs returned (1 when the gold is empty)
	Underfilled bool    // fewer than min(K, eligible) results
	Foreign     int     // results from another scope
	Ineligible  int     // results that break W1's rule otherwise (superseded, deleted, filters)
	ExactMatch  bool    // same IDs in the same order, distances within tolerance
}

// distanceTolerance absorbs float32 storage and engine arithmetic; two gold neighbours
// closer than this may swap places without counting as an error.
const distanceTolerance = 1e-4

func Compare(f Fixture, gold []Hit, eligible int, q Query, got []Hit) Check {
	deleted := map[string]bool{}
	for _, id := range f.Deleted {
		deleted[id] = true
	}
	docs := map[string]Doc{}
	for _, d := range f.Docs {
		docs[d.Key()] = d
	}
	var c Check
	want := map[string]bool{}
	for _, h := range gold {
		want[h.ChunkID] = true
	}
	found := 0
	for _, h := range got {
		d, ok := docs[docKey(h.DocID, h.Version)]
		switch {
		case ok && d.Scope != q.Scope:
			c.Foreign++
		case !ok || !Eligible(d, deleted, q):
			c.Ineligible++
		}
		if want[h.ChunkID] {
			found++
		}
	}
	c.Recall = 1
	if len(gold) > 0 {
		c.Recall = float64(found) / float64(len(gold))
	}
	c.Underfilled = len(got) < min(q.K, eligible)
	c.ExactMatch = len(got) == len(gold) && c.Foreign == 0 && c.Ineligible == 0
	for i := 0; c.ExactMatch && i < len(got); i++ {
		if math.Abs(got[i].Distance-gold[i].Distance) > distanceTolerance {
			c.ExactMatch = false
		}
	}
	return c
}
