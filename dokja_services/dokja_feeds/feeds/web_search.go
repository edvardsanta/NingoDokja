//go:build unix

package feeds

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const webSearchEnv = "DOKJA_WEB_SEARCH_URL"
const maxWebSearchResults = 5
const maxWebSearchBytes = 1 << 20
const webSearchTimeout = 8 * time.Second
const maxWebSearchQuery = 300

// searchWeb asks the operator's search instance for the best results of one query. The instance
// address is operator configuration and may be internal; the pages it names are never fetched here,
// and only a title, an address and a snippet leave this method, all of them untrusted text.
func (s *Service) searchWeb(ctx context.Context, payload map[string]any) (map[string]any, error) {
	base, _ := s.lookup(webSearchEnv)
	base = strings.TrimSpace(base)
	if base == "" {
		return nil, fmt.Errorf("web_search_unavailable: no search instance configured")
	}
	query, _ := payload["query"].(string)
	query = strings.Join(strings.Fields(query), " ")
	if query == "" || len([]rune(query)) > maxWebSearchQuery {
		return nil, fmt.Errorf("web_search_invalid: query is empty or too long")
	}
	endpoint, err := url.Parse(base)
	if err != nil || endpoint.Hostname() == "" || endpoint.User != nil || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
		return nil, fmt.Errorf("web_search_unavailable: invalid search instance")
	}
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/search"
	values := url.Values{"q": {query}, "format": {"json"}, "safesearch": {"1"}}
	if language, _ := payload["language"].(string); language != "" && len(language) <= 10 {
		values.Set("language", language)
	}
	endpoint.RawQuery = values.Encode()

	ctx, cancel := context.WithTimeout(ctx, webSearchTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("web_search_unavailable: bad request")
	}
	request.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: webSearchTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("web_timeout: search instance did not answer")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("web_search_unavailable: search instance answered HTTP %d", response.StatusCode)
	}
	var body struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, maxWebSearchBytes)).Decode(&body); err != nil {
		return nil, fmt.Errorf("web_search_unavailable: unreadable answer")
	}
	results := make([]map[string]any, 0, maxWebSearchResults)
	for _, item := range body.Results {
		address, err := webURL(item.URL)
		snippet := cleanText(item.Content, 600)
		if err != nil || snippet == "" {
			continue
		}
		results = append(results, map[string]any{"title": cleanText(item.Title, 160), "url": address.String(), "snippet": snippet})
		if len(results) == maxWebSearchResults {
			break
		}
	}
	return map[string]any{"query": query, "results": results, "retrieved_at": s.now().UTC().Format(time.RFC3339)}, nil
}
