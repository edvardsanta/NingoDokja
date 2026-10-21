// Package spike compares storage engines on the cognitive graph's first consumer, W1
// (ADR 0009): find evidence about an entity in the caller's scope, filtered by form,
// content kind and date, ranked by vector similarity. The data is generated; no owner
// database or private text is used.
package spike

import (
	"fmt"
	"math"
	"math/rand/v2"
	"sort"
)

// Scopes the fixture splits documents between. Some guest documents are near-identical
// twins of owner documents, so a leak across scopes ranks high and shows.
const (
	ScopeOwner = "owner"
	ScopeGuest = "guest"
)

// Forms and Kinds follow Knowledge's vocabulary (ADR 0008). Law is kept rare so a filter
// on it is selective and approximate search may come back short.
var (
	Forms     = []string{"book", "article", "note", "conversation", "meme", "paper", "list", "song", "report", "letter", "manual", "liturgy", "law"}
	formShare = []float64{12, 25, 15, 8, 3, 8, 5, 3, 8, 5, 5, 2, 1}
	Kinds     = []string{"news", "reference", "research", "narrative"}
)

// Config sizes a fixture. The same config and seed give the same fixture.
type Config struct {
	Seed         uint64
	Chunks       int // chunks of current documents, before twins and old versions
	ChunksPerDoc int
	Dim          int
	Entities     int
	Queries      int
}

func (c Config) withDefaults() Config {
	if c.ChunksPerDoc <= 0 {
		c.ChunksPerDoc = 10
	}
	if c.Dim <= 0 {
		c.Dim = 64
	}
	if c.Entities <= 0 {
		c.Entities = 500
	}
	if c.Queries <= 0 {
		c.Queries = 200
	}
	return c
}

// Doc is one version of a document. Only the current version of a document that was not
// deleted may be returned.
type Doc struct {
	ID         string
	Version    int
	Current    bool
	Scope      string
	Form       string
	Kind       string
	DateAnchor string // YYYY-MM-DD for news, otherwise empty
	Mentions   []string
	Case       string // why the fixture made it: "", "twin", "superseded", "deleted"
}

// Key names one version of a document.
func (d Doc) Key() string { return fmt.Sprintf("%s@%d", d.ID, d.Version) }

// Chunk is a piece of one document version with its unit-length vector.
type Chunk struct {
	ID      string
	DocID   string
	Version int
	Vec     []float32
}

// Query is W1: evidence about Entity in Scope, optionally filtered, nearest to Vec.
type Query struct {
	Name     string
	Scope    string
	Entity   string
	Form     string
	Kind     string
	DateFrom string
	DateTo   string
	Vec      []float32
	K        int
}

// Fixture is the generated corpus, the documents to delete after loading, and the queries.
type Fixture struct {
	Config  Config
	Docs    []Doc
	Chunks  []Chunk
	Deleted []string // document IDs removed after loading, through each engine's delete
	Queries []Query
}

// HighDegree is the entity a quarter of all documents mention.
const HighDegree = "e0000"

