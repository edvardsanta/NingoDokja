//go:build unix

package feeds

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func searchService(base string) *Service {
	return &Service{now: time.Now, lookup: func(name string) (string, bool) {
		if name == webSearchEnv && base != "" {
			return base, true
		}
		return "", false
	}}
}

func TestWebSearchReturnsBoundedResultsAndDropsUnusableOnes(t *testing.T) {
	var gotQuery, gotFormat string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery, gotFormat = r.URL.Query().Get("q"), r.URL.Query().Get("format")
		_, _ = w.Write([]byte(`{"results":[
			{"title":"Kian","url":"https://example.com/kian","content":"A character."},
			{"title":"No snippet","url":"https://example.com/empty","content":""},
			{"title":"Bad scheme","url":"file:///etc/passwd","content":"x"},
			{"title":"A","url":"https://example.com/a","content":"a"},{"title":"B","url":"https://example.com/b","content":"b"},
			{"title":"C","url":"https://example.com/c","content":"c"},{"title":"D","url":"https://example.com/d","content":"d"},
			{"title":"E","url":"https://example.com/e","content":"e"}]}`))
	}))
	defer server.Close()
	reply, err := searchService(server.URL).Dispatch(context.Background(), "feeds.web.search", map[string]any{"query": "  kian   ordem  "})
	if err != nil {
		t.Fatal(err)
	}
	if gotQuery != "kian ordem" || gotFormat != "json" {
		t.Fatalf("query=%q format=%q", gotQuery, gotFormat)
	}
	results := reply["results"].([]map[string]any)
	if len(results) != maxWebSearchResults || results[0]["url"] != "https://example.com/kian" {
		t.Fatalf("results = %#v", results)
	}
	for _, r := range results {
		if strings.HasPrefix(r["url"].(string), "file:") || r["snippet"] == "" {
			t.Fatalf("unusable result kept: %#v", r)
		}
	}
}

func TestWebSearchRefusesWithoutInstanceOrQuery(t *testing.T) {
	if _, err := searchService("").Dispatch(context.Background(), "feeds.web.search", map[string]any{"query": "kian"}); err == nil || !strings.HasPrefix(err.Error(), "web_search_unavailable") {
		t.Fatalf("no instance: %v", err)
	}
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	if _, err := searchService(server.URL).Dispatch(context.Background(), "feeds.web.search", map[string]any{"query": "   "}); err == nil {
		t.Fatal("an empty query must be refused")
	}
	if _, err := searchService(server.URL).Dispatch(context.Background(), "feeds.web.search", map[string]any{"query": "kian"}); err == nil {
		t.Fatal("a non-200 answer must fail")
	}
}
