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
