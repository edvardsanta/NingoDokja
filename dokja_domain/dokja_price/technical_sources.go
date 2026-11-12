package price

import (
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

// Sources are collector observations, never citations invented by a model.
// An excerpt is supplied by the collector; a URL alone does not mean the model read it.
func cleanTechnicalSources(raw any) ([]any, error) {
	rows, ok := raw.([]any)
	if !ok || len(rows) > 5 {
		return nil, fmt.Errorf("at most five technical sources are allowed")
	}
	clean := []any{}
	seen := map[string]bool{}
	for _, raw := range rows {
		row, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid technical source")
		}
		result := map[string]any{}
		for field, limit := range map[string]int{"title": 120, "url": 500, "excerpt": 1200, "workload": 120, "observed_at": 35, "published_at": 35, "updated_at": 35, "test_conditions": 600} {
			value, exists := row[field]
			if !exists {
				continue
			}
			text, ok := value.(string)
			if !ok || !utf8.ValidString(text) || utf8.RuneCountInString(text) > limit {
				return nil, fmt.Errorf("invalid technical source field")
			}
			result[field] = strings.TrimSpace(text)
		}
		title, _ := result["title"].(string)
		address, _ := result["url"].(string)
		stamp, _ := result["observed_at"].(string)
		u, err := url.Parse(address)
		if title == "" || err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
			return nil, fmt.Errorf("invalid technical source address")
		}
		if _, err := time.Parse(time.RFC3339, stamp); err != nil {
			return nil, fmt.Errorf("technical source requires observed_at")
		}
		for _, key := range []string{"published_at", "updated_at"} {
			if value, _ := result[key].(string); value != "" {
				if _, err := time.Parse(time.RFC3339, value); err != nil {
					if _, err := time.Parse("2006-01-02", value); err != nil {
						return nil, fmt.Errorf("invalid source date")
					}
				}
			}
		}
		if rawBenchmarks, ok := row["benchmarks"]; ok {
			metrics, err := cleanBenchmarks(rawBenchmarks)
			if err != nil {
				return nil, err
			}
			result["benchmarks"] = metrics
		}
		if !seen[address] {
			clean = append(clean, result)
			seen[address] = true
		}
	}
	return clean, nil
}
func CollectedTechnicalSources(product map[string]any) []any {
	specs, _ := product["specifications"].(map[string]any)
	rows, err := cleanTechnicalSources(specs["technical_sources"])
	if err != nil {
		return []any{}
	}
	return rows
}

// ComparisonSources retains product attribution for reference and shortlisted candidates.
func ComparisonSources(result map[string]any) []any {
	products := []map[string]any{}
	if p, ok := result["product"].(map[string]any); ok {
		products = append(products, p)
	}
	items, _ := result["alternatives"].([]any)
	for _, raw := range items {
		if p, ok := raw.(map[string]any); ok {
			products = append(products, p)
		}
	}
	sources := []any{}
	for _, p := range products {
		for _, raw := range CollectedTechnicalSources(p) {
			row := raw.(map[string]any)
			copy := map[string]any{"product": p["product"]}
			for k, v := range row {
				copy[k] = v
			}
			sources = append(sources, copy)
		}
	}
	return sources
}
