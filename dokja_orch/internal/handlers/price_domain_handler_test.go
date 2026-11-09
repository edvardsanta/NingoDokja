package handlers

import (
	"context"
	"encoding/json"
	"errors"
	pricedomain "read_books/dokja_domain/dokja_price"
	"read_books/internal/core"
	"testing"
	"time"
)

func priceObservationItem(t *testing.T, id, on, price string) map[string]any {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"contract": pricedomain.ContractVersion, "product": "ssd-example-1tb", "title": "SSD Example 1TB",
		"merchant": "store-a", "series": "merchant", "price_kind": "pix", "currency": "BRL",
		"points": []map[string]any{{"on": on, "price": json.Number(price)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{"plugin": "example-prices", "id": id, "title": "SSD Example 1TB", "kind": "price", "body": string(body)}
}

func TestPriceIntakeRecordsObservationsAndLeavesOtherItems(t *testing.T) {
	today := time.Now().UTC().Format("2006-01-02")
	feeds := &fakeFeeds{items: []any{
		priceObservationItem(t, "p1", today, "459.90"),
		intakeFeedItem("an-article"),
	}}
	domain := pricedomain.New(pricedomain.NewMemoryRepository())
	task := NewPriceIntakeTask(feeds, domain, nil)

	result, run, err := task.run(context.Background(), core.Event{EventID: "event-1", Type: "price.intake.run"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != core.TaskCompleted {
		t.Fatalf("status %s reason %s", result.Status, result.Reason)
	}
	if len(feeds.calls) != 1 || feeds.calls[0] != "feeds.items" {
		t.Fatalf("feeds calls: %v", feeds.calls)
	}
	if run.outcome.Items != 2 || run.outcome.Observations != 1 || run.outcome.Written != 1 {
		t.Fatalf("outcome %+v", run.outcome)
	}

	// A second run over the same Feeds page writes nothing.
	_, again, _ := task.run(context.Background(), core.Event{EventID: "event-2", Type: "price.intake.run"})
	if again.outcome.Written != 0 {
		t.Fatalf("replay wrote %d", again.outcome.Written)
	}
}

func TestPriceIntakeReportsFeedsBeingDown(t *testing.T) {
	task := NewPriceIntakeTask(&fakeFeeds{err: errors.New("down")}, pricedomain.New(pricedomain.NewMemoryRepository()), nil)
	result, _, err := task.run(context.Background(), core.Event{EventID: "event-1", Type: "price.intake.run"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status == core.TaskCompleted || result.Reason != "feeds_unavailable" {
		t.Fatalf("status %s reason %s", result.Status, result.Reason)
	}
}

func TestPriceIntakeSkipsWhenFeedsIsSwitchedOffOrAlreadyRunning(t *testing.T) {
	domain := pricedomain.New(pricedomain.NewMemoryRepository())
	off := NewPriceIntakeTask(&fakeFeeds{}, domain, func() bool { return false })
	if got, _ := off.Start(core.Event{}); got["reason"] != "feeds_switched_off" {
		t.Fatalf("switched off: %v", got)
	}

	feeds := &fakeFeeds{started: make(chan struct{}), release: make(chan struct{})}
	busy := NewPriceIntakeTask(feeds, domain, nil)
	if got, _ := busy.Start(core.Event{}); got["started"] != true {
		t.Fatalf("first start: %v", got)
	}
	<-feeds.started
	if got, _ := busy.Start(core.Event{}); got["reason"] != "already_running" {
		t.Fatalf("second start: %v", got)
	}
	close(feeds.release)
}

func TestPriceHandlerRoutesIntakeAndAssessment(t *testing.T) {
	today := time.Now().UTC().Format("2006-01-02")
	domain := pricedomain.New(pricedomain.NewMemoryRepository())
	if _, err := domain.Record(context.Background(), []any{priceObservationItem(t, "p1", today, "100")}); err != nil {
		t.Fatal(err)
	}
	handler := NewPriceDomainHandler(domain, nil)
	if handler.Domain() != core.DomainPrice {
		t.Fatalf("domain %s", handler.Domain())
	}

	got, err := handler.Handle(context.Background(),
		core.Event{Type: "price.assess", Payload: map[string]any{"product": "ssd-example-1tb"}},
		core.WorkflowStep{Action: pricedomain.ActionAssessPrice})
	if err != nil {
		t.Fatal(err)
	}
	if got["count"] != 1 {
		t.Fatalf("assessment %v", got)
	}

	if _, err := handler.Handle(context.Background(), core.Event{Type: "price.intake.run"},
		core.WorkflowStep{Action: pricedomain.ActionIntakePrice}); err == nil {
		t.Fatal("intake without a task was accepted")
	}
}

func TestPriceEventsRouteToThePriceDomain(t *testing.T) {
	router := core.NewRuleBasedEventRouter()
	engine := core.NewDefaultWorkflowEngine()
	for eventType, action := range map[string]string{
		"price.intake.run": pricedomain.ActionIntakePrice,
		"price.assess":     pricedomain.ActionAssessPrice,
		"price.search":     pricedomain.ActionSearchPrices,
		"price.watches":    pricedomain.ActionListWatches,
		"price.unwatch":    pricedomain.ActionUnwatch,
	} {
		event := core.Event{Type: eventType, Source: core.SourceOrchestrator}
		route, err := router.Route(context.Background(), event)
		if err != nil {
			t.Fatal(err)
		}
		if len(route.Domains) != 1 || route.Domains[0] != core.DomainPrice {
			t.Fatalf("%s routed to %v", eventType, route.Domains)
		}
		plan, err := engine.Plan(context.Background(), event, route)
		if err != nil {
			t.Fatal(err)
		}
		if len(plan.Steps) == 0 || plan.Steps[0].Action != action {
			t.Fatalf("%s planned %+v", eventType, plan)
		}
	}
}

func TestPriceHandlerPassesTheOwnerMarkToTheDomain(t *testing.T) {
	handler := NewPriceDomainHandler(pricedomain.New(pricedomain.NewMemoryRepository()), nil)
	search := func(owner bool) any {
		got, err := handler.Handle(context.Background(), core.Event{
			Type: "price.search", Source: core.SourceDiscord,
			Payload: map[string]any{"query": "ssd", "brand": "samsung"},
			Context: map[string]any{"is_owner": owner},
		}, core.WorkflowStep{Action: pricedomain.ActionSearchPrices})
		if err != nil {
			t.Fatal(err)
		}
		return got["watch"].(map[string]any)["status"]
	}
	if got := search(false); got != "not_owner" {
		t.Fatalf("a guest: %v", got)
	}
	if got := search(true); got != "added" {
		t.Fatalf("the owner: %v", got)
	}
}
