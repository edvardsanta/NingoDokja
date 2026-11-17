package digest

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

const ActionReadWebLink = "read-web-link"

// ReadWebLink reads a user-supplied page without saving it in the Library or
// invoking social readers, which may execute plugins or download media.
func (d *Domain) ReadWebLink(ctx context.Context, address string) (map[string]any, error) {
	u, err := url.Parse(address)
	if err != nil || len(address) > 2048 || u.Hostname() == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("invalid web address")
	}
	if d == nil || d.service == nil {
		return nil, fmt.Errorf("web reader unavailable")
	}
	return d.service.Dispatch(ctx, "feeds.web.read", map[string]any{"url": address})
}

// WebFailureReason exposes only fixed reader categories, never response text,
// transport errors, credentials, challenge tokens or operator diagnostics.
func WebFailureReason(err error) string {
	if err != nil {
		switch {
		case strings.HasPrefix(err.Error(), "web_blocked:"):
			return "blocked"
		case strings.HasPrefix(err.Error(), "web_timeout:"):
			return "timeout"
		case strings.HasPrefix(err.Error(), "web_pdf_unreadable:"):
			return "pdf_unreadable"
		}
	}
	return "unavailable"
}

// SearchWeb asks the search backend for the best results of one query, without saving them. It
// returns the service's untrusted results; callers decide what to show the model.
func (d *Domain) SearchWeb(ctx context.Context, query, language string) (map[string]any, error) {
	query = strings.Join(strings.Fields(query), " ")
	if query == "" || len([]rune(query)) > 300 {
		return nil, fmt.Errorf("invalid search query")
	}
	if d == nil || d.service == nil {
		return nil, fmt.Errorf("web search unavailable")
	}
	payload := map[string]any{"query": query}
	if language != "" {
		payload["language"] = language
	}
	return d.service.Dispatch(ctx, "feeds.web.search", payload)
}
