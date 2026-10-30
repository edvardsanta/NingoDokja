// Package digest decides what the reading screen shows: which items, in what order, what is a
// duplicate and what is worth showing. The service only follows sources and the screen never
// ranks, so this is the one place those rules live.
package digest

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	ActionInspectDigest = "inspect-digest"
	ActionReadDigest    = "read-digest"
)

const (
	defaultLimit = 12
	maxLimit     = 50
	maxOffset    = 1000

	// defaultPerSource is how many items one source may take of the first part of the digest.
	defaultPerSource = 3
	maxPerSource     = 10

	// defaultMaxAgeHours is how old a dated item may be and still be worth showing.
	defaultMaxAgeHours = 168
	maxMaxAgeHours     = 8760
)

type Event struct {
	ID      string
	Source  string
	Type    string
	Payload map[string]any
	Context map[string]any
}

type Request struct {
	Event  Event
	Action string
}

// Service is the feeds service. Dispatch sends one feeds.* event to it.
type Service interface {
	Dispatch(ctx context.Context, eventType string, payload map[string]any) (map[string]any, error)
}

type Domain struct {
	service Service
	now     func() time.Time
}

func New(service Service) *Domain {
	return &Domain{service: service, now: time.Now}
}

// WithClock replaces the clock the freshness window is measured against.
func (d *Domain) WithClock(now func() time.Time) *Domain {
	d.now = now
	return d
}

func (d *Domain) Handle(ctx context.Context, request Request) (map[string]any, error) {
	if d == nil {
		return nil, fmt.Errorf("digest domain is nil")
	}
	if d.service == nil {
		return nil, fmt.Errorf("digest service is not configured")
	}

	switch request.Action {
	case ActionInspectDigest:
		return d.service.Dispatch(ctx, "feeds.status", nil)
	case ActionReadDigest:
		return d.read(ctx, request.Event.Payload)
	case ActionReadLink:
		return d.readLink(ctx, request.Event.Payload)
	}
	return nil, fmt.Errorf("unsupported digest action %q for event type %q", request.Action, request.Event.Type)
}

func (d *Domain) read(ctx context.Context, payload map[string]any) (map[string]any, error) {
	chosen, err := policyFrom(payload)
	if err != nil {
		return nil, err
	}
	reply, err := d.service.Dispatch(ctx, "feeds.items", nil)
	if err != nil {
		return nil, err
	}
	items, err := itemsFrom(reply)
	if err != nil {
		return nil, err
	}
	updated, _ := reply["updated"].(string)
	return compose(items, chosen, d.now()).reply(updated), nil
}

// policy is what one request asks for. Every field is optional and clamped, so a caller can
// page through the digest but cannot ask for something unbounded.
type policy struct {
	limit     int
	offset    int
	perSource int
	maxAge    time.Duration
	source    string
	query     string
	from      time.Time
	until     time.Time
}

func policyFrom(payload map[string]any) (policy, error) {
	var chosen policy
	var err error
	if chosen.limit, err = wholeNumber(payload, "limit", defaultLimit, 1, maxLimit); err != nil {
		return policy{}, err
	}
	if chosen.offset, err = wholeNumber(payload, "offset", 0, 0, maxOffset); err != nil {
		return policy{}, err
	}
	if chosen.perSource, err = wholeNumber(payload, "per_source", defaultPerSource, 1, maxPerSource); err != nil {
		return policy{}, err
	}
	hours, err := wholeNumber(payload, "max_age_hours", defaultMaxAgeHours, 1, maxMaxAgeHours)
	if err != nil {
		return policy{}, err
	}
	chosen.maxAge = time.Duration(hours) * time.Hour
	for name, target := range map[string]*string{"source": &chosen.source, "query": &chosen.query} {
		if raw, present := payload[name]; present {
			value, ok := raw.(string)
			if !ok || utf8.RuneCountInString(value) > 200 {
				return policy{}, fmt.Errorf("%s must be text of at most 200 characters", name)
			}
			*target = strings.TrimSpace(value)
		}
	}
	chosen.query = strings.ToLower(chosen.query)
	for name, target := range map[string]*time.Time{"from": &chosen.from, "to": &chosen.until} {
		if raw, present := payload[name]; present && raw != "" {
			value, ok := raw.(string)
			if !ok {
				return policy{}, fmt.Errorf("%s must be a date", name)
			}
			*target, err = time.Parse("2006-01-02", value)
			if err != nil {
				return policy{}, fmt.Errorf("%s must be a date in YYYY-MM-DD format", name)
			}
		}
	}
	if !chosen.from.IsZero() && !chosen.until.IsZero() && chosen.from.After(chosen.until) {
		return policy{}, fmt.Errorf("from must not be later than to")
	}
	if !chosen.until.IsZero() {
		chosen.until = chosen.until.AddDate(0, 0, 1) // inclusive calendar date, exclusive instant
	}
	return chosen, nil
}

