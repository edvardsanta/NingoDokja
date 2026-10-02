package cli

import (
	"fmt"
	"sort"
	"strings"
)

// FormatRecall lists the closest experiences best first, with how each turned out, and the
// overall count of outcomes the baseline comes from.
func FormatRecall(answer map[string]any) string {
	var out strings.Builder
	if degraded, _ := answer["degraded"].(bool); degraded {
		reason, _ := answer["reason"].(string)
		fmt.Fprintf(&out, "! degraded: %s (no similarity, only the counts)\n", reason)
	}
	out.WriteString(formatOutcomes(answer["outcomes"]))

	neighbors, _ := answer["neighbors"].([]any)
	if len(neighbors) == 0 {
		out.WriteString("no similar experience with an outcome yet\n")
		return out.String()
	}
	for rank, raw := range neighbors {
		neighbor, _ := raw.(map[string]any)
		similarity, _ := neighbor["similarity"].(float64)
		fmt.Fprintf(&out, "%2d. [%.2f] %-9v %-14v %v\n", rank+1, similarity, neighbor["outcome"], neighbor["detail"], neighbor["ref"])
		if text, _ := neighbor["context_snippet"].(string); text != "" {
			fmt.Fprintf(&out, "      \"%s\"\n", snippet(text, 100))
		}
	}
	return out.String()
}

func formatOutcomes(raw any) string {
	counts, _ := raw.(map[string]any)
	if len(counts) == 0 {
		return "no outcomes recorded yet\n"
	}
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%s %v", name, counts[name]))
	}
	return "outcomes: " + strings.Join(parts, ", ") + "\n"
}

// FormatPrediction says what was estimated, against which baseline, and how much evidence stood
// behind it. A prediction that is only the baseline is never presented as more than that.
func FormatPrediction(answer map[string]any) string {
	var out strings.Builder
	predicted, _ := answer["predicted_p"].(float64)
	baseline, _ := answer["baseline_p"].(float64)
	fmt.Fprintf(&out, "chance %v is accepted: %.2f (baseline %.2f)\n", answer["action"], predicted, baseline)
	fmt.Fprintf(&out, "evidence: %v similar experiences, %v resolved in all\n", answer["support"], answer["resolved"])
	if degraded, _ := answer["degraded"].(bool); degraded {
		out.WriteString("! degraded: similarity is unavailable\n")
	}
	if insufficient, _ := answer["insufficient"].(bool); insufficient {
		reason, _ := answer["reason"].(string)
		fmt.Fprintf(&out, "! not enough evidence (%s): this is just the baseline\n", reason)
	}
	evidence, _ := answer["evidence"].([]any)
	for _, raw := range evidence {
		neighbor, _ := raw.(map[string]any)
		similarity, _ := neighbor["similarity"].(float64)
		fmt.Fprintf(&out, "  [%.2f] %-9v %-14v %v\n", similarity, neighbor["outcome"], neighbor["detail"], neighbor["ref"])
		if text, _ := neighbor["context_snippet"].(string); text != "" {
			fmt.Fprintf(&out, "      \"%s\"\n", snippet(text, 100))
		}
	}
	return out.String()
}

// FormatScore reports the scorecard. Unless there is enough data it says so first: a few
// scored predictions say nothing about whether the method works.
func FormatScore(answer map[string]any) string {
	var out strings.Builder
	scored, _ := answer["scored"].(float64)
	if scored == 0 {
		out.WriteString("nothing scored yet: no resolved experience carries a stored prediction\n")
		fmt.Fprintf(&out, "unscored: %v\n", answer["unscored"])
		return out.String()
	}
	if enough, _ := answer["enough_data"].(bool); !enough {
		fmt.Fprintf(&out, "! only %v scored; judging needs at least %v, so read the numbers as noise\n", answer["scored"], answer["min_scored"])
	}
	prediction, _ := answer["brier_prediction"].(float64)
	baseline, _ := answer["brier_baseline"].(float64)
	skill, _ := answer["skill"].(float64)
	fmt.Fprintf(&out, "scored %v, unscored %v\n", answer["scored"], answer["unscored"])
	fmt.Fprintf(&out, "brier (lower is better): prediction %.3f, baseline %.3f\n", prediction, baseline)
	verdict := "does not beat the baseline"
	if beats, _ := answer["beats_baseline"].(bool); beats {
		verdict = "beats the baseline"
	}
	fmt.Fprintf(&out, "skill %+.3f: %s\n", skill, verdict)
	return out.String()
}

// FormatExperiences lists a page of experiences, newest first. Each line says how it turned out
// (pending until a verdict arrives), what the bot did, the chance it predicted against the baseline
// and the ref to use with show, resolve and forget; the snippet of its context is on the next line.
func FormatExperiences(answer map[string]any) string {
	items, _ := answer["experiences"].([]any)
	total, _ := answer["total"].(float64)
	offset, _ := answer["offset"].(float64)
	if len(items) == 0 {
		if total == 0 {
			return "no experiences\n"
		}
		return fmt.Sprintf("nothing at offset %d: %d in all\n", int(offset), int(total))
	}

	var out strings.Builder
	fmt.Fprintf(&out, "%d experiences, newest first (showing %d-%d)\n", int(total), int(offset)+1, int(offset)+len(items))
	for i, raw := range items {
		experience, _ := raw.(map[string]any)
		outcome, _ := experience["outcome"].(string)
		if outcome == "" {
			outcome = "pending"
		}
		chance := "   -   "
		if predicted, ok := experience["predicted_p"].(float64); ok {
			if baseline, ok := experience["baseline_p"].(float64); ok {
				chance = fmt.Sprintf("%.2f/%.2f", predicted, baseline)
			}
		}
		created, _ := experience["created_at"].(string)
		if len(created) > 10 {
			created = created[:10]
		}
		fmt.Fprintf(&out, "%3d. %-8s %-16v %-12v %s %s %v\n",
			int(offset)+i+1, outcome, experience["action"], experience["detail"], chance, created, experience["ref"])
		if text, _ := experience["context_snippet"].(string); text != "" {
			fmt.Fprintf(&out, "       \"%s\"\n", snippet(text, 100))
		}
		if matched := formatMatched(experience); matched != "" {
			fmt.Fprintf(&out, "       %s\n", matched)
		}
	}
	return out.String()
}

// formatMatched says what the bot's choice rested on: how close the earlier example was and the start
// of its text. It is empty for an experience recorded without them.
func formatMatched(experience map[string]any) string {
	score, hasScore := experience["matched_score"].(float64)
	text, _ := experience["matched_snippet"].(string)
	switch {
	case hasScore && text != "":
		return fmt.Sprintf("matched %.2f: \"%s\"", score, snippet(text, 100))
	case hasScore:
		return fmt.Sprintf("matched %.2f", score)
	case text != "":
		return fmt.Sprintf("matched: \"%s\"", snippet(text, 100))
	}
	return ""
}
