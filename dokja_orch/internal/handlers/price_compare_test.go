package handlers

import (
	"context"
	"encoding/json"
	"errors"
	pricedomain "read_books/dokja_domain/dokja_price"
	"read_books/internal/core"
	"strings"
	"testing"
	"time"
)

// compareResolver answers the resolver's identify and lookup actions and records what it got.
type compareResolver struct {
	actions  []string
	inputs   []map[string]any
	identify map[string]any
	lookup   []any
	err      error
}

func (r *compareResolver) Dispatch(_ context.Context, _ string, payload map[string]any) (map[string]any, error) {
	action, _ := payload["action"].(string)
	r.actions = append(r.actions, action)
	input, _ := payload["input"].(map[string]any)
	r.inputs = append(r.inputs, input)
	if r.err != nil {
		return nil, r.err
	}
	switch action {
	case "identify":
		return r.identify, nil
	case "lookup":
		return map[string]any{"observations": r.lookup}, nil
	}
	return map[string]any{"observations": []any{}}, nil
}

type fakeCommenter struct {
	messages [][]map[string]string
	reply    string
	err      error
}

func (c *fakeCommenter) GenerateMessages(_ context.Context, messages []map[string]string) (map[string]any, error) {
	c.messages = append(c.messages, messages)
	return map[string]any{"reply": c.reply}, c.err
}

func offerObservation(product, part, merchant string, kind string, price string) map[string]any {
	return map[string]any{
		"contract": pricedomain.ContractVersion, "product": product, "title": "SSD Example 240GB " + part, "part_number": part,
		"merchant": merchant, "series": "merchant", "price_kind": kind, "currency": "BRL", "url": "https://" + merchant + ".example/p",
		"points": []any{map[string]any{"on": time.Now().UTC().Format("2006-01-02"), "price": json.Number(price)}},
	}
}

func compareHandler(resolver *compareResolver, commenter priceCommenter) (*PriceDomainHandler, *pricedomain.Domain) {
	domain := pricedomain.New(pricedomain.NewMemoryRepository())
	handler := NewPriceDomainHandler(domain, NewPriceIntakeTask(resolver, domain, nil))
	if commenter != nil {
		handler.WithCommenter(commenter)
	}
	return handler, domain
}

func runCompare(t *testing.T, handler *PriceDomainHandler, owner bool, payload map[string]any) map[string]any {
	t.Helper()
	got, err := handler.Handle(context.Background(), core.Event{
		Type: "price.compare", Source: core.SourceDiscord, Payload: payload, Context: map[string]any{"is_owner": owner},
	}, core.WorkflowStep{Action: pricedomain.ActionCompareProduct})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestCompareIdentifiesALinkLooksTheProductUpForTheOwnerAndComments(t *testing.T) {
	resolver := &compareResolver{
		identify: map[string]any{"store": "store-z", "store_name": "Store Z", "part_number": "XY650SS-240GT-R", "title": "ssd example 240gb"},
		lookup: []any{
			offerObservation("pn:xy650ss-240gt-r", "XY650SS-240GT-R", "store-a", "pix", "129.90"),
			offerObservation("pn:xy650ss-240gt-r", "XY650SS-240GT-R", "store-b", "unknown", "119.90"),
		},
	}
	commenter := &fakeCommenter{reply: "Compre na store-b, plebeu."}
	handler, _ := compareHandler(resolver, commenter)

	got := runCompare(t, handler, true, map[string]any{"link": "https://store-z.example/produto/10527/ssd-example"})
	if got["found"] != true || got["looked_up"] != true || got["comment"] != "Compre na store-b, plebeu." {
		t.Fatalf("result %v", got)
	}
	if strings.Join(resolver.actions, ",") != "identify,lookup" || resolver.inputs[1]["part_number"] != "XY650SS-240GT-R" {
		t.Fatalf("actions %v inputs %v", resolver.actions, resolver.inputs)
	}
	offers := got["offers"].([]any)
	if offers[0].(map[string]any)["merchant"] != "store-b" {
		t.Fatalf("cheapest first: %v", offers)
	}
	if link := got["link"].(map[string]any); link["store"] != "store-z" || link["known"] != true {
		t.Fatalf("link %v", got["link"])
	}
	// the comment is written from the facts, marked as data, with the persona
	user := commenter.messages[0][1]["content"]
	if !strings.Contains(user, "<<FACTS>>") || !strings.Contains(user, "11990") || !strings.HasPrefix(commenter.messages[0][0]["content"], chatPersona) {
		t.Fatalf("comment prompt %v", commenter.messages[0])
	}
}

func TestCompareNeverLooksUpForAGuestAndStandsWithoutAComment(t *testing.T) {
	resolver := &compareResolver{lookup: []any{offerObservation("pn:x", "XYZ12345", "store-a", "pix", "10")}}
	handler, _ := compareHandler(resolver, &fakeCommenter{err: errors.New("no model")})
	got := runCompare(t, handler, false, map[string]any{"query": "ssd example"})
	if got["found"] != false || len(resolver.actions) != 0 {
		t.Fatalf("guest %v actions %v", got, resolver.actions)
	}

	owner := runCompare(t, handler, true, map[string]any{"query": "XYZ12345"})
	if owner["found"] != true {
		t.Fatalf("owner %v", owner)
	}
	if _, has := owner["comment"]; has {
		t.Fatalf("a failing model must leave no comment: %v", owner["comment"])
	}
}

func TestCompareAnswersAnUnknownLinkWithoutGuessing(t *testing.T) {
	resolver := &compareResolver{err: errors.New("unknown store")}
	handler, _ := compareHandler(resolver, nil)
	got := runCompare(t, handler, true, map[string]any{"link": "https://unknown.example/x"})
	if got["found"] != false || got["link"].(map[string]any)["known"] != false {
		t.Fatalf("unknown link %v", got)
	}
}
