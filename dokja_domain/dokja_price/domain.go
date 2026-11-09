package price

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const (
	// ActionRecordObservations keeps the price observations found in feed items.
	ActionRecordObservations = "record-price-observations"
	// ActionAssessPrice judges the latest price of every history of a product, or of
	// every watched product.
	ActionAssessPrice = "assess-price"
	// ActionSearchPrices assesses the watched products that match words, such as a kind of
	// product ("hd") or a brand ("amd").
	ActionSearchPrices = "search-prices"
	// ActionIntakePrice names the scheduled run that reads Feeds and records what it
	// holds. The orchestrator sequences it; the domain only records.
	ActionIntakePrice = "intake-price"
)

const (
	maxItemsPerRecord = 1000
	maxHistoryDays    = 400
)

// Skip reasons are codes, not prose: the caller logs or translates them.
const (
	SkipNoBody  = "no_body"
	SkipOther   = "not_observation"
	SkipInvalid = "invalid"
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

type Domain struct {
	repository Repository
	now        func() time.Time
}

func New(repository Repository) *Domain {
	return &Domain{repository: repository, now: time.Now}
}

func (d *Domain) WithClock(now func() time.Time) *Domain {
	d.now = now
	return d
}

func (d *Domain) Handle(ctx context.Context, request Request) (map[string]any, error) {
	if d == nil || d.repository == nil {
		return nil, fmt.Errorf("price domain is not configured")
	}
	switch request.Action {
	case ActionRecordObservations:
		items, err := itemsFrom(request.Event.Payload)
		if err != nil {
			return nil, err
		}
		outcome, err := d.Record(ctx, items)
		if err != nil {
			return nil, err
		}
		return outcome.Map(), nil
	case ActionAssessPrice:
		product, ok := request.Event.Payload["product"].(string)
		if raw, present := request.Event.Payload["product"]; present && !ok && raw != nil {
			return nil, fmt.Errorf("product must be a string")
		}
		assessments, err := d.AssessProduct(ctx, product)
		if err != nil {
			return nil, err
		}
		rows := make([]any, 0, len(assessments))
		for _, assessment := range assessments {
			rows = append(rows, assessment.Map())
		}
		return map[string]any{"assessments": rows, "count": len(rows)}, nil
	case ActionSearchPrices:
		search := Search{}
		for name, target := range map[string]*string{"product": &search.Product, "query": &search.Query, "brand": &search.Brand} {
			raw, present := request.Event.Payload[name]
			if !present || raw == nil {
				continue
			}
			value, ok := raw.(string)
			if !ok {
				return nil, fmt.Errorf("%s must be a string", name)
			}
			*target = value
		}
		result, err := d.SearchProducts(ctx, search)
		if err != nil {
			return nil, err
		}
		return result.Map(), nil
	}
	return nil, fmt.Errorf("unsupported price action %q for event type %q", request.Action, request.Event.Type)
}

// itemsFrom reads the feed items of a request. Items decoded from JSON arrive as []any;
// items built in Go may arrive as []map[string]any. Any other type is an error, never a
// silently empty batch.
func itemsFrom(payload map[string]any) ([]any, error) {
	switch items := payload["items"].(type) {
	case nil:
		return nil, nil
	case []any:
		return items, nil
	case []map[string]any:
		converted := make([]any, len(items))
		for i, item := range items {
			converted[i] = item
		}
		return converted, nil
	default:
		return nil, fmt.Errorf("items must be a list of feed items, not %T", items)
	}
}

// RecordOutcome counts what one batch of feed items gave.
type RecordOutcome struct {
	Items        int
	Observations int
	Points       int
	Written      int
	Skipped      map[string]int
	// Invalid lists the ids of items that claimed the contract and broke it, with the
	// reason, so a broken collector is visible.
	Invalid []InvalidItem
}

type InvalidItem struct {
	ID     string
	Plugin string
	Reason string
}

func (o RecordOutcome) Map() map[string]any {
	invalid := make([]any, 0, len(o.Invalid))
	for _, item := range o.Invalid {
		invalid = append(invalid, map[string]any{"id": item.ID, "plugin": item.Plugin, "reason": item.Reason})
	}
	skipped := map[string]any{}
	for reason, count := range o.Skipped {
		skipped[reason] = count
	}
	return map[string]any{
		"items": o.Items, "observations": o.Observations, "points": o.Points,
		"written": o.Written, "skipped": skipped, "invalid": invalid,
	}
}

// Record keeps every price observation carried in the bodies of feed items. Items of
// other kinds are counted and left alone, so it can read a whole feed.
func (d *Domain) Record(ctx context.Context, items []any) (RecordOutcome, error) {
	outcome := RecordOutcome{Skipped: map[string]int{}}
	if len(items) > maxItemsPerRecord {
		return outcome, fmt.Errorf("at most %d items per record", maxItemsPerRecord)
	}
	today := d.now()
	for _, raw := range items {
		outcome.Items++
		item, _ := raw.(map[string]any)
		body, _ := item["body"].(string)
		if body == "" {
			outcome.Skipped[SkipNoBody]++
			continue
		}
		observation, err := ParseObservation(body, today)
		if errors.Is(err, ErrNotObservation) {
			outcome.Skipped[SkipOther]++
			continue
		}
		if err != nil {
			outcome.Skipped[SkipInvalid]++
			id, _ := item["id"].(string)
			plugin, _ := item["plugin"].(string)
			outcome.Invalid = append(outcome.Invalid, InvalidItem{ID: id, Plugin: plugin, Reason: err.Error()})
			continue
		}
		written, err := d.repository.Upsert(ctx, observation)
		if err != nil {
			return outcome, fmt.Errorf("keep observation of %s: %w", observation.Product, err)
		}
		outcome.Observations++
		outcome.Points += len(observation.Points)
		outcome.Written += written
	}
	return outcome, nil
}

// AssessProduct judges every history of one product, or of every watched product when
// product is empty.
func (d *Domain) AssessProduct(ctx context.Context, product string) ([]Assessment, error) {
	var products []string
	if product != "" {
		if !keyPattern.MatchString(product) {
			return nil, fmt.Errorf("product must be 1 to 80 lowercase letters, digits, '.', '_', ':' or '-'")
		}
		products = []string{product}
	} else {
		watched, err := d.repository.Products(ctx)
		if err != nil {
			return nil, err
		}
		for _, item := range watched {
			products = append(products, item.Key)
		}
	}
	today := d.now()
	from := today.UTC().AddDate(0, 0, -maxHistoryDays).Format(dateLayout)
	var assessments []Assessment
	for _, name := range products {
		keys, err := d.repository.Keys(ctx, name)
		if err != nil {
			return nil, err
		}
		for _, key := range keys {
			points, err := d.repository.History(ctx, key, from, "")
			if err != nil {
				return nil, err
			}
			assessments = append(assessments, Assess(key, points, today))
		}
	}
	return assessments, nil
}

func (a Assessment) Map() map[string]any {
	signals := make([]any, 0, len(a.Signals))
	for _, signal := range a.Signals {
		signals = append(signals, string(signal))
	}
	return map[string]any{
		"product": a.Key.Product, "merchant": a.Key.Merchant,
		"series": string(a.Key.Series), "price_kind": string(a.Key.Kind),
		"verdict": string(a.Verdict), "signals": signals,
		"latest_on": a.Latest.On, "latest_cents": a.Latest.Cents, "list_cents": a.Latest.ListCents,
		"median_30_cents": a.Median30Cents, "median_90_cents": a.Median90Cents,
		"max_90_cents": a.Max90Cents, "lowest_cents": a.LowestCents,
		"discount_vs_30_bps": a.DiscountVs30Bps, "advertised_discount_bps": a.AdvertisedDiscountBps,
		"history_days": a.HistoryDays,
	}
}
