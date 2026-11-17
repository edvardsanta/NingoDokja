package handlers

import (
	"context"
	"strings"
	"testing"
	"time"

	pricedomain "read_books/dokja_domain/dokja_price"
	"read_books/internal/core"
)

func priceNotificationEvent() core.Event {
	event := chatEvent("/preco produto:ryzen 5 5600")
	event.Context = map[string]any{"message_id": "command-1", "is_owner": true}
	return event
}

func TestPriceNotifierDeliversTheFollowUpWhenTheWatchCollects(t *testing.T) {
	watch := pricedomain.Watch{Category: "ryzen 5 5600"}
	domain := watchedDomain(t, watch)
	resolver := &fakeResolver{}
	deliverer := &fakeProactiveDeliverer{}
	notifier := NewPriceNotifier(&memoryDeferredStore{}, deliverer, domain)

	event := priceNotificationEvent()
	if !notifier.Register(context.Background(), event, watch, "ryzen 5 5600") {
		t.Fatal("promise refused")
	}
	if notifier.Register(context.Background(), event, watch, "ryzen 5 5600") {
		t.Fatal("duplicate promise admitted")
	}

	task := NewPriceIntakeTask(resolver, domain, nil)
	task.notifier = notifier
	if _, _, err := task.run(context.Background(), core.Event{EventID: "event-1", Type: "price.intake.run"}, nil); err != nil {
		t.Fatal(err)
	}
	delivered := waitForDelivery(t, deliverer, 1)
	if delivered[0].replyTo != "command-1" || delivered[0].channelID != "c1" {
		t.Fatalf("follow-up lost its correlation: %#v", delivered[0])
	}
	if !strings.Contains(delivered[0].content, "Chegou preço para **ryzen 5 5600**") ||
		!strings.Contains(delivered[0].content, "459,90") ||
		!strings.Contains(delivered[0].content, "R$ 459,90") ||
		!strings.Contains(delivered[0].content, "store-a") ||
		!strings.Contains(delivered[0].content, "/preco produto:ryzen 5 5600") {
		t.Fatalf("follow-up content wrong: %s", delivered[0].content)
	}
	if strings.ContainsAny(delivered[0].content, "{}") {
		t.Fatalf("a template placeholder leaked into the follow-up: %s", delivered[0].content)
	}
	notifier.mu.Lock()
	remaining := len(notifier.pending)
	notifier.mu.Unlock()
	if remaining != 0 {
		t.Fatal("settled promise still pending")
	}
}

func TestPriceNotificationFormatsPricesAndRendersEveryPlaceholder(t *testing.T) {
	if got := formatCents(45990); got != "459,90" {
		t.Fatalf("formatCents(45990) = %s", got)
	}
	if got := formatCents(105999); got != "1.059,99" {
		t.Fatalf("formatCents(105999) = %s", got)
	}
	event := priceNotificationEvent()
	line := priceNotificationPhrase(event, "offer", "ryzen 5 5600",
		"title", "Processador Example", "merchant", "kabum", "price", formatCents(105999))
	if line != "• Processador Example — kabum R$ 1.059,99" {
		t.Fatalf("offer line rendered wrong: %s", line)
	}
}

func TestPriceNotifierKeepsThePromiseWhenNothingWasRecorded(t *testing.T) {
	watch := pricedomain.Watch{Category: "ryzen 5 5600"}
	domain := watchedDomain(t, watch)
	// A resolver that records nothing for the search.
	resolver := emptyResolver{}
	deliverer := &fakeProactiveDeliverer{}
	notifier := NewPriceNotifier(nil, deliverer, domain)
	if !notifier.Register(context.Background(), priceNotificationEvent(), watch, "ryzen 5 5600") {
		t.Fatal("promise refused")
	}
	task := NewPriceIntakeTask(resolver, domain, nil)
	task.notifier = notifier
	if _, _, err := task.run(context.Background(), core.Event{EventID: "event-1", Type: "price.intake.run"}, nil); err != nil {
		t.Fatal(err)
	}
	if deliverer.count() != 0 {
		t.Fatal("a follow-up arrived with nothing recorded")
	}
	notifier.mu.Lock()
	remaining := len(notifier.pending)
	notifier.mu.Unlock()
	if remaining != 1 {
		t.Fatal("promise dropped without a settlement")
	}
}

// emptyResolver answers every search with no observations, so a run records nothing.
type emptyResolver struct{}

func (emptyResolver) Dispatch(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
	return map[string]any{"observations": []any{}}, nil
}

func TestPriceNotifierExpiresSilentPromisesWithAFixedNotice(t *testing.T) {
	watch := pricedomain.Watch{Category: "ryzen 5 5600"}
	domain := watchedDomain(t, watch)
	deliverer := &fakeProactiveDeliverer{}
	notifier := NewPriceNotifier(nil, deliverer, domain)
	notifier.now = func() time.Time { return time.Now().Add(-2 * priceNotificationTTL) }
	if !notifier.Register(context.Background(), priceNotificationEvent(), watch, "ryzen 5 5600") {
		t.Fatal("promise refused")
	}
	notifier.now = time.Now
	notifier.ExpireDue(context.Background())
	delivered := waitForDelivery(t, deliverer, 1)
	if !strings.Contains(delivered[0].content, "parei de vigiar") {
		t.Fatalf("expiry notice missing: %s", delivered[0].content)
	}
}