// Generate builds a fixture deterministically from its config.
func Generate(config Config) Fixture {
	c := config.withDefaults()
	r := rand.New(rand.NewPCG(c.Seed, c.Seed^0x9e3779b97f4a7c15))
	f := Fixture{Config: c}
	docs := c.Chunks / c.ChunksPerDoc
	chunkN := 0
	addChunks := func(d Doc, base [][]float32, noise float64) [][]float32 {
		vecs := make([][]float32, c.ChunksPerDoc)
		for i := range vecs {
			if base != nil {
				vecs[i] = jitter(r, base[i], noise)
			} else {
				vecs[i] = randomUnit(r, c.Dim)
			}
			f.Chunks = append(f.Chunks, Chunk{ID: fmt.Sprintf("c%07d", chunkN), DocID: d.ID, Version: d.Version, Vec: vecs[i]})
			chunkN++
		}
		return vecs
	}
	for i := 0; i < docs; i++ {
		d := Doc{ID: fmt.Sprintf("d%06d", i), Version: 1, Current: true, Scope: ScopeOwner,
			Form: pick(r, Forms, formShare), Kind: Kinds[r.IntN(len(Kinds))]}
		if r.Float64() < 0.2 {
			d.Scope = ScopeGuest
		}
		if d.Kind == "news" {
			d.DateAnchor = fmt.Sprintf("%04d-%02d-%02d", 2024+r.IntN(3), 1+r.IntN(12), 1+r.IntN(28))
		}
		d.Mentions = mentions(r, c.Entities)
		roll := r.Float64()
		switch {
		case roll < 0.05 && d.Scope == ScopeOwner:
			// A superseded version with near-identical vectors: it must never be returned.
			old := d
			old.Version, old.Current, old.Case = 1, false, "superseded"
			d.Version = 2
			f.Docs = append(f.Docs, old)
			base := addChunks(old, nil, 0)
			f.Docs = append(f.Docs, d)
			addChunks(d, base, 1e-3)
			continue
		case roll < 0.10 && d.Scope == ScopeOwner:
			// A guest twin of an owner document: same entities, near-identical vectors.
			f.Docs = append(f.Docs, d)
			base := addChunks(d, nil, 0)
			twin := d
			twin.ID, twin.Scope, twin.Case = fmt.Sprintf("t%06d", i), ScopeGuest, "twin"
			f.Docs = append(f.Docs, twin)
			addChunks(twin, base, 1e-3)
			continue
		case roll < 0.12:
			d.Case = "deleted"
			f.Deleted = append(f.Deleted, d.ID)
		}
		f.Docs = append(f.Docs, d)
		addChunks(d, nil, 0)
	}
	f.Queries = queries(r, c, f)
	return f
}

func queries(r *rand.Rand, c Config, f Fixture) []Query {
	docByKey := map[string]Doc{}
	for _, d := range f.Docs {
		docByKey[d.Key()] = d
	}
	var out []Query
	for len(out) < c.Queries {
		// Aim each query near an existing chunk, so its neighbourhood holds twins,
		// superseded and deleted versions as often as the corpus does.
		chunk := f.Chunks[r.IntN(len(f.Chunks))]
		d := docByKey[fmt.Sprintf("%s@%d", chunk.DocID, chunk.Version)]
		q := Query{Scope: d.Scope, Entity: d.Mentions[r.IntN(len(d.Mentions))], Vec: jitter(r, chunk.Vec, 0.05), K: 10}
		if d.Case == "twin" || r.Float64() < 0.1 {
			q.Scope = ScopeOwner
		}
		roll := r.Float64()
		switch {
		case roll < 0.25:
			q.Entity, q.Name = HighDegree, "high_degree"
		case roll < 0.40:
			q.Form, q.Name = "law", "rare_form"
		case roll < 0.55:
			q.Form, q.Name = d.Form, "form"
		case roll < 0.70:
			q.Kind, q.Name = d.Kind, "kind"
		case roll < 0.85:
			q.Kind, q.DateFrom, q.DateTo, q.Name = "news", "2025-01-01", "2025-12-31", "news_dates"
		default:
			q.Name = "entity"
		}
		out = append(out, q)
	}
	return out
}

func mentions(r *rand.Rand, entities int) []string {
	set := map[string]bool{}
	if r.Float64() < 0.25 {
		set[HighDegree] = true
	}
	for n := 1 + r.IntN(4); len(set) < n; {
		set[fmt.Sprintf("e%04d", 1+r.IntN(entities-1))] = true
	}
	out := make([]string, 0, len(set))
	for e := range set {
		out = append(out, e)
	}
	sort.Strings(out)
	return out
}

func pick(r *rand.Rand, values []string, weights []float64) string {
	total := 0.0
	for _, w := range weights {
		total += w
	}
	x := r.Float64() * total
	for i, w := range weights {
		if x < w {
			return values[i]
		}
		x -= w
	}
	return values[len(values)-1]
}

func randomUnit(r *rand.Rand, dim int) []float32 {
	v := make([]float32, dim)
	for i := range v {
		v[i] = float32(r.NormFloat64())
	}
	return normalize(v)
}

func jitter(r *rand.Rand, base []float32, noise float64) []float32 {
	v := make([]float32, len(base))
	for i := range v {
		v[i] = base[i] + float32(noise*r.NormFloat64())
	}
	return normalize(v)
}

func normalize(v []float32) []float32 {
	sum := 0.0
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	n := float32(math.Sqrt(sum))
	for i := range v {
		v[i] /= n
	}
	return v
}
