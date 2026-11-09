package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	pricedomain "read_books/dokja_domain/dokja_price"
	"read_books/internal/core"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeResolver stands in for the Feeds price resolver: it records each invoke and answers with
// observations for the categories it was asked, or with an error.
type fakeResolver struct {
	mu      sync.Mutex
	inputs  []map[string]any
	err     error
	started chan struct{}
	release chan struct{}
	extra   []any
}

func (f *fakeResolver) Dispatch(_ context.Context, eventType string, payload map[string]any) (map[string]any, error) {
	if f.started != nil {
		f.started <- struct{}{}
		<-f.release
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if eventType != "feeds.invoke" || payload["capability"] != "price.resolve.v1" || payload["action"] != "search" {
		return nil, fmt.Errorf("unexpected %s %v", eventType, payload)
	}
	input, _ := payload["input"].(map[string]any)
	f.inputs = append(f.inputs, input)
	if f.err != nil {
		return nil, f.err
	}
	observations := append([]any(nil), f.extra...)
	for _, raw := range input["searches"].([]any) {
		search := raw.(map[string]any)
		observations = append(observations, map[string]any{
			"contract": pricedomain.ContractVersion, "product": "found-" + strings.ReplaceAll(search["category"].(string), " ", "-"),
			"title": "Found " + search["category"].(string), "merchant": "store-a", "series": "merchant", "price_kind": "pix",
			"currency": "BRL", "category": search["category"], "brand": search["brand"], "url": "https://store.example/p",
			"points": []any{map[string]any{"on": time.Now().UTC().Format("2006-01-02"), "price": json.Number("459.90")}},
		})
	}
	return map[string]any{"observations": observations}, nil
}

func (f *fakeResolver) categories() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out [][]string
	for _, input := range f.inputs {
		var asked []string
		for _, raw := range input["searches"].([]any) {
			asked = append(asked, raw.(map[string]any)["category"].(string))
		}
		sort.Strings(asked)
		out = append(out, asked)
	}
	return out
}

func watchedDomain(t *testing.T, watches ...pricedomain.Watch) *pricedomain.Domain {
	t.Helper()
	repo := pricedomain.NewMemoryRepository()
	for _, watch := range watches {
		if _, err := repo.AddWatch(context.Background(), watch); err != nil {
			t.Fatal(err)
		}
	}
	return pricedomain.New(repo)
}

