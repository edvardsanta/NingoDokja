package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	pricedomain "read_books/dokja_domain/dokja_price"
	"read_books/internal/core"
	"read_books/internal/logger"
	"sync"
	"time"
)

const (
	priceIntakeTaskVersion = "2"
	// priceIntakeDeadline bounds one run: the collector's store queries and the recording.
	priceIntakeDeadline = 6 * time.Minute
	// priceSearchLimit bounds the products one watched search may bring in.
	priceSearchLimit = 30
	// priceMarketLookups bounds how many products a run asks the collector to find a
	// market-low history for; the rest wait for later runs.
	priceMarketLookups = 5
)

// PriceDomainHandler serves price.* events: the scheduled intake and assessments.
type PriceDomainHandler struct {
	domain *pricedomain.Domain
	intake *PriceIntakeTask
}

func NewPriceDomainHandler(domain *pricedomain.Domain, intake *PriceIntakeTask) *PriceDomainHandler {
	return &PriceDomainHandler{domain: domain, intake: intake}
}

func (h *PriceDomainHandler) Domain() core.Domain {
	return core.DomainPrice
}

func (h *PriceDomainHandler) Handle(ctx context.Context, event core.Event, workflow core.WorkflowStep) (map[string]any, error) {
	if h == nil || h.domain == nil {
		return nil, fmt.Errorf("price domain is not configured")
	}
	if workflow.Action == pricedomain.ActionIntakePrice {
		if h.intake == nil {
			return nil, fmt.Errorf("price intake is not configured")
		}
		return h.intake.Start(event)
	}
	result, err := h.domain.Handle(ctx, pricedomain.Request{
		Action: workflow.Action,
		Event: pricedomain.Event{
			ID: event.EventID, Source: string(event.Source), Type: event.Type,
			Payload: event.Payload, Context: event.Context,
		},
	})
	// A search the owner just started to watch is collected now, not at the next scheduled run.
	if err == nil && h.intake != nil {
		if watch, ok := result["watch"].(map[string]any); ok && watch["status"] == "added" {
			category, _ := watch["category"].(string)
			brand, _ := watch["brand"].(string)
			h.intake.StartFor(event, pricedomain.Watch{Category: category, Brand: brand})
		}
	}
	return result, err
}

// PriceIntakeTask collects the watched searches. It asks the Price domain what to look for,
// invokes the Feeds price resolver with it, and hands what comes back to the domain, which
// keeps only valid observations. Feeds never calls the domain: the orchestrator sequences them.
//
// Runs happen one at a time in the background. A scheduled run collects every watched search;
// a search the owner just added is collected on its own. Asked while a run is going, both wait
// and run next, so nothing asked is dropped. Recording is idempotent, so a run that dies is
// simply redone by the next one.
type PriceIntakeTask struct {
	feeds    pricedomainFeeds
	domain   *pricedomain.Domain
	enabled  func() bool
	deadline time.Duration

	mu      sync.Mutex
	running bool
	full    bool
	pending map[pricedomain.Watch]bool
	event   core.Event
	idle    chan struct{} // closed when a drain ends; tests wait on it
}

// pricedomainFeeds is the Feeds client as the intake uses it.
type pricedomainFeeds interface {
	Dispatch(ctx context.Context, eventType string, payload map[string]any) (map[string]any, error)
}

func NewPriceIntakeTask(feeds pricedomainFeeds, domain *pricedomain.Domain, feedsEnabled func() bool) *PriceIntakeTask {
	return &PriceIntakeTask{
		feeds: feeds, domain: domain, enabled: feedsEnabled, deadline: priceIntakeDeadline,
		pending: map[pricedomain.Watch]bool{}, idle: closedChannel(),
	}
}

func closedChannel() chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
}

// Start collects every watched search, unless Feeds is switched off.
func (t *PriceIntakeTask) Start(event core.Event) (map[string]any, error) {
	if t == nil || t.domain == nil {
		return nil, fmt.Errorf("price intake is not configured")
	}
	if t.enabled != nil && !t.enabled() {
		return map[string]any{"skipped": true, "reason": "feeds_switched_off"}, nil
	}
	return t.ask(event, func() { t.full = true }), nil
}

// StartFor collects one watched search, the one the owner just asked for.
func (t *PriceIntakeTask) StartFor(event core.Event, watch pricedomain.Watch) {
	if t == nil || t.domain == nil || (t.enabled != nil && !t.enabled()) {
		return
	}
	t.ask(event, func() { t.pending[watch] = true })
}

func (t *PriceIntakeTask) ask(event core.Event, mark func()) map[string]any {
	t.mu.Lock()
	defer t.mu.Unlock()
	mark()
	t.event = event
	if t.running {
		return map[string]any{"queued": true}
	}
	t.running = true
	t.idle = make(chan struct{})
	go t.drain()
	return map[string]any{"started": true}
}

// drain runs what was asked until nothing is left.
func (t *PriceIntakeTask) drain() {
	for {
		t.mu.Lock()
		full, event := t.full, t.event
		only := make([]pricedomain.Watch, 0, len(t.pending))
		for watch := range t.pending {
			only = append(only, watch)
		}
		t.full, t.pending = false, map[pricedomain.Watch]bool{}
		if !full && len(only) == 0 {
			t.running = false
			close(t.idle)
			t.mu.Unlock()
			return
		}
		t.mu.Unlock()
		if full {
			only = nil // every watched search
		}
		t.run(context.Background(), event, only)
	}
}

