package handlers

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	pricedomain "read_books/dokja_domain/dokja_price"
)

// Collection is bounded separately so optional evidence cannot consume review/comment time.
func (h *PriceDomainHandler) collectTechnicalEvidence(ctx context.Context, result map[string]any, deadline time.Time) {
	products := []map[string]any{}
	add := func(product map[string]any) {
		title, _ := product["title"].(string)
		category, _ := product["category"].(string)
		profile := pricedomain.CategoryProfileFor(category)
		if title != "" && (profile.ID == "gpu" || profile.ID == "cpu") {
			products = append(products, map[string]any{"product": product["product"], "title": title})
		}
	}
	if p, ok := result["product"].(map[string]any); ok {
		add(p)
	}
	if len(products) == 0 {
		return
	}
	items, _ := result["alternatives"].([]any)
	for _, raw := range items {
		if p, ok := raw.(map[string]any); ok {
			add(p)
		}
	}
	if len(products) > 13 {
		products = products[:13]
	}
	sort.Slice(products, func(i, j int) bool { return products[i]["product"].(string) < products[j]["product"].(string) })
	key, _ := cacheKey(map[string]any{"version": "technical-facts-v1", "products": products})
	apply := func(rows []map[string]any) bool {
		raw := make([]any, 0, len(rows))
		for _, row := range rows {
			raw = append(raw, row)
		}
		validated, err := pricedomain.ApplyTechnicalEvidence(result, raw)
		return err == nil && len(validated) > 0
	}
	if h.equivalenceCache != nil {
		if rows, hit, err := h.equivalenceCache.GetEquivalences(ctx, key); err == nil && hit && pricedomain.TechnicalEvidenceFresh(rows, time.Now()) && apply(rows) {
			result["technical_collection"] = map[string]any{"status": "cached"}
			pricedomain.AttachBenchmarkComparisons(result)
			return
		}
	}
	if h.intake != nil && h.intake.enabled != nil && !h.intake.enabled() {
		result["technical_collection"] = map[string]any{"status": "disabled"}
		return
	}
	if h.feeds == nil {
		return
	}
	wait := min(20*time.Second, time.Until(deadline)-2*compareCommentFloor)
	if wait <= 0 {
		result["technical_collection"] = map[string]any{"status": "time_budget"}
		return
	}
	readCtx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	reply, err := h.feeds.Dispatch(readCtx, "feeds.invoke", map[string]any{"capability": "price.technical.v1", "action": "collect", "input": map[string]any{"products": products}})
	if err != nil {
		result["technical_collection"] = map[string]any{"status": "unavailable"}
		return
	}
	// Round-trip the plugin's JSON-compatible shape; concrete mocks use the same contract.
	data, _ := json.Marshal(reply["evidence"])
	var raw []any
	if json.Unmarshal(data, &raw) != nil {
		result["technical_collection"] = map[string]any{"status": "invalid"}
		return
	}
	rows, err := pricedomain.ApplyTechnicalEvidence(result, raw)
	if err != nil {
		result["technical_collection"] = map[string]any{"status": "invalid"}
		return
	}
	status := "no_match"
	if len(rows) > 0 {
		status = "collected"
		if h.equivalenceCache != nil {
			_ = h.equivalenceCache.PutEquivalences(ctx, key, rows)
		}
	}
	result["technical_collection"] = map[string]any{"status": status, "failed_sources": reply["failed_sources"]}
	pricedomain.AttachBenchmarkComparisons(result)
}