func TestPriceNotifierRestoresPendingPromisesFromItsStore(t *testing.T) {
	store := &memoryDeferredStore{}
	watch := pricedomain.Watch{Category: "ryzen 5 5600"}
	domain := watchedDomain(t, watch)
	before := NewPriceNotifier(store, &fakeProactiveDeliverer{}, domain)
	if !before.Register(context.Background(), priceNotificationEvent(), watch, "ryzen 5 5600") {
		t.Fatal("promise refused")
	}
	after := NewPriceNotifier(store, &fakeProactiveDeliverer{}, domain)
	after.restoreOnce(context.Background())
	after.mu.Lock()
	remaining := len(after.pending)
	after.mu.Unlock()
	if remaining != 1 {
		t.Fatal("promise did not survive the restart")
	}
}

func TestPriceNotificationRegistrationOnlyAtTheDeterministicMoment(t *testing.T) {
	watch := pricedomain.Watch{Category: "ryzen 5 5600"}
	domain := watchedDomain(t, watch)
	deliverer := &fakeProactiveDeliverer{}
	notifier := NewPriceNotifier(nil, deliverer, domain)
	handler := NewPriceDomainHandler(domain, nil).WithPriceNotifier(notifier)
	ctx := context.Background()

	registered := func(result map[string]any, event core.Event) int {
		notifier.mu.Lock()
		notifier.pending = map[string]pendingPriceNotification{}
		notifier.mu.Unlock()
		handler.registerPriceNotification(ctx, event, result)
		notifier.mu.Lock()
		defer notifier.mu.Unlock()
		return len(notifier.pending)
	}
	none := map[string]any{"products": []any{}, "watch": map[string]any{"status": "added", "collected": true, "category": "ryzen 5 5600"}}
	withProducts := map[string]any{"products": []any{map[string]any{"product": "found"}}, "watch": map[string]any{"status": "added", "collected": true, "category": "ryzen 5 5600"}}
	notCollected := map[string]any{"products": []any{}, "watch": map[string]any{"status": "added", "category": "ryzen 5 5600"}}
	watchedNone := map[string]any{"products": []any{}, "watch": map[string]any{"status": "watched", "category": "ryzen 5 5600"}}
	watchedProducts := map[string]any{"products": []any{map[string]any{"product": "found"}}, "watch": map[string]any{"status": "watched", "category": "ryzen 5 5600"}}

	if got := registered(none, priceNotificationEvent()); got != 1 {
		t.Fatal("the deterministic moment did not register")
	}
	if got := registered(watchedNone, priceNotificationEvent()); got != 1 {
		t.Fatal("an existing watched search with nothing did not register")
	}
	whatsapp := priceNotificationEvent()
	whatsapp.Source = core.SourceWhatsApp
	if got := registered(none, whatsapp); got != 0 {
		t.Fatal("a non-Discord turn registered")
	}
	if got := registered(withProducts, priceNotificationEvent()); got != 0 {
		t.Fatal("products present registered")
	}
	if got := registered(watchedProducts, priceNotificationEvent()); got != 0 {
		t.Fatal("a watched search with products registered")
	}
	if got := registered(notCollected, priceNotificationEvent()); got != 0 {
		t.Fatal("an uncollected new watch registered")
	}
}

func TestPriceNotificationFollowupPrefersStoresAndShowsDiscountAndMarket(t *testing.T) {
	result := pricedomain.SearchResult{Products: []pricedomain.ProductAssessments{{
		Product: pricedomain.Product{Key: "pn:example", Title: "Processador Example"},
		Assessments: []pricedomain.Assessment{
			{Key: pricedomain.Key{Product: "pn:example", Merchant: "store-b", Series: pricedomain.SeriesMerchant, Kind: pricedomain.KindCard},
				Latest: pricedomain.Point{On: "2026-11-17", Cents: 120000, ListCents: 150000}},
			{Key: pricedomain.Key{Product: "pn:example", Merchant: "store-a", Series: pricedomain.SeriesMerchant, Kind: pricedomain.KindPix},
				Latest: pricedomain.Point{On: "2026-11-17", Cents: 105999, ListCents: 150000}},
			{Key: pricedomain.Key{Product: "pn:example", Merchant: "gamevalio", Series: pricedomain.SeriesMarketMin, Kind: pricedomain.KindUnknown},
				Latest: pricedomain.Point{On: "2026-11-17", Cents: 90000}},
		},
		Market: []pricedomain.Point{{On: "2026-11-17", Cents: 90000}},
	}}}
	content := priceNotificationFollowup(priceNotificationEvent(), "processador example", result)
	// The store offer speaks, never the market aggregate; the discount and the
	// market reference annotate the line.
	if !strings.Contains(content, "store-a R$ 1.059,99") {
		t.Fatalf("the cheapest store offer is missing: %s", content)
	}
	if strings.Contains(content, "gamevalio R$") {
		t.Fatalf("the market aggregate spoke for the product: %s", content)
	}
	if !strings.Contains(content, "mercado mín. R$ 900,00") {
		t.Fatalf("the market reference is missing: %s", content)
	}
	if !strings.Contains(content, "(de R$ 1.500,00)") {
		t.Fatalf("the list-price discount is missing: %s", content)
	}
}
