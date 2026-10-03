package feeds

import (
	"strings"
	"testing"
	"time"
)

var testNow = time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

func testPlugin() Plugin {
	return Plugin{ID: "example", Name: "Example source", MaxItems: 50}
}

func TestCleanText(t *testing.T) {
	reorder := string(rune(0x202E)) + string(rune(0x2066))
	mark := string(rune(0xFEFF))
	cases := map[string]struct {
		in    string
		limit int
		want  string
	}{
		"plain text is kept":               {"An example title", 50, "An example title"},
		"whitespace becomes single spaces": {"  one \n\t two   three  ", 50, "one two three"},
		"control characters are dropped":   {"a\x00b\x07c\x1b[31md", 50, "abc[31md"},
		"display reordering is dropped":    {"ab" + reorder + "cd", 50, "abcd"},
		"a byte order mark is dropped":     {mark + "title", 50, "title"},
		"invalid UTF-8 is dropped":         {"a\xffb", 50, "ab"},
		"a text at the limit is not cut":   {"abcde", 5, "abcde"},
		"a longer text is cut with a mark": {"abcdefgh", 5, "abcd" + string(ellipsis)},
		"only spaces is empty":             {" \n\t ", 5, ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := cleanText(c.in, c.limit); got != c.want {
				t.Fatalf("cleanText(%q, %d) = %q, want %q", c.in, c.limit, got, c.want)
			}
		})
	}
}

func TestCleanURLKeepsOnlyPlainWebAddresses(t *testing.T) {
	kept := map[string]string{
		"https://example.com/a?b=1": "https://example.com/a?b=1",
		"  http://example.com/x  ":  "http://example.com/x",
		"HTTPS://Example.com/Path":  "https://Example.com/Path",
	}
	for in, want := range kept {
		if got := cleanURL(in); got != want {
			t.Errorf("cleanURL(%q) = %q, want %q", in, got, want)
		}
	}

	dropped := []string{
		"", "   ", "/relative/path", "example.com/no-scheme",
		"javascript:alert(1)", "data:text/html,hi", "file:///etc/passwd", "ftp://example.com/a",
		"https://user:pass@example.com/", "https://user@example.com/", "http://", "https:///path",
		"https://example.com/\x00", "https://example.com/" + strings.Repeat("a", maxURLBytes),
	}
	for _, in := range dropped {
		if got := cleanURL(in); got != "" {
			t.Errorf("cleanURL(%q) = %q, want it dropped", in, got)
		}
	}
}

