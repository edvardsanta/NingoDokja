package price

func gpuModel(title string) string {
	tokens := comparisonWords(title)
	for i, token := range tokens {
		if token != "rx" && token != "rtx" && token != "gtx" {
			continue
		}
		if i+1 >= len(tokens) {
			continue
		}
		model := tokens[i+1]
		if len(model) < 3 || model[0] < '0' || model[0] > '9' {
			continue
		}
		for j := i + 2; j < len(tokens); j++ {
			switch tokens[j] {
			case "xt", "xtx", "ti", "super", "gre", "m", "mobile", "laptop":
				model += tokens[j]
			default:
				return token + ":" + model
			}
		}
		return token + ":" + model
	}
	return ""
}

func eligibleAlternative(original, candidate map[string]any) bool {
	title, _ := original["title"].(string)
	other, _ := candidate["title"].(string)
	if model := gpuModel(title); model != "" && model == gpuModel(other) {
		if candidate["model_assessment"] == nil {
			candidate["classification"] = "same_model_variant"
		}
		return true
	}
	review, _ := candidate["model_assessment"].(map[string]any)
	switch review["market_role"] {
	case "older_option", "different_tier":
		return false
	case "direct_peer", "variant":
		return true
	}
	classification, _ := candidate["classification"].(string)
	return classification == "technically_comparable" || classification == "technically_superior"
}
