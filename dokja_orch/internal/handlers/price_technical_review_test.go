package handlers

import (
	"context"
	"path/filepath"
	competitorcache "read_books/internal/infrastructure/competitor_cache"
	"testing"
	"time"
)

func TestTechnicalReviewSurvivesReopenAndPriceChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.db")
	cache, err := competitorcache.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	model := &bindingCommenter{fakeCommenter: fakeCommenter{
		reply: `{"assessments":[{"product_key":"other","relationship":"comparable","explanation":"Desempenho estimado semelhante, com outra plataforma."}]}`,
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
	handler.reviewAlternatives(context.Background(), result, time.Now().Add(time.Minute))
	if candidate["model_assessment"] == nil || len(model.bindings) != 1 {
		t.Fatalf("cache missed after price change: %v %v", candidate, model.bindings)
	}
	candidate["specifications"] = map[string]any{"cores": 8.0}
	handler.reviewAlternatives(context.Background(), result, time.Now().Add(time.Minute))
	if len(model.bindings) != 2 {
		t.Fatalf("specification change reused stale review: %v", model.bindings)
	}
}

func TestTechnicalReviewRejectsInventedCandidatesAndFallsBack(t *testing.T) {
	model := &bindingCommenter{replies: map[string]string{
		"claude_code": `{"assessments":[{"product_key":"invented","relationship":"superior","explanation":"Invented."}]}`,
		"codex":       `{"assessments":[{"product_key":"other","relationship":"unknown","explanation":"Dados insuficientes."}]}`,
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