func TestParsePublished(t *testing.T) {
	zone := time.FixedZone("test", 3*3600)
	cases := map[string]struct {
		in   string
		want time.Time
	}{
		"a date with an offset is turned into UTC": {"2026-10-02T09:30:00+03:00", time.Date(2026, 10, 2, 6, 30, 0, 0, time.UTC)},
		"empty means unknown":                      {"", time.Time{}},
		"text that is not a date is unknown":       {"yesterday", time.Time{}},
		"a date without a time zone is unknown":    {"2026-10-02 09:30:00", time.Time{}},
		"a little ahead of the clock is skew":      {testNow.Add(30 * time.Minute).In(zone).Format(time.RFC3339), testNow.Add(30 * time.Minute)},
		"far ahead of the clock is not believed":   {testNow.Add(2 * time.Hour).Format(time.RFC3339), time.Time{}},
		"an old date is kept":                      {"2001-01-01T00:00:00Z", time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := parsePublished(c.in, testNow); !got.Equal(c.want) {
				t.Fatalf("parsePublished(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestNormalizeItem(t *testing.T) {
	t.Run("a title is required", func(t *testing.T) {
		if _, ok := normalizeItem(rawItem{Summary: "text"}, testPlugin(), testNow); ok {
			t.Fatal("an item without a title was kept")
		}
		if _, ok := normalizeItem(rawItem{Title: " \n "}, testPlugin(), testNow); ok {
			t.Fatal("an item with a blank title was kept")
		}
	})

	t.Run("the source defaults to the plugin name", func(t *testing.T) {
		item, _ := normalizeItem(rawItem{Title: "T"}, testPlugin(), testNow)
		if item.Source != "Example source" {
			t.Fatalf("source = %q", item.Source)
		}
		item, _ = normalizeItem(rawItem{Title: "T", Source: "Own name"}, testPlugin(), testNow)
		if item.Source != "Own name" {
			t.Fatalf("source = %q", item.Source)
		}
	})

	t.Run("a missing id is derived and stable", func(t *testing.T) {
		first, _ := normalizeItem(rawItem{Title: "T", URL: "https://example.com/a"}, testPlugin(), testNow)
		again, _ := normalizeItem(rawItem{Title: "T", URL: "https://example.com/a"}, testPlugin(), testNow)
		other, _ := normalizeItem(rawItem{Title: "T", URL: "https://example.com/b"}, testPlugin(), testNow)
		if first.ID == "" || first.ID != again.ID || first.ID == other.ID {
			t.Fatalf("ids %q, %q, %q", first.ID, again.ID, other.ID)
		}
	})

	t.Run("a given id is kept, cleaned", func(t *testing.T) {
		item, _ := normalizeItem(rawItem{ID: "  abc\n123 ", Title: "T"}, testPlugin(), testNow)
		if item.ID != "abc 123" {
			t.Fatalf("id = %q", item.ID)
		}
	})

	t.Run("an unsafe address is dropped but the item stays", func(t *testing.T) {
		item, ok := normalizeItem(rawItem{Title: "T", URL: "javascript:alert(1)"}, testPlugin(), testNow)
		if !ok || item.URL != "" {
			t.Fatalf("ok=%v url=%q", ok, item.URL)
		}
	})

	t.Run("text is bounded", func(t *testing.T) {
		item, _ := normalizeItem(rawItem{Title: strings.Repeat("t", 1000), Summary: strings.Repeat("s", 5000)}, testPlugin(), testNow)
		if n := len([]rune(item.Title)); n != maxTitleRunes {
			t.Fatalf("title has %d characters", n)
		}
		if n := len([]rune(item.Summary)); n != maxSummaryRunes {
			t.Fatalf("summary has %d characters", n)
		}
	})
}

func TestParseOutput(t *testing.T) {
	plugin := testPlugin()
	plugin.MaxItems = 3

	t.Run("a valid list is read in order", func(t *testing.T) {
		items, skipped, err := parseOutput([]byte(`{"items":[{"id":"a","title":"One"},{"id":"b","title":"Two"}]}`), plugin, testNow)
		if err != nil || skipped != 0 || len(items) != 2 || items[0].ID != "a" || items[1].Title != "Two" {
			t.Fatalf("items=%v skipped=%d err=%v", items, skipped, err)
		}
	})

	t.Run("an empty list is fine", func(t *testing.T) {
		items, skipped, err := parseOutput([]byte(`{"items":[]}`), plugin, testNow)
		if err != nil || skipped != 0 || len(items) != 0 {
			t.Fatalf("items=%v skipped=%d err=%v", items, skipped, err)
		}
	})

	failures := map[string]string{
		"output is not valid JSON":   `not json`,
		"output with trailing text":  `{"items":[]} extra`,
		`output has no "items" list`: `{"entries":[]}`,
		"a null list":                `{"items":null}`,
		"an array at the top":        `[]`,
	}
	for name, body := range failures {
		t.Run(name, func(t *testing.T) {
			if _, _, err := parseOutput([]byte(body), plugin, testNow); err == nil {
				t.Fatalf("%s was accepted", body)
			}
		})
	}

	t.Run("an item that cannot be used is skipped and counted, the rest stays", func(t *testing.T) {
		body := `{"items":[
			{"id":"a","title":"Good"},
			{"id":5,"title":"id is a number"},
			{"id":"c"},
			"not an object",
			{"id":"a","title":"Duplicate of the first"},
			{"id":"d","title":"Also good"}
		]}`
		items, skipped, err := parseOutput([]byte(body), plugin, testNow)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 2 || items[0].ID != "a" || items[1].ID != "d" || skipped != 4 {
			t.Fatalf("items=%v skipped=%d", items, skipped)
		}
	})

	t.Run("more items than the manifest allows are dropped and counted", func(t *testing.T) {
		body := `{"items":[{"id":"1","title":"1"},{"id":"2","title":"2"},{"id":"3","title":"3"},{"id":"4","title":"4"},{"id":"5","title":"5"}]}`
		items, skipped, err := parseOutput([]byte(body), plugin, testNow)
		if err != nil || len(items) != 3 || skipped != 2 || items[2].ID != "3" {
			t.Fatalf("items=%v skipped=%d err=%v", items, skipped, err)
		}
	})
}
