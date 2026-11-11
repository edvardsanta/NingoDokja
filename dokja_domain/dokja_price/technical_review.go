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
