package digest

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"
)

// minTitleKeyRunes is how long a normalised title must be to count two items as the same entry.
// A short title such as "Update" says nothing about the entry, so only an address can match it.
const minTitleKeyRunes = 16

type item struct {
	id        string
	plugin    string
	title     string
	summary   string
	url       string
	source    string
	published time.Time // the zero time when unknown
}

// itemsFrom reads the items of a feeds.items reply. An entry that is not an object with a title
// is skipped: the service already cleans what plugins print, and one bad entry must not hide
// the rest.
func itemsFrom(reply map[string]any) ([]item, error) {
	value, present := reply["items"]
	if !present {
		return nil, fmt.Errorf("feeds service returned no item list")
	}
	if value == nil {
		return nil, nil // an empty list that was sent as null
	}
	raw, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("feeds service returned no item list")
	}
	items := make([]item, 0, len(raw))
	for _, entry := range raw {
		fields, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		title := text(fields, "title")
		if title == "" {
			continue
		}
		published, _ := time.Parse(time.RFC3339, text(fields, "published"))
		items = append(items, item{
			id:        text(fields, "id"),
			plugin:    text(fields, "plugin"),
			title:     title,
			summary:   text(fields, "summary"),
			url:       text(fields, "url"),
			source:    text(fields, "source"),
			published: published,
		})
	}
	return items, nil
}

func text(fields map[string]any, name string) string {
	value, _ := fields[name].(string)
	return strings.TrimSpace(value)
}

type digest struct {
	items      []item // the whole digest in order, before paging
	offset     int
	limit      int
	duplicates int
	older      int
}

// compose applies the rules, in this order:
//
//  1. an item dated before the freshness window is dropped; one with no date stays;
//  2. the rest are ordered newest first, undated ones after the dated ones, ties keeping the
//     order the service gave;
//  3. an item whose address, or whose long enough title, was already seen is dropped, so the
//     newest copy of an entry is the one that stays;
//  4. no source may take more than perSource places of the first part of the digest; its other
//     items follow after every source's first ones, still newest first.
//
// The result is one total order, so paging through it never repeats or skips an item.
func compose(items []item, chosen policy, now time.Time) digest {
	result := digest{offset: chosen.offset, limit: chosen.limit}

	fresh := make([]item, 0, len(items))
	for _, candidate := range items {
		if !candidate.published.IsZero() && candidate.published.Before(now.Add(-chosen.maxAge)) {
			result.older++
			continue
		}
		fresh = append(fresh, candidate)
	}
	sort.SliceStable(fresh, func(i, j int) bool { return newer(fresh[i], fresh[j]) })

	seenAddress := map[string]bool{}
	seenTitle := map[string]bool{}
	unique := make([]item, 0, len(fresh))
	for _, candidate := range fresh {
		address, title := addressKey(candidate.url), titleKey(candidate.title)
		if (address != "" && seenAddress[address]) || (title != "" && seenTitle[title]) {
			result.duplicates++
			continue
		}
		if address != "" {
			seenAddress[address] = true
		}
		if title != "" {
			seenTitle[title] = true
		}
		unique = append(unique, candidate)
	}

	taken := map[string]int{}
	var first, rest []item
	for _, candidate := range unique {
		source := sourceKey(candidate)
		taken[source]++
		if taken[source] <= chosen.perSource {
			first = append(first, candidate)
		} else {
			rest = append(rest, candidate)
		}
	}
	result.items = append(first, rest...)
	return result
}

// newer orders dated items newest first and puts undated ones last.
func newer(a, b item) bool {
	switch {
	case a.published.IsZero():
		return false
	case b.published.IsZero():
		return true
	}
	return a.published.After(b.published)
}

func sourceKey(candidate item) string {
	if source := strings.ToLower(candidate.source); source != "" {
		return source
	}
	return candidate.plugin
}

// addressKey makes two addresses of the same page equal: scheme, a leading www., letter case of
// the host, a trailing slash, the fragment and the tracking parameters do not count.
func addressKey(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return ""
	}
	query := parsed.Query()
	for name := range query {
		if isTracking(name) {
			query.Del(name)
		}
	}
	key := strings.TrimPrefix(strings.ToLower(parsed.Host), "www.") + strings.TrimRight(parsed.EscapedPath(), "/")
	if encoded := query.Encode(); encoded != "" {
		key += "?" + encoded
	}
	return key
}

func isTracking(name string) bool {
	name = strings.ToLower(name)
	return strings.HasPrefix(name, "utm_") || name == "fbclid" || name == "gclid"
}

// titleKey is the title in lower case with only its letters and digits, one space between words.
// It is empty when the title is too short to say anything about an entry.
func titleKey(title string) string {
	var key strings.Builder
	pendingSpace := false
	length := 0
	for _, r := range strings.ToLower(title) {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			pendingSpace = true
			continue
		}
		if pendingSpace && key.Len() > 0 {
			key.WriteByte(' ')
			length++
		}
		pendingSpace = false
		key.WriteRune(r)
		length++
	}
	if length < minTitleKeyRunes {
		return ""
	}
	return key.String()
}

// reply is the digest page the orchestrator hands to the interfaces.
func (d digest) reply(updated string) map[string]any {
	total := len(d.items)
	start := min(d.offset, total)
	end := min(start+d.limit, total)

	page := make([]map[string]any, 0, end-start)
	for _, shown := range d.items[start:end] {
		published := ""
		if !shown.published.IsZero() {
			published = shown.published.UTC().Format(time.RFC3339)
		}
		page = append(page, map[string]any{
			"id":        shown.id,
			"plugin":    shown.plugin,
			"title":     shown.title,
			"summary":   shown.summary,
			"url":       shown.url,
			"published": published,
			"source":    shown.source,
		})
	}

	sources := map[string]bool{}
	for _, shown := range d.items {
		sources[sourceKey(shown)] = true
	}
	return map[string]any{
		"items":      page,
		"total":      total,
		"offset":     start,
		"more":       total - end,
		"sources":    len(sources),
		"duplicates": d.duplicates,
		"older":      d.older,
		"updated":    updated,
	}
}