// wait blocks until the intake has nothing left to do; for tests.
func (t *PriceIntakeTask) wait() {
	t.mu.Lock()
	idle := t.idle
	t.mu.Unlock()
	<-idle
}

type priceIntakeRun struct {
	searches []pricedomain.Watch
	market   []pricedomain.Product
	items    []any
	refused  int
	outcome  pricedomain.RecordOutcome
}

func (t *PriceIntakeTask) run(ctx context.Context, event core.Event, only []pricedomain.Watch) (core.TaskResult, *priceIntakeRun, error) {
	run := &priceIntakeRun{}
	result, err := core.RunTask(ctx, event, core.TypedWorkflow{
		Name: "price-intake", Version: priceIntakeTaskVersion, Goal: "keep the prices of the watched searches",
		Deadline: t.deadline,
		Steps: []core.TaskStep{
			{Name: "plan", Domain: core.DomainPrice, Action: pricedomain.ActionListWatches, Run: t.plan(run, only)},
			{Name: "collect", Domain: core.DomainPrice, Action: "resolve-prices", Run: t.collect(run)},
			{Name: "record", Domain: core.DomainPrice, Action: pricedomain.ActionRecordObservations, Run: t.record(run)},
		},
	}, core.PrototypeTaskLimits())
	if err != nil {
		logger.Error("price intake was not started", err)
		return result, run, err
	}
	// Only the shape of what happened is logged; invalid observations name the item so a broken
	// collector can be found.
	logger.Info(fmt.Sprintf(
		"price intake attempt=%s status=%s reason=%s searches=%d market_lookups=%d observations=%d points=%d written=%d invalid=%d",
		result.AttemptID, result.Status, result.Reason, len(run.searches), len(run.market), run.outcome.Observations,
		run.outcome.Points, run.outcome.Written, len(run.outcome.Invalid),
	))
	for _, invalid := range run.outcome.Invalid {
		logger.Info(fmt.Sprintf("price intake refused item=%s reason=%s", invalid.ID, invalid.Reason))
	}
	return result, run, nil
}

// plan asks the domain what to collect: the given searches, or every watched one.
func (t *PriceIntakeTask) plan(run *priceIntakeRun, only []pricedomain.Watch) core.TaskStepFunc {
	return func(ctx context.Context, _ core.StepInput) (core.StepResult, error) {
		run.searches = only
		if len(only) == 0 {
			watches, err := t.domain.Watches(ctx)
			if err != nil {
				return core.StepResult{Disposition: core.StepFail, Reason: "price_watches_unavailable"}, nil
			}
			run.searches = watches
		}
		if len(run.searches) == 0 {
			return core.StepResult{Disposition: core.StepFail, Reason: "no_watches"}, nil
		}
		market, err := t.domain.MissingMarket(ctx, priceMarketLookups)
		if err != nil {
			return core.StepResult{Disposition: core.StepFail, Reason: "price_watches_unavailable"}, nil
		}
		run.market = market
		return core.StepResult{Disposition: core.StepContinue, Output: map[string]any{"searches": len(run.searches), "market_lookups": len(market)}}, nil
	}
}

// collect invokes the Feeds price resolver. What it returns is untrusted until the domain checks
// it, so each observation travels as the body of an item, the one shape the domain validates.
func (t *PriceIntakeTask) collect(run *priceIntakeRun) core.TaskStepFunc {
	return func(ctx context.Context, _ core.StepInput) (core.StepResult, error) {
		searches := make([]any, 0, len(run.searches))
		for _, watch := range run.searches {
			searches = append(searches, map[string]any{"category": watch.Category, "brand": watch.Brand})
		}
		market := make([]any, 0, len(run.market))
		for _, product := range run.market {
			market = append(market, map[string]any{"product": product.Key, "title": product.Title, "part_number": product.PartNumber})
		}
		reply, err := t.feeds.Dispatch(ctx, "feeds.invoke", map[string]any{
			"capability": "price.resolve.v1", "action": "search",
			"input": map[string]any{"searches": searches, "limit": priceSearchLimit, "market": market},
		})
		if err != nil {
			return core.StepResult{Disposition: core.StepFail, Reason: "price_resolver_unavailable"}, nil
		}
		observations, _ := reply["observations"].([]any)
		for index, observation := range observations {
			body, err := json.Marshal(observation)
			if err != nil {
				run.refused++
				continue
			}
			run.items = append(run.items, map[string]any{"id": fmt.Sprintf("resolved-%d", index), "body": string(body)})
		}
		return core.StepResult{Disposition: core.StepContinue, Output: map[string]any{"observations": len(run.items)}}, nil
	}
}

func (t *PriceIntakeTask) record(run *priceIntakeRun) core.TaskStepFunc {
	return func(ctx context.Context, _ core.StepInput) (core.StepResult, error) {
		outcome, err := t.domain.Record(ctx, run.items)
		run.outcome = outcome
		output := map[string]any{
			"observations": outcome.Observations, "points": outcome.Points,
			"written": outcome.Written, "invalid": len(outcome.Invalid),
		}
		if err != nil {
			return core.StepResult{Disposition: core.StepFail, Reason: "price_record_failed", Output: output}, nil
		}
		return core.StepResult{Disposition: core.StepContinue, Output: output}, nil
	}
}
