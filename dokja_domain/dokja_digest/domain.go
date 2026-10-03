// Package digest decides what the reading screen shows: which items, in what order, what is a
// duplicate and what is worth showing. The service only follows sources and the screen never
// ranks, so this is the one place those rules live.
package digest

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"
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
