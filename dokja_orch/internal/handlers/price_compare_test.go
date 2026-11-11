package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	replies  []string
	err      error
}

type bindingCommenter struct {
	fakeCommenter
	bindings []string
	failures map[string]bool
	// replies answers a provider with its own text instead of the shared reply
	replies map[string]string
	schemas []map[string]any
}

func (c *bindingCommenter) GenerateJSONWithBinding(_ context.Context, provider, model string, schema map[string]any, messages []map[string]string) (map[string]any, error) {
	c.bindings = append(c.bindings, provider+":"+model)
	c.schemas = append(c.schemas, schema)
	if c.failures[provider] {
		return nil, errors.New("provider unavailable")
	}
	if reply, ok := c.replies[provider]; ok {
		return map[string]any{"reply": reply}, nil
	}
	return c.GenerateMessages(context.Background(), messages)
}

type fakeQueryCache struct {
	queries []string
	hits    int
	puts    int
}

func (c *fakeQueryCache) Get(context.Context, string) ([]string, bool, error) {
	c.hits++
	return c.queries, len(c.queries) > 0, nil
}

func (c *fakeQueryCache) Put(_ context.Context, _ string, queries []string) error {
	c.puts++
	c.queries = append([]string(nil), queries...)
	return nil
}

func (c *fakeCommenter) GenerateMessages(_ context.Context, messages []map[string]string) (map[string]any, error) {
	c.messages = append(c.messages, messages)
	reply := c.reply
	if len(c.replies) > 0 {
		reply = c.replies[0]
		c.replies = c.replies[1:]
	}
	return map[string]any{"reply": reply}, c.err
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
	commenter := &fakeCommenter{replies: []string{`{"queries":[]}`, "Compre na store-b, plebeu."}}
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
	comment := commenter.messages[len(commenter.messages)-1]
	user := comment[1]["content"]
	if !strings.Contains(user, "<<FACTS>>") || !strings.Contains(user, "11990") || !strings.HasPrefix(comment[0]["content"], chatPersona) {
		t.Fatalf("comment prompt %v", comment)
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

func TestGenericComparisonContinuesAfterSeveralVariantsAreCollected(t *testing.T) {
	variants := []any{}
	for i, price := range []string{"1900", "2000", "2100"} {
		o := offerObservation(fmt.Sprintf("variant-%d", i), fmt.Sprintf("GPU-EXAMPLE-%d", i), "store-a", "pix", price)
		o["title"] = fmt.Sprintf("GPU Example Challenger Variant %d", i)
		o["category"] = "gpu"
		variants = append(variants, o)
	}
	handler, _ := compareHandler(&compareResolver{lookup: variants}, nil)
	got := runCompare(t, handler, true, map[string]any{"query": "GPU Example Challenger"})
	if got["found"] != true || got["reference_selection"] == nil || got["decision"] == nil {
		t.Fatalf("comparison still requires a manual variant: %v", got)
	}
	if got["product"].(map[string]any)["product"] != "variant-0" {
		t.Fatalf("reference %v", got)
	}
}

func TestCPUDiscoverySearchesTheCompetingManufacturer(t *testing.T) {
	for _, test := range []struct {
		title string
		want  string
	}{
		{"Processador AMD Ryzen 7 9700X, 8 Cores", "processador intel core i7"},
		{"Processador Intel Core i7 Example", "processador amd ryzen 7"},
		{"Processador Intel Core Ultra 7 Example", "processador amd ryzen 7"},
	} {
		queries := competingCPUQueries(map[string]any{"title": test.title, "category": "cpu"})
		if len(queries) == 0 || queries[0] != test.want {
			t.Fatalf("%s: queries %v", test.title, queries)
		}
		for _, query := range queries {
			if strings.Contains(query, "am5") || strings.Contains(query, "lga") {
				t.Fatalf("discovery constrained to a socket: %v", queries)
			}
		}
	}
	if got := competingCPUQueries(map[string]any{"title": "SSD Example", "category": "ssd"}); len(got) != 0 {
		t.Fatalf("CPU queries for storage: %v", got)
	}
}

func TestComparisonCommentReceivesAlternativeEvidence(t *testing.T) {
	commenter := &fakeCommenter{reply: "Considere o candidato."}
	handler, _ := compareHandler(nil, commenter)
	result := map[string]any{"alternatives": []any{map[string]any{
		"title": "CPU Other", "classification": "similar_cpu_specs",
		"reason_codes": []string{"different_cpu_socket"},
	}}}
	handler.comment(context.Background(), result, time.Now().Add(time.Minute))
	var facts map[string]any
	body := commenter.messages[0][1]["content"]
	body = strings.TrimSuffix(strings.TrimPrefix(body, "<<FACTS>>\n"), "\n<<END FACTS>>")
	if err := json.Unmarshal([]byte(body), &facts); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fmt.Sprint(facts["alternatives"]), "different_cpu_socket") {
		t.Fatalf("missing alternative evidence: %v", facts)
	}
}

func TestCPUComparisonDiscoversCompetitorsWithoutAChatModel(t *testing.T) {
	original := offerObservation("cpu-original", "EXAMPLE-ORIGINAL", "store-a", "pix", "1700")
	original["title"] = "Processador AMD Ryzen 7 Example"
	original["category"] = "cpu"
	original["specifications"] = map[string]any{"cores": 8.0, "threads": 16.0, "socket": "s1"}
	candidate := offerObservation("cpu-other", "EXAMPLE-OTHER", "store-a", "pix", "1500")
	candidate["title"] = "Processador Intel Core Example"
	candidate["category"] = "cpu"
	candidate["specifications"] = map[string]any{"cores": 8.0, "threads": 16.0, "socket": "s2"}
	resolver := &compareResolver{lookup: []any{original, candidate}}
	handler, _ := compareHandler(resolver, nil)
	got := runCompare(t, handler, true, map[string]any{"query": "EXAMPLE-ORIGINAL", "budget_cents": 200000})
	if len(resolver.inputs) < 2 || resolver.inputs[1]["query"] != "processador intel core i7" {
		t.Fatalf("competing brand was not searched: %v", resolver.inputs)
	}
	alternatives, _ := got["alternatives"].([]any)
	if len(alternatives) != 1 {
		t.Fatalf("alternatives %v", got)
	}
	first := alternatives[0].(map[string]any)
	if first["classification"] != "similar_cpu_specs" || first["specifications"] == nil {
		t.Fatalf("technical evidence %v", first)
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

func TestCompetitorQueriesComeFromStructuredProviderOutput(t *testing.T) {
	handler, _ := compareHandler(&compareResolver{}, &fakeCommenter{
		reply: `{"queries":["processador 8 cores 16 threads am5","processador 6 cores 12 threads am5"]}`,
	})
	queries := handler.competitorQueries(context.Background(), map[string]any{
		"title": "Processador 8 cores 16 threads", "category": "cpu",
		"specifications": map[string]any{"cores": float64(8), "threads": float64(16), "socket": "am5"},
	}, map[string]any{"id": "cpu"})
	assert := strings.Join(queries, "|")
	if !strings.Contains(assert, "8 cores 16 threads") || strings.Contains(assert, "ryzen") || strings.Contains(assert, "intel") {
		t.Fatalf("queries %v", queries)
	}
}

func TestCompetitorQueriesUseCacheBeforeProvider(t *testing.T) {
	cache := &fakeQueryCache{queries: []string{"processador 8 cores 16 threads am5"}}
	handler, _ := compareHandler(&compareResolver{}, &fakeCommenter{reply: `{"queries":["provider query"]}`})
	handler.WithCompetitorQueryCache(cache)
	queries := handler.competitorQueries(context.Background(), map[string]any{"title": "CPU", "category": "cpu"}, map[string]any{"id": "cpu"})
	if len(queries) != 1 || queries[0] != cache.queries[0] || cache.hits != 1 || cache.puts != 0 {
		t.Fatalf("cache was not used: queries=%v hits=%d puts=%d", queries, cache.hits, cache.puts)
	}
}

func TestCompetitorPlanningFallsBackInConfiguredOrder(t *testing.T) {
	commenter := &bindingCommenter{
		fakeCommenter: fakeCommenter{reply: `{"queries":["processador 8 cores 16 threads am5"]}`},
		failures:      map[string]bool{"claude_code": true, "codex": true},
	}
	handler, _ := compareHandler(&compareResolver{}, commenter)
	queries := handler.competitorQueries(context.Background(), map[string]any{
		"title": "CPU", "category": "cpu",
	}, map[string]any{"id": "cpu"})
	if len(queries) != 1 || commenter.bindings[0] != "claude_code:haiku" ||
		commenter.bindings[1] != "codex:gpt-6-luna" || commenter.bindings[2] != "ollama:qwen3.5:2b" {
		t.Fatalf("fallback order: bindings=%v queries=%v", commenter.bindings, queries)
	}
}

func TestObservationItemsWithCategoryFillsOnlyMissingCategory(t *testing.T) {
	raw := []any{
		map[string]any{"product": "one", "category": "", "title": "Ryzen"},
		map[string]any{"product": "two", "category": "gpu", "title": "RTX"},
	}
	items := observationItemsWithCategory(raw, "cpu")
	first := items[0].(map[string]any)["body"].(string)
	second := items[1].(map[string]any)["body"].(string)
	if !strings.Contains(first, `"category":"cpu"`) {
		t.Fatalf("missing category was not filled: %s", first)
	}
	if !strings.Contains(second, `"category":"gpu"`) {
		t.Fatalf("existing category was overwritten: %s", second)
	}
}

func TestAPlanThatBreaksTheContractIsRefusedAndTheNextProviderAsked(t *testing.T) {
	commenter := &bindingCommenter{
		replies: map[string]string{
			"claude_code": `{"queries":[{"name":"not a string"}]}`,
			"codex":       `here you go: {"queries":["x"]}`,
			"ollama":      `{"queries":["processador 8 nucleos"]}`,
		},
	}
	handler, _ := compareHandler(&compareResolver{}, commenter)
	queries := handler.competitorQueries(context.Background(), map[string]any{"title": "CPU", "category": "cpu"}, map[string]any{"id": "cpu"})
	if len(queries) != 1 || queries[0] != "processador 8 nucleos" || len(commenter.bindings) != 3 {
		t.Fatalf("queries=%v bindings=%v", queries, commenter.bindings)
	}
	// every provider is handed the schema of the reply it must write
	items := commenter.schemas[0]["properties"].(map[string]any)["queries"].(map[string]any)
	if items["maxItems"] != maxCompetitorQueries || items["items"].(map[string]any)["type"] != "string" {
		t.Fatalf("schema %v", commenter.schemas[0])
	}
}

// deadlineCommenter records how long Ningo was given to comment.
type deadlineCommenter struct {
	fakeCommenter
	left []time.Duration
}

func (c *deadlineCommenter) GenerateMessages(ctx context.Context, messages []map[string]string) (map[string]any, error) {
	if deadline, ok := ctx.Deadline(); ok {
		c.left = append(c.left, time.Until(deadline))
	}
	return c.fakeCommenter.GenerateMessages(ctx, messages)
}

func TestNingoGetsLongerThanAResolverCallToCommentAndNothingPastTheBudget(t *testing.T) {
	resolver := &compareResolver{lookup: []any{offerObservation("pn:ab-1000x1", "AB-1000X1", "store-a", "pix", "10")}}
	commenter := &deadlineCommenter{fakeCommenter: fakeCommenter{reply: "Compre."}}
	handler, _ := compareHandler(resolver, commenter)
	got := runCompare(t, handler, true, map[string]any{"query": "AB-1000X1"})
	if got["comment"] != "Compre." || len(commenter.left) != 2 || commenter.left[1] <= compareStepTimeout {
		t.Fatalf("comment %v, given %v", got["comment"], commenter.left)
	}

	late := &deadlineCommenter{fakeCommenter: fakeCommenter{reply: "Compre."}}
	handler, _ = compareHandler(&compareResolver{lookup: resolver.lookup}, late)
	handler.compareTime = compareCommentFloor / 2
	got = runCompare(t, handler, true, map[string]any{"query": "AB-1000X1"})
	if _, has := got["comment"]; has || len(late.left) != 0 {
		t.Fatalf("with no time left the comparison answers without a comment: %v %v", got["comment"], late.left)
	}
}
