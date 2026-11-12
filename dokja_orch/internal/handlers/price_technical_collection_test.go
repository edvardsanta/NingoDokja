package handlers

import (
	"context"
	"errors"
	"path/filepath"
	cache "read_books/internal/infrastructure/competitor_cache"
	"strings"
	"testing"
	"time"
)

type technicalFeedFixture struct {
	calls int
	fail  bool
}

func (f *technicalFeedFixture) Dispatch(_ context.Context, kind string, payload map[string]any) (map[string]any, error) {
	f.calls++
	if f.fail {
		return nil, errors.New("private transport detail")
	}
	if kind != "feeds.invoke" || payload["capability"] != "price.technical.v1" || payload["action"] != "collect" {
		return nil, errors.New("wrong collection request")
	}
	rows := []any{}
	products := payload["input"].(map[string]any)["products"].([]map[string]any)
	for i, p := range products {
		rows = append(rows, map[string]any{"product_key": p["product"], "opinion": "not durable", "technical_sources": []any{map[string]any{"title": "Example lab", "url": "https://bench.example/report", "observed_at": time.Now().UTC().Format(time.RFC3339), "excerpt": "Exact table row", "test_conditions": "Same native test system", "benchmarks": []any{map[string]any{"test": "1080p Ultra", "value": 100.0 + float64(i)*10, "unit": "fps", "sample": p["title"], "snapshot": strings.Repeat("a", 64), "higher_is_better": true}}}}})
	}
	return map[string]any{"evidence": rows}, nil
}
func collectedComparison(budget int) map[string]any {
	return map[string]any{"product": map[string]any{"product": "ref", "title": "RX 7600", "category": "placa de video"}, "alternatives": []any{map[string]any{"product": "other", "title": "RTX 4060", "category": "placa de video", "price_cents": 100000.0}}, "decision": map[string]any{"budget_cents": budget}}
}
func TestTechnicalCollectionCachesFactsAcrossBudgetsAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.db")
	store, err := cache.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	feeds := &technicalFeedFixture{}
	h := &PriceDomainHandler{feeds: feeds, equivalenceCache: store}
	first := collectedComparison(200000)
	h.collectTechnicalEvidence(t.Context(), first, time.Now().Add(time.Minute))
	if first["technical_collection"].(map[string]any)["status"] != "collected" || feeds.calls != 1 {
		t.Fatal(first, feeds.calls)
	}
	store.Close()
	store, err = cache.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	h.equivalenceCache = store
	feeds.fail = true
	second := collectedComparison(150000)
	h.collectTechnicalEvidence(t.Context(), second, time.Now().Add(time.Minute))
	if second["technical_collection"].(map[string]any)["status"] != "cached" || feeds.calls != 1 {
		t.Fatal(second, feeds.calls)
	}
	if len(second["alternatives"].([]any)[0].(map[string]any)["benchmark_comparisons"].([]any)) != 1 {
		t.Fatal("cached facts lost comparisons")
	}
}
func TestTechnicalCollectionFailureAndShortDeadlinePreservePriceResult(t *testing.T) {
	feeds := &technicalFeedFixture{fail: true}
	h := &PriceDomainHandler{feeds: feeds}
	result := collectedComparison(200000)
	h.collectTechnicalEvidence(t.Context(), result, time.Now().Add(time.Minute))
	if result["technical_collection"].(map[string]any)["status"] != "unavailable" {
		t.Fatal(result)
	}
	h.collectTechnicalEvidence(t.Context(), result, time.Now().Add(time.Second))
	if feeds.calls != 1 || result["technical_collection"].(map[string]any)["status"] != "time_budget" {
		t.Fatal(result, feeds.calls)
	}
}
