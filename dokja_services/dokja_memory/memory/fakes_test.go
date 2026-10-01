package memory

import (
	"context"
	"hash/crc32"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode"
)

// fakeEmbedder is a bag-of-words hashing embedder: texts that share words get similar vectors,
// texts that share none are nearly orthogonal. Deterministic and offline, so it tests the
// retrieval logic and not the quality of a real model.
type fakeEmbedder struct {
	mu    sync.Mutex
	down  bool
	calls int
	texts int
}

const fakeDimensions = 256

func (f *fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.texts += len(texts)
	if f.down {
		return nil, ErrEmbedUnavailable
	}
	vectors := make([][]float32, len(texts))
	for i, content := range texts {
		vector := make([]float32, fakeDimensions)
		for _, word := range strings.FieldsFunc(strings.ToLower(content), func(r rune) bool { return !unicode.IsLetter(r) }) {
			vector[crc32.ChecksumIEEE([]byte(word))%fakeDimensions]++
		}
		vectors[i] = normalize(vector)
	}
	return vectors, nil
}

func (f *fakeEmbedder) Reachable(context.Context) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return !f.down
}

func (f *fakeEmbedder) setDown(down bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.down = down
}

// clock is a controllable time source for the store.
type clock struct{ now time.Time }

func (c *clock) Now() time.Time          { return c.now }
func (c *clock) Advance(d time.Duration) { c.now = c.now.Add(d) }

func newClock() *clock {
	return &clock{now: time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)}
}

// newTestStore opens an in-memory store driven by the returned clock.
func newTestStore(t *testing.T, expireAfter time.Duration) (*Store, *clock) {
	t.Helper()
	store, err := OpenStore(":memory:", expireAfter)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	c := newClock()
	store.now = c.Now
	return store, c
}

func newTestService(t *testing.T, expireAfter time.Duration) (*Service, *fakeEmbedder, *clock) {
	t.Helper()
	store, c := newTestStore(t, expireAfter)
	embedder := &fakeEmbedder{}
	return NewService(store, embedder, "test-model"), embedder, c
}

func ptr(value float64) *float64 { return &value }
