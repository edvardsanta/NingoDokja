package digest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const ActionReadLink = "read-link"

// SocialCapability is what a post reader provides in Feeds.
const SocialCapability = "social.resolve.v1"

const (
	maxLinkChars    = 2048
	maxReaders      = 8
	maxMedia        = 20
	maxAuthorRunes  = 200
	maxCaptionRunes = 8000
)

// The reasons a link is not shown. A reader's own words never reach the screen: they are mapped
// to one of these, which the interfaces word themselves.
const (
	RefusedUnsupported   = "unsupported"
	RefusedLoginRequired = "login_required"
	RefusedUnavailable   = "unavailable"
	RefusedNoReader      = "no_reader"
	RefusedFailed        = "failed"
)

// readLink asks the enabled post readers, in id order, for one link and takes the first that
// handles it. A reader that refuses ends the search: it said the link is its own.
func (d *Domain) readLink(ctx context.Context, payload map[string]any) (map[string]any, error) {
	link, err := linkFrom(payload)
	if err != nil {
		return nil, err
	}
	readers, err := d.readers(ctx)
	if err != nil {
		return nil, err
	}
	if len(readers) == 0 {
		return refused(RefusedNoReader), nil
	}
	failures := 0
	for _, reader := range readers {
		reply, err := d.service.Dispatch(ctx, "feeds.invoke", map[string]any{
			"capability": SocialCapability, "action": "read", "plugin": reader, "input": map[string]any{"url": link},
		})
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			failures++
			continue
		}
		if handled, _ := reply["handled"].(bool); !handled {
			continue
		}
		if reason, _ := reply["refused"].(string); reason != "" {
			return refused(refusal(reason)), nil
		}
		post, ok := postFrom(reply["post"])
		if !ok {
			failures++
			continue
		}
		post["reader"] = reader
		return map[string]any{"status": "ok", "post": post}, nil
	}
	if failures > 0 {
		return refused(RefusedFailed), nil
	}
	return refused(RefusedUnsupported), nil
}

func refused(reason string) map[string]any {
	return map[string]any{"status": "refused", "refused": reason}
}

func refusal(reason string) string {
	switch reason {
	case RefusedLoginRequired, RefusedUnavailable, RefusedUnsupported:
		return reason
	}
	return RefusedFailed
}

func linkFrom(payload map[string]any) (string, error) {
	raw, _ := payload["url"].(string)
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > maxLinkChars {
		return "", fmt.Errorf("url must be a link of 1 to %d characters", maxLinkChars)
	}
	if !isWebAddress(raw) {
		return "", fmt.Errorf("url must be an http(s) link without credentials")
	}
	return raw, nil
}

func isWebAddress(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != "" && parsed.User == nil
}

// readers lists the enabled providers of the capability by id, so the order never depends on
// the plugins directory.
func (d *Domain) readers(ctx context.Context) ([]string, error) {
	status, err := d.service.Dispatch(ctx, "feeds.status", nil)
	if err != nil {
		return nil, err
	}
	rows, _ := status["providers"].([]any)
	var ids []string
	for _, row := range rows {
		provider, _ := row.(map[string]any)
		id, _ := provider["id"].(string)
		if enabled, _ := provider["enabled"].(bool); !enabled || id == "" {
			continue
		}
		provides, _ := provider["provides"].([]any)
		for _, capability := range provides {
			if capability == SocialCapability {
				ids = append(ids, id)
				break
			}
		}
	}
	sort.Strings(ids)
	if len(ids) > maxReaders {
		ids = ids[:maxReaders]
	}
	return ids, nil
}

// postFrom keeps only what the screen may show, each field bounded. A post without a valid
// address is not a post.
func postFrom(value any) (map[string]any, bool) {
	fields, _ := value.(map[string]any)
	address, _ := fields["url"].(string)
	if !isWebAddress(address) || len(address) > maxLinkChars {
		return nil, false
	}
	post := map[string]any{
		"url":         address,
		"document_id": documentID(address),
		"author":      bounded(fields["author"], maxAuthorRunes),
		"caption":     bounded(fields["caption"], maxCaptionRunes),
		"published":   "",
		"media":       mediaFrom(fields["media"]),
	}
	if published, _ := fields["published"].(string); published != "" {
		if at, err := time.Parse(time.RFC3339, published); err == nil {
			post["published"] = at.UTC().Format(time.RFC3339)
		}
	}
	return post, true
}

func mediaFrom(value any) []map[string]any {
	rows, _ := value.([]any)
	media := []map[string]any{}
	for _, row := range rows {
		if len(media) == maxMedia {
			break
		}
		fields, _ := row.(map[string]any)
		kind, _ := fields["kind"].(string)
		address, _ := fields["url"].(string)
		if (kind != "image" && kind != "video") || !isWebAddress(address) || len(address) > maxLinkChars {
			continue
		}
		entry := map[string]any{"kind": kind, "url": address}
		if thumbnail, _ := fields["thumbnail"].(string); isWebAddress(thumbnail) && len(thumbnail) <= maxLinkChars {
			entry["thumbnail"] = thumbnail
		}
		media = append(media, entry)
	}
	return media
}

func bounded(value any, limit int) string {
	text, _ := value.(string)
	text = strings.ToValidUTF8(strings.TrimSpace(text), "")
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	return string([]rune(text)[:limit])
}

// documentID is the Library id of a saved post: the same post saved twice, under another
// tracking parameter or with a trailing slash, is one document.
func documentID(address string) string {
	sum := sha256.Sum256([]byte(addressKey(address)))
	return "post-" + hex.EncodeToString(sum[:8])
}
