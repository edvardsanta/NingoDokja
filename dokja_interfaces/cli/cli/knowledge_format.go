package cli

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// FormatIngest is the one-line report for an added file.
func FormatIngest(name string, answer map[string]any) string {
	if _, ok := answer["documents"].([]any); ok {
		line := fmt.Sprintf("added  %s: %v entries (%v new, %v unchanged), %v chunks",
			name, answer["count"], answer["created"], answer["unchanged"], answer["chunks"])
		if answer["degraded"] == true {
			reason, _ := answer["reason"].(string)
			line += fmt.Sprintf("; keyword search only (%s), run 'knowledge reindex' later", reason)
		}
		return line
	}
	sourceID, _ := answer["source_id"].(string)
	switch {
	case answer["changed"] == false:
		return fmt.Sprintf("same   %s (%s already stored, unchanged)", name, sourceID)
	case answer["degraded"] == true:
		reason, _ := answer["reason"].(string)
		return fmt.Sprintf("kept   %s -> %s: %v chunks, keyword search only (%s); run 'knowledge reindex' later",
			name, sourceID, answer["chunks"], reason)
	}
	verb := "added"
	if answer["created"] == false {
		verb = "updated"
	}
	return fmt.Sprintf("%-6s %s -> %s: %v chunks", verb, name, sourceID, answer["chunks"])
}

// FormatSearch shows the passages best first, marking which ones the service considers
// relevant, so a weak match is not mistaken for an answer.
func FormatSearch(answer map[string]any) string {
	var out strings.Builder
	hits, _ := answer["hits"].([]any)
	if degraded, _ := answer["degraded"].(bool); degraded {
		reason, _ := answer["reason"].(string)
		fmt.Fprintf(&out, "! degraded: %s\n", reason)
	}
	if len(hits) == 0 {
		out.WriteString("nothing found\n")
		return out.String()
	}
	if count, _ := answer["relevant_count"].(float64); count == 0 {
		out.WriteString("no passage looks relevant; the closest ones follow\n")
	}
	for _, raw := range hits {
		hit, _ := raw.(map[string]any)
		mark := "  "
		if relevant, _ := hit["relevant"].(bool); relevant {
			mark = "* "
		}
		score := "  -  "
		if value, ok := hit["score"].(float64); ok {
			score = fmt.Sprintf("%.2f", value)
		}
		title, _ := hit["title"].(string)
		heading, _ := hit["heading"].(string)
		place := placeInDocument(title, heading)
		fmt.Fprintf(&out, "%s%v. [%s] %s (%v)\n", mark, hit["rank"], score, place, hit["source_id"])
		text, _ := hit["text"].(string)
		fmt.Fprintf(&out, "      %s\n", snippet(text, 240))
	}
	return out.String()
}

func FormatList(answer map[string]any) string {
	documents, _ := answer["documents"].([]any)
	if len(documents) == 0 {
		return "no documents stored\n"
	}
	var out strings.Builder
	for _, raw := range documents {
		document, _ := raw.(map[string]any)
		embedded, _ := document["embedded"].(float64)
		chunks, _ := document["chunks"].(float64)
		note := ""
		if embedded < chunks {
			note = fmt.Sprintf("  (%d/%d embedded)", int(embedded), int(chunks))
		}
		fmt.Fprintf(&out, "%-40s %-8v %3v chunks  %s%s\n", document["source_id"], document["kind"], document["chunks"], document["title"], note)
	}
	fmt.Fprintf(&out, "%v documents in total\n", answer["total"])
	return out.String()
}

func snippet(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	return string([]rune(text)[:limit]) + "…"
}

// placeInDocument names where a passage sits. The service's heading path starts at the
// document's own top heading, which is usually its title, so drop that repetition.
func placeInDocument(title, heading string) string {
	heading = strings.TrimPrefix(heading, title)
	heading = strings.TrimPrefix(heading, " > ")
	if heading == "" {
		return title
	}
	return title + " › " + heading
}
