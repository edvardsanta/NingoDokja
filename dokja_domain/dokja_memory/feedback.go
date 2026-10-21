package memory

// addFeedbackGuidance explains existing verdict rules; it never records a verdict
// or treats absent feedback as rejection. The UI translates these stable codes.
func addFeedbackGuidance(result map[string]any) {
	if result == nil {
		return
	}
	if found, _ := result["found"].(bool); found {
		result["feedback"] = feedbackFor(result)
	}
	if rows, ok := result["experiences"].([]any); ok {
		for _, raw := range rows {
			if row, ok := raw.(map[string]any); ok {
				row["feedback"] = feedbackFor(row)
			}
		}
	}
}

func feedbackFor(row map[string]any) map[string]any {
	code, message := "explicit_outcome", "Record an observed outcome for this exact experience reference. Do not infer rejection from missing feedback."
	switch {
	case text(row, "outcome") == "expired":
		code, message = "expired", "This experience expired without an observed verdict; expiry is not rejection."
	case text(row, "outcome") != "":
		code, message = "recorded", "An outcome is already recorded. The first verdict is retained."
	case text(row, "action") == "hashtag.suggest" && text(row, "detail") != "":
		code, message = "hashtag_tag", "Give explicit hashtag feedback for the same meme: the suggested tag means accepted, a different tag means replaced. No feedback remains unknown. Repeated attempts require reliable attribution; do not guess which attempt feedback belongs to."
	}
	return map[string]any{"code": code, "message": message}
}