func TestPriceIntakeCollectsEveryWatchedSearchAndRecordsWhatIsValid(t *testing.T) {
	resolver := &fakeResolver{extra: []any{map[string]any{"contract": pricedomain.ContractVersion, "product": "Bad Key"}}}
	domain := watchedDomain(t, pricedomain.Watch{Category: "ssd sata", Brand: "samsung"}, pricedomain.Watch{Category: "hd"})
	task := NewPriceIntakeTask(resolver, domain, nil)

	result, run, err := task.run(context.Background(), core.Event{EventID: "event-1", Type: "price.intake.run"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != core.TaskCompleted {
		t.Fatalf("status %s reason %s", result.Status, result.Reason)
	}
	if got := resolver.categories(); !reflect.DeepEqual(got, [][]string{{"hd", "ssd sata"}}) {
		t.Fatalf("asked %v", got)
	}
	if resolver.inputs[0]["limit"] != priceSearchLimit {
		t.Fatalf("input %v", resolver.inputs[0])
	}
	if run.outcome.Observations != 2 || len(run.outcome.Invalid) != 1 {
		t.Fatalf("outcome %+v", run.outcome)
	}
	found, _ := domain.SearchProducts(context.Background(), pricedomain.Search{Query: "ssd sata", Brand: "samsung"})
	if found.Matched != 1 || found.Products[0].Product.Brand != "samsung" {
		t.Fatalf("found %+v", found)
	}

	// the next run is told which products still lack a market history
	task.run(context.Background(), core.Event{EventID: "event-2"}, nil)
	market, _ := resolver.inputs[1]["market"].([]any)
	if len(market) != 2 {
		t.Fatalf("market lookups %v", resolver.inputs[1]["market"])
	}
}

func TestPriceIntakeSaysWhenNothingIsWatchedOrTheResolverIsDown(t *testing.T) {
	result, _, _ := NewPriceIntakeTask(&fakeResolver{}, watchedDomain(t), nil).run(context.Background(), core.Event{EventID: "e"}, nil)
	if result.Status == core.TaskCompleted || result.Reason != "no_watches" {
		t.Fatalf("status %s reason %s", result.Status, result.Reason)
	}
	down := &fakeResolver{err: errors.New("down")}
	result, _, _ = NewPriceIntakeTask(down, watchedDomain(t, pricedomain.Watch{Category: "hd"}), nil).run(context.Background(), core.Event{EventID: "e"}, nil)
	if result.Status == core.TaskCompleted || result.Reason != "price_resolver_unavailable" {
		t.Fatalf("status %s reason %s", result.Status, result.Reason)
	}
}

func TestPriceIntakeSkipsWhenFeedsIsSwitchedOffAndQueuesWhatIsAskedMeanwhile(t *testing.T) {
	off := NewPriceIntakeTask(&fakeResolver{}, watchedDomain(t, pricedomain.Watch{Category: "hd"}), func() bool { return false })
	if got, _ := off.Start(core.Event{}); got["reason"] != "feeds_switched_off" {
		t.Fatalf("switched off: %v", got)
	}

	resolver := &fakeResolver{started: make(chan struct{}), release: make(chan struct{})}
	task := NewPriceIntakeTask(resolver, watchedDomain(t, pricedomain.Watch{Category: "hd"}, pricedomain.Watch{Category: "ssd"}), nil)
	if got, _ := task.Start(core.Event{}); got["started"] != true {
		t.Fatalf("first start: %v", got)
	}
	<-resolver.started
	// asked while the first run is going: queued, then run on its own
	task.StartFor(core.Event{}, pricedomain.Watch{Category: "nvme"})
	if got, _ := task.Start(core.Event{}); got["queued"] != true {
		t.Fatalf("second start: %v", got)
	}
	resolver.release <- struct{}{}
	<-resolver.started
	resolver.release <- struct{}{}
	task.wait()
	got := resolver.categories()
	// the queued full run covers the new search too, since every watched search is collected
	if len(got) != 2 || !reflect.DeepEqual(got[0], []string{"hd", "ssd"}) {
		t.Fatalf("asked %v", got)
	}
}

func TestAnOwnerSearchThatAddsAWatchIsCollectedAtOnce(t *testing.T) {
	resolver := &fakeResolver{}
	domain := watchedDomain(t)
	intake := NewPriceIntakeTask(resolver, domain, nil)
	handler := NewPriceDomainHandler(domain, intake)
	got, err := handler.Handle(context.Background(), core.Event{
		Type: "price.search", Source: core.SourceDiscord, Payload: map[string]any{"watch": true, "query": "SSD Sata", "brand": "Samsung"},
		Context: map[string]any{"is_owner": true},
	}, core.WorkflowStep{Action: pricedomain.ActionSearchPrices})
	if err != nil || got["watch"].(map[string]any)["status"] != "added" {
		t.Fatalf("search %v %v", got, err)
	}
	intake.wait()
	if asked := resolver.categories(); !reflect.DeepEqual(asked, [][]string{{"ssd sata"}}) {
		t.Fatalf("asked %v", asked)
	}
	again, _ := handler.Handle(context.Background(), core.Event{
		Type: "price.search", Source: core.SourceDiscord, Payload: map[string]any{"watch": true, "query": "ssd sata", "brand": "samsung"},
		Context: map[string]any{"is_owner": true},
	}, core.WorkflowStep{Action: pricedomain.ActionSearchPrices})
	intake.wait()
	if again["matched"] != 1 || len(resolver.categories()) != 1 {
		t.Fatalf("a watched search is answered from what is known, not collected again: %v %v", again["matched"], resolver.categories())
	}
}

func TestPriceHandlerRoutesIntakeAndAssessment(t *testing.T) {
	domain := pricedomain.New(pricedomain.NewMemoryRepository())
	body, _ := json.Marshal(map[string]any{"contract": pricedomain.ContractVersion, "product": "ssd-example-1tb", "title": "SSD Example 1TB",
		"merchant": "store-a", "series": "merchant", "price_kind": "pix", "currency": "BRL",
		"points": []any{map[string]any{"on": time.Now().UTC().Format("2006-01-02"), "price": json.Number("100")}}})
	if _, err := domain.Record(context.Background(), []any{map[string]any{"id": "p1", "body": string(body)}}); err != nil {
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
		"price.merchants":  pricedomain.ActionListMerchants,
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
			Payload: map[string]any{"watch": true, "query": "ssd", "brand": "samsung"},
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

func TestPriceIntakeHandsTheKnownMarketPagesBack(t *testing.T) {
	resolver := &fakeResolver{extra: []any{map[string]any{
		"contract": pricedomain.ContractVersion, "product": "found-hd", "title": "Found hd", "merchant": "aggregator",
		"series": "market_min", "price_kind": "unknown", "currency": "BRL", "url": "https://market.example/found-hd",
		"points": []any{map[string]any{"on": time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02"), "price": json.Number("400")}},
	}}}
	task := NewPriceIntakeTask(resolver, watchedDomain(t, pricedomain.Watch{Category: "hd"}), nil)
	task.run(context.Background(), core.Event{EventID: "e1"}, nil)
	task.run(context.Background(), core.Event{EventID: "e2"}, nil)
	market, _ := resolver.inputs[1]["market"].([]any)
	var pages []string
	for _, raw := range market {
		if page, _ := raw.(map[string]any)["page"].(string); page != "" {
			pages = append(pages, page)
		}
	}
	if !reflect.DeepEqual(pages, []string{"https://market.example/found-hd"}) {
		t.Fatalf("pages %v in %v", pages, market)
	}
}
