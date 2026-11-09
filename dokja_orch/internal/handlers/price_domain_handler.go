package handlers

import (
	"context"
	"fmt"
	pricedomain "read_books/dokja_domain/dokja_price"
	"read_books/internal/core"
	"read_books/internal/logger"
	"sync/atomic"
	"time"
)

const (
	priceIntakeTaskVersion = "1"
	// priceIntakeDeadline bounds one run. Recording is in-process or a local database
	// write per observation, so a run of a full Feeds page ends well within it.
	priceIntakeDeadline = 5 * time.Minute
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
	return h.domain.Handle(ctx, pricedomain.Request{
		Action: workflow.Action,
		Event: pricedomain.Event{
			ID: event.EventID, Source: string(event.Source), Type: event.Type,
			Payload: event.Payload, Context: event.Context,
		},
	})
}

// PriceIntakeTask hands what Feeds holds to the Price domain. Feeds never calls the domain:
// the orchestrator sequences the two, and the domain decides which items are observations,
// so no list of sources has to be configured.
//
// The scheduler's events are handled one at a time, so Start returns at once and the run goes
// on in the background. Recording is idempotent, so a run that dies is simply redone by the
// next one.
type PriceIntakeTask struct {
	feeds    pricedomainFeeds
	domain   *pricedomain.Domain
	enabled  func() bool
	deadline time.Duration
	running  atomic.Bool
}

// pricedomainFeeds is the Feeds client as the intake uses it.
type pricedomainFeeds interface {
	Dispatch(ctx context.Context, eventType string, payload map[string]any) (map[string]any, error)
}

func NewPriceIntakeTask(feeds pricedomainFeeds, domain *pricedomain.Domain, feedsEnabled func() bool) *PriceIntakeTask {
	return &PriceIntakeTask{feeds: feeds, domain: domain, enabled: feedsEnabled, deadline: priceIntakeDeadline}
}

// Start begins a run unless Feeds is switched off or a run is already going.
func (t *PriceIntakeTask) Start(event core.Event) (map[string]any, error) {
	if t == nil || t.domain == nil {
		return nil, fmt.Errorf("price intake is not configured")
	}
	if t.enabled != nil && !t.enabled() {
		return map[string]any{"skipped": true, "reason": "feeds_switched_off"}, nil
	}
	if !t.running.CompareAndSwap(false, true) {
		return map[string]any{"skipped": true, "reason": "already_running"}, nil
	}
	go func() {
		defer t.running.Store(false)
		t.run(context.Background(), event)
	}()
	return map[string]any{"started": true}, nil
}

type priceIntakeRun struct {
	items   []any
	outcome pricedomain.RecordOutcome
}

func (t *PriceIntakeTask) run(ctx context.Context, event core.Event) (core.TaskResult, *priceIntakeRun, error) {
	run := &priceIntakeRun{}
	result, err := core.RunTask(ctx, event, core.TypedWorkflow{
		Name: "price-intake", Version: priceIntakeTaskVersion, Goal: "keep the prices the collectors observed",
		Deadline: t.deadline,
		Steps: []core.TaskStep{
			{Name: "collect", Domain: core.DomainPrice, Action: "read-feed-items", Run: t.collect(run)},
			{Name: "record", Domain: core.DomainPrice, Action: pricedomain.ActionRecordObservations, Run: t.record(run)},
		},
	}, core.PrototypeTaskLimits())
	if err != nil {
		logger.Error("price intake was not started", err)
		return result, run, err
	}
	// Only the shape of what happened is logged; invalid observations name their plugin and item
	// so a broken collector can be found.
	logger.Info(fmt.Sprintf(
		"price intake attempt=%s status=%s reason=%s items=%d observations=%d points=%d written=%d invalid=%d",
		result.AttemptID, result.Status, result.Reason, run.outcome.Items, run.outcome.Observations,
		run.outcome.Points, run.outcome.Written, len(run.outcome.Invalid),
	))
	for _, invalid := range run.outcome.Invalid {
		logger.Info(fmt.Sprintf("price intake refused plugin=%s item=%s reason=%s", invalid.Plugin, invalid.ID, invalid.Reason))
	}
	return result, run, nil
}

// collect only reads what Feeds already holds; collectors run on their own schedule.
func (t *PriceIntakeTask) collect(run *priceIntakeRun) core.TaskStepFunc {
	return func(ctx context.Context, _ core.StepInput) (core.StepResult, error) {
		page, err := t.feeds.Dispatch(ctx, "feeds.items", nil)
		if err != nil {
			return core.StepResult{Disposition: core.StepFail, Reason: "feeds_unavailable"}, nil
		}
		run.items, _ = page["items"].([]any)
		return core.StepResult{Disposition: core.StepContinue, Output: map[string]any{"items": len(run.items)}}, nil
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
