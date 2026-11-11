package price

import "strings"

// TechnicalReview keeps a model's qualitative estimate distinct from collected evidence.
// The caller supplies the candidate identity already returned by the domain.
func TechnicalReview(product string, raw map[string]any) (map[string]any, bool) {
	key, _ := raw["product_key"].(string)
	relationship, _ := raw["relationship"].(string)
	explanation, _ := raw["explanation"].(string)
	explanation = strings.TrimSpace(explanation)
	if key != product || explanation == "" || len([]rune(explanation)) > 400 {
		return nil, false
	}
	switch relationship {
	case "comparable", "superior", "inferior", "unknown":
		review := map[string]any{
			"product_key": key, "relationship": relationship,
			"explanation": explanation, "estimated": true,
		}
		if role, _ := raw["market_role"].(string); role != "" {
			switch role {
			case "direct_peer", "variant", "older_option", "different_tier", "unknown":
				review["market_role"] = role
			default:
				return nil, false
			}
		}
		if advantage, _ := raw["advantage"].(string); advantage != "" && len([]rune(advantage)) <= 60 {
			review["advantage"] = advantage
		}
		return review, true
	default:
		return nil, false
	}
}

// TechnicalReviewWithSources checks citations against the supplied collector excerpts.
// A collected URL alone does not establish that its contents were read.
func TechnicalReviewWithSources(product string, raw map[string]any, sources []any) (map[string]any, bool) {
	review, valid := TechnicalReview(product, raw)
	if !valid {
		return nil, false
	}
	requested, exists := raw["source_urls"]
	if !exists {
		return review, true
	}
	rows, ok := requested.([]any)
	if !ok || len(rows) > 5 {
		return nil, false
	}
	allowed := map[string]bool{}
	for _, raw := range sources {
		source, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		excerpt, _ := source["excerpt"].(string)
		address, _ := source["url"].(string)
		if strings.TrimSpace(excerpt) != "" {
			allowed[address] = true
		}
	}
	citations := []any{}
	seen := map[string]bool{}
	for _, raw := range rows {
		address, ok := raw.(string)
		if !ok || !allowed[address] {
			return nil, false
		}
		if !seen[address] {
			citations = append(citations, address)
			seen[address] = true
		}
	}
	review["source_urls"] = citations
	return review, true
}
