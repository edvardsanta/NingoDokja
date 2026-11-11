package handlers

import (
	"context"
	"path/filepath"
	competitorcache "read_books/internal/infrastructure/competitor_cache"
	"strings"
	"testing"
	"time"
)

func TestTechnicalReviewReevaluatesAfterPriceChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.db")
	cache, err := competitorcache.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	model := &bindingCommenter{fakeCommenter: fakeCommenter{
		reply: `{"assessments":[{"product_key":"other","relationship":"comparable","market_role":"direct_peer","explanation":"Desempenho estimado semelhante, com outra plataforma."}]}`,
	}}
	handler, _ := compareHandler(nil, model)
	handler.WithCompetitorEquivalenceCache(cache)
	candidate := map[string]any{"product": "other", "title": "CPU Other", "price_cents": 150000}
	result := map[string]any{
		"product":      map[string]any{"product": "original", "title": "CPU Original"},
		"alternatives": []any{candidate},
	}
	handler.reviewAlternatives(context.Background(), result, time.Now().Add(time.Minute))
	if candidate["model_assessment"] == nil || len(model.bindings) != 1 || model.bindings[0] != "claude_code:haiku" {
		t.Fatalf("assessment %v bindings %v", candidate, model.bindings)
	}
	cache.Close()
	cache, err = competitorcache.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	handler.WithCompetitorEquivalenceCache(cache)
	delete(candidate, "model_assessment")
	candidate["price_cents"] = 160000
	result["decision"] = map[string]any{"budget_cents": 170000}
	handler.reviewAlternatives(context.Background(), result, time.Now().Add(time.Minute))
	if candidate["model_assessment"] == nil || len(model.bindings) != 2 {
		t.Fatalf("review was reused after price change: %v %v", candidate, model.bindings)
	}
	latest := model.messages[len(model.messages)-1][1]["content"]
	if !strings.Contains(latest, "160000") || !strings.Contains(latest, "170000") {
		t.Fatalf("missing current context: %s", latest)
	}
	candidate["specifications"] = map[string]any{"cores": 8.0}
	handler.reviewAlternatives(context.Background(), result, time.Now().Add(time.Minute))
	if len(model.bindings) != 3 {
		t.Fatalf("specification change reused stale review: %v", model.bindings)
	}
}

func TestTechnicalReviewRejectsInventedCandidatesAndFallsBack(t *testing.T) {
	model := &bindingCommenter{replies: map[string]string{
		"claude_code": `{"assessments":[{"product_key":"invented","relationship":"superior","market_role":"direct_peer","explanation":"Invented."}]}`,
		"codex":       `{"assessments":[{"product_key":"other","relationship":"unknown","market_role":"direct_peer","explanation":"Dados insuficientes."}]}`,
	}}
	handler, _ := compareHandler(nil, model)
	candidate := map[string]any{"product": "other", "title": "CPU Other"}
	handler.reviewAlternatives(context.Background(), map[string]any{
		"product": map[string]any{"title": "CPU Original"}, "alternatives": []any{candidate},
	}, time.Now().Add(time.Minute))
	if len(model.bindings) != 2 || model.bindings[1] != "codex:gpt-6-luna" || candidate["model_assessment"] == nil {
		t.Fatalf("fallback %v assessment %v", model.bindings, candidate)
	}
}

func TestCachedCompetitorsContainDiscoveryFacts(t *testing.T) {
	cache, err := competitorcache.Open(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	handler, _ := compareHandler(nil, nil)
	handler.WithCompetitorEquivalenceCache(cache)
	product := map[string]any{"product": "original", "title": "CPU Original"}
	result := map[string]any{"product": product, "alternatives": []any{map[string]any{
		"product": "other", "title": "CPU Other", "part_number": "OTHER-CPU", "classification": "same_category_candidate",
		"confidence": "low", "price_cents": 150000, "model_assessment": map[string]any{"explanation": "Old opinion"},
	}}}
	handler.persistCompetitors(context.Background(), result)
	queries := handler.competitorQueries(context.Background(), product, nil)
	if len(queries) != 1 || queries[0] != "OTHER-CPU" {
		t.Fatalf("competitors %v", queries)
	}
	key, _ := cacheKey(map[string]any{"plan_version": competitorPlanVersion, "product": product, "profile": nil})
	items, _, _ := cache.GetEquivalences(context.Background(), key)
	for _, item := range items {
		for _, field := range []string{"model_assessment", "price_cents", "classification", "confidence", "relationship", "explanation"} {
			if item[field] != nil {
				t.Fatalf("cached opinion or price: %v", item)
			}
		}
	}
}

func TestTechnicalReviewUsesEquipmentContextAndCollectedSourceExcerpts(t *testing.T) {
	for _, tc := range []struct {
		name, url, excerpt string
		accepted           bool
	}{
		{"collected excerpt", "https://bench.example/review", "Measured under the supplied workload", true},
		{"invented citation", "https://invented.example/review", "Measured under the supplied workload", false},
		{"unread URL", "https://bench.example/review", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := &bindingCommenter{fakeCommenter: fakeCommenter{reply: `{"assessments":[{"product_key":"other","relationship":"comparable","market_role":"direct_peer","explanation":"Qualitative estimate for the supplied workload.","source_urls":["` + tc.url + `"]}]}`}}
			handler, _ := compareHandler(nil, model)
			candidate := map[string]any{"product": "other", "title": "GPU Example"}
			result := map[string]any{"product": map[string]any{"product": "reference", "specifications": map[string]any{"technical_sources": []any{map[string]any{"title": "Example test lab", "url": "https://bench.example/review", "observed_at": "2026-11-11T10:00:00Z", "excerpt": tc.excerpt}}}}, "alternatives": []any{candidate}, "comparison_context": map[string]any{"intended_use": "1080p gaming", "current_equipment": "Example CPU / 500W"}}
			handler.reviewAlternatives(t.Context(), result, time.Now().Add(time.Minute))
			_, ok := candidate["model_assessment"]
			if ok != tc.accepted {
				t.Fatal(candidate)
			}
			facts := model.messages[0][1]["content"]
			if !strings.Contains(facts, "1080p gaming") || !strings.Contains(facts, "Example CPU / 500W") {
				t.Fatal(facts)
			}
		})
	}
}