// wholeNumber reads an optional integer field. A value of the wrong kind is an error; one out
// of range is moved to the nearest end of the range.
func wholeNumber(payload map[string]any, name string, fallback, low, high int) (int, error) {
	raw, present := payload[name]
	if !present || raw == nil {
		return fallback, nil
	}
	var value float64
	switch number := raw.(type) {
	case float64:
		value = number
	case int:
		value = float64(number)
	case int64:
		value = float64(number)
	case json.Number:
		parsed, err := number.Float64()
		if err != nil {
			return 0, fmt.Errorf("%s must be a whole number", name)
		}
		value = parsed
	default:
		return 0, fmt.Errorf("%s must be a whole number", name)
	}
	if math.IsNaN(value) || math.IsInf(value, 0) || value != math.Trunc(value) {
		return 0, fmt.Errorf("%s must be a whole number", name)
	}
	switch {
	case value < float64(low):
		return low, nil
	case value > float64(high):
		return high, nil
	}
	return int(value), nil
}

// Reference resolves a selected cached reading by identity. The caller supplies no article
// text: the domain takes it from the same feeds cache used to build the reading list.
type Reference struct {
	ID        string
	Title     string
	Summary   string
	Source    string
	Published string
}

// RecentReferences returns bounded excerpts from the sources already collected by
// Feeds. A named source in the question takes priority over other subscriptions.
func (d *Domain) RecentReferences(ctx context.Context, question string, limit int) ([]Reference, error) {
	if d == nil || d.service == nil {
		return nil, fmt.Errorf("reading sources are not configured")
	}
	if limit < 1 || limit > 20 {
		return nil, fmt.Errorf("recent reading limit must be between 1 and 20")
	}
	reply, err := d.service.Dispatch(ctx, "feeds.items", nil)
	if err != nil {
		return nil, err
	}
	items, err := itemsFrom(reply)
	if err != nil {
		return nil, err
	}
	query := strings.ToLower(question)
	selected := ""
	for _, item := range items {
		for _, name := range []string{item.plugin, item.source} {
			name = strings.ToLower(strings.TrimSpace(name))
			if len(name) >= 5 && strings.Contains(query, name) && len(name) > len(selected) {
				selected = name
			}
		}
	}
	if selected != "" {
		matched := make([]item, 0, len(items))
		for _, candidate := range items {
			if strings.ToLower(candidate.plugin) == selected || strings.ToLower(candidate.source) == selected {
				matched = append(matched, candidate)
			}
		}
		items = matched
	}
	// The digest already defines ordering and duplicate handling. Use a generous
	// freshness window so an infrequently updated source still has recent items.
	ordered := compose(items, policy{limit: len(items), perSource: len(items), maxAge: 100 * 365 * 24 * time.Hour}, d.now()).items
	result := make([]Reference, 0, limit)
	for _, item := range ordered {
		bound := func(s string, n int) string {
			if utf8.RuneCountInString(s) > n {
				return string([]rune(s)[:n])
			}
			return s
		}
		source := item.source
		if source == "" {
			source = item.plugin
		}
		published := ""
		if !item.published.IsZero() {
			published = item.published.UTC().Format(time.RFC3339)
		}
		result = append(result, Reference{ID: item.id, Title: bound(item.title, 300), Summary: bound(item.summary, 1000), Source: bound(source, 100), Published: published})
		if len(result) == limit {
			break
		}
	}
	return result, nil
}

func (d *Domain) Reference(ctx context.Context, id string) (Reference, error) {
	if strings.TrimSpace(id) == "" || len(id) > 512 {
		return Reference{}, fmt.Errorf("reading id is required and must be at most 512 bytes")
	}
	if d == nil || d.service == nil {
		return Reference{}, fmt.Errorf("reading sources are not configured")
	}
	reply, err := d.service.Dispatch(ctx, "feeds.items", nil)
	if err != nil {
		return Reference{}, err
	}
	items, err := itemsFrom(reply)
	if err != nil {
		return Reference{}, err
	}
	for _, item := range items {
		if item.id != id {
			continue
		}
		bound := func(s string, n int) string {
			if utf8.RuneCountInString(s) > n {
				return string([]rune(s)[:n])
			}
			return s
		}
		return Reference{ID: id, Title: bound(item.title, 300), Summary: bound(item.summary, 6000), Source: bound(item.source, 100)}, nil
	}
	return Reference{}, fmt.Errorf("selected reading is no longer available; refresh the reading list")
}
