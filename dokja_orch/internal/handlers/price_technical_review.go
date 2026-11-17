package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	pricedomain "read_books/dokja_domain/dokja_price"
	promptfiles "read_books/dokja_store/prompts"
	"time"
)

func technicalReviewInstructions() string {
	return promptfiles.MustRead("technical_review_instructions")
}

func technicalIdentity(product map[string]any) map[string]any {
	specs, _ := product["specifications"].(map[string]any)
	identitySpecs := map[string]any{}
	for key, value := range specs {
		if key != "technical_sources" {
			identitySpecs[key] = value
		}
	}
	return map[string]any{
		"product": product["product"], "title": product["title"],
		"category": product["category"], "brand": product["brand"],
		"part_number": product["part_number"], "specifications": identitySpecs,
	}
}

func (h *PriceDomainHandler) reviewAlternatives(ctx context.Context, result map[string]any, deadline time.Time) {
	product, _ := result["product"].(map[string]any)
	items, _ := result["alternatives"].([]any)
	pending := map[string]map[string]any{}
	candidates := []any{}
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, _ := item["product"].(string)
		if id == "" {
			continue
		}
		delete(item, "model_assessment")
		pending[id] = item
		candidate := technicalIdentity(item)
		for _, key := range []string{"price_cents", "classification", "reason_codes", "benchmark_comparisons"} {
			candidate[key] = item[key]
			if key == "benchmark_comparisons" {
				if rows, ok := item[key].([]any); ok && len(rows) > 2 {
					candidate[key] = rows[:2]
				}
			}
		}
		candidates = append(candidates, candidate)
	}
	if len(pending) == 0 || time.Until(deadline) < 2*compareCommentFloor {
		return
	}
	sources := pricedomain.ComparisonSources(result)
	facts, err := json.Marshal(map[string]any{"comparison_context": result["comparison_context"], "technical_sources": sources, "product": technicalIdentity(product), "candidates": candidates, "offers": result["offers"], "decision": result["decision"]})
	if err != nil || len(facts) > 110000 {
		return
	}
	schema := map[string]any{
		"type": "object", "additionalProperties": false, "required": []string{"assessments"},
		"properties": map[string]any{"assessments": map[string]any{
			"type": "array", "minItems": 1, "maxItems": len(pending),
			"items": map[string]any{
				"type": "object", "additionalProperties": false,
				"required": []string{"product_key", "relationship", "explanation", "market_role"},
				"properties": map[string]any{
					"product_key":  map[string]any{"type": "string"},
					"relationship": map[string]any{"type": "string", "enum": []string{"comparable", "superior", "inferior", "unknown"}},
					"explanation":  map[string]any{"type": "string", "minLength": 1, "maxLength": 400},
					"source_urls":  map[string]any{"type": "array", "maxItems": 5, "items": map[string]any{"type": "string"}},
					"advantage":    map[string]any{"type": "string", "maxLength": 60},
					"market_role":  map[string]any{"type": "string", "enum": []string{"direct_peer", "variant", "older_option", "different_tier", "unknown"}},
				},
			},
		}},
	}
	reviews := map[string]map[string]any{}
	accept := func(reply map[string]any) error {
		text, _ := reply["reply"].(string)
		var decoded struct {
			Assessments []map[string]any `json:"assessments"`
		}
		if err := json.Unmarshal([]byte(text), &decoded); err != nil || len(decoded.Assessments) == 0 || len(decoded.Assessments) > len(pending) {
			return fmt.Errorf("invalid technical review")
		}
		validated := map[string]map[string]any{}
		for _, raw := range decoded.Assessments {
			id, _ := raw["product_key"].(string)
			review, valid := pricedomain.TechnicalReviewWithSources(id, raw, sources)
			if !valid || review["market_role"] == nil || pending[id] == nil || validated[id] != nil {
				return fmt.Errorf("invalid technical review candidate")
			}
			validated[id] = review
		}
		reviews = validated
		return nil
	}
	wait := min(45*time.Second, time.Until(deadline)-compareCommentFloor)
	reviewCtx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	_, err = h.generatePriceTaskProfile(reviewCtx, "analysis", schema, []map[string]string{
		{"role": "system", "content": technicalReviewInstructions()},
		{"role": "user", "content": "<<FACTS>>\n" + string(facts) + "\n<<END FACTS>>"},
	}, accept)
	if err != nil {
		return
	}
	for id, review := range reviews {
		pending[id]["model_assessment"] = review
	}
}
