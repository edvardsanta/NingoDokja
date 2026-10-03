package feeds

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"
	"unicode"
)

// ellipsis marks a text that was cut (U+2026).
const ellipsis rune = 0x2026

const (
	maxTitleRunes   = 300
	maxSummaryRunes = 1000
	maxSourceRunes  = 80
	maxIDRunes      = 120
	maxURLBytes     = 2048

	// A date a little ahead of the clock is skew; one far ahead is not believed.
	futureTolerance = time.Hour
)

// Item is one entry of a plugin's output once it has been checked.
type Item struct {
	ID      string
	Title   string
	Summary string
	URL     string
	// Published is the zero time when the plugin gave none, or none that can be believed.
	Published time.Time
	Source    string
}

type rawItem struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Summary   string `json:"summary"`
	URL       string `json:"url"`
	Published string `json:"published"`
	Source    string `json:"source"`
}

var errNoItems = errors.New(`output has no "items" list`)

// parseOutput reads what a plugin printed: {"items": [...]}. Plugin output is untrusted text
// from the outside, so every item is checked and bounded here and nothing downstream has to
// repeat that. An item that cannot be used is skipped and counted, never fatal to the rest.
func parseOutput(data []byte, plugin Plugin, now time.Time) (items []Item, skipped int, err error) {
	var output struct {
		Items *[]json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(data, &output); err != nil {
		return nil, 0, errors.New("output is not valid JSON")
	}
	if output.Items == nil {
		return nil, 0, errNoItems
	}

	seen := map[string]bool{}
	for _, entry := range *output.Items {
		var raw rawItem
		if err := json.Unmarshal(entry, &raw); err != nil {
			skipped++
			continue
		}
		item, ok := normalizeItem(raw, plugin, now)
		if !ok || seen[item.ID] || len(items) >= plugin.MaxItems {
			skipped++
			continue
		}
		seen[item.ID] = true
		items = append(items, item)
	}
	return items, skipped, nil
}

func normalizeItem(raw rawItem, plugin Plugin, now time.Time) (Item, bool) {
	title := cleanText(raw.Title, maxTitleRunes)
	if title == "" {
		return Item{}, false
	}
	link := cleanURL(raw.URL)
	id := cleanText(raw.ID, maxIDRunes)
	if id == "" {
		sum := sha256.Sum256([]byte(link + "\n" + title))
		id = hex.EncodeToString(sum[:8])
	}
	source := cleanText(raw.Source, maxSourceRunes)
	if source == "" {
		source = plugin.Name
	}
	return Item{
		ID:        id,
		Title:     title,
		Summary:   cleanText(raw.Summary, maxSummaryRunes),
		URL:       link,
		Published: parsePublished(raw.Published, now),
		Source:    source,
	}, true
}

// cleanText makes text safe to show: valid UTF-8, one line, no control characters and none of
// the characters that reorder what is displayed, at most limit characters.
func cleanText(text string, limit int) string {
	text = strings.ToValidUTF8(text, "")
	runes := make([]rune, 0, min(len(text), limit+1))
	pendingSpace := false
	for _, r := range text {
		switch {
		case unicode.IsSpace(r):
			pendingSpace = len(runes) > 0
			continue
		case unicode.IsControl(r), isBidiControl(r), r == 0xFEFF:
			continue
		}
		if pendingSpace {
			runes = append(runes, ' ')
			pendingSpace = false
		}
		runes = append(runes, r)
		if len(runes) > limit {
			break
		}
	}
	if len(runes) > limit {
		runes = append(runes[:limit-1], ellipsis)
	}
	return string(runes)
}

func isBidiControl(r rune) bool {
	return r == 0x061C || r == 0x200E || r == 0x200F ||
		(r >= 0x202A && r <= 0x202E) || (r >= 0x2066 && r <= 0x2069)
}

// cleanURL keeps an absolute http or https address with a host and no credentials, and
// drops anything else (javascript:, file:, data:, user:pass@host).
func cleanURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > maxURLBytes {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || !parsed.IsAbs() || parsed.User != nil || parsed.Hostname() == "" {
		return ""
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		return parsed.String()
	}
	return ""
}

func parsePublished(raw string, now time.Time) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	published, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}
	if published.After(now.Add(futureTolerance)) {
		return time.Time{}
	}
	return published.UTC()
}
