package handlers

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeHealth struct{ err error }

func (f fakeHealth) Health(context.Context) error { return f.err }

type fakeStatus struct {
	result map[string]any
	err    error
}

func (f fakeStatus) Status(context.Context) (map[string]any, error) { return f.result, f.err }

func statusServices(t *testing.T, handler *SystemDomainHandler) (map[string]any, map[string]any) {
	t.Helper()
	event, step := adminEvent("ningo.status", map[string]any{})
	result, err := handler.Handle(context.Background(), event, step)
	if err != nil {
		t.Fatal(err)
	}
	return result, result["services"].(map[string]any)
}

func TestStatusProbesBookKnowledgeAndMemory(t *testing.T) {
	handler, _ := controlledHandler(t)
	handler.WithServiceProbes(map[string]ServiceProbe{
		"book":      HealthProbe(fakeHealth{}),
		"knowledge": StatusProbe(fakeStatus{result: map[string]any{"documents": 2, "embedder_reachable": true}}),
		"memory":    StatusProbe(fakeStatus{result: map[string]any{"experiences": 3, "embeddings": true, "embedder_reachable": true}}),
	})

	_, services := statusServices(t, handler)

	for _, name := range []string{"book", "knowledge", "memory"} {
		entry := services[name].(map[string]any)
		if entry["status"] != "ok" || entry["enabled"] != true {
			t.Fatalf("%s should be ok and enabled, got %#v", name, entry)
		}
		if _, noisy := entry["detail"]; noisy {
			t.Fatalf("%s is healthy, it needs no detail: %#v", name, entry)
		}
	}
}

func TestStatusReportsAFailingProbeAndDegradesThePlatform(t *testing.T) {
	handler, _ := controlledHandler(t)
	handler.WithServiceProbes(map[string]ServiceProbe{
		"book":      HealthProbe(fakeHealth{err: errors.New("call book health: connection refused")}),
		"knowledge": StatusProbe(fakeStatus{err: errors.New("resource temporarily unavailable")}),
		"memory":    StatusProbe(fakeStatus{result: map[string]any{"experiences": 3}}),
	})

	result, services := statusServices(t, handler)

	book := services["book"].(map[string]any)
	if book["status"] != "error" || book["error"] != "call book health: connection refused" {
		t.Fatalf("unexpected book entry %#v", book)
	}
	if services["knowledge"].(map[string]any)["status"] != "error" {
		t.Fatalf("a failed status request is an error: %#v", services["knowledge"])
	}
	if services["memory"].(map[string]any)["status"] != "ok" {
		t.Fatalf("one failing probe must not spoil the others: %#v", services["memory"])
	}
	if result["status"] != "degraded" {
		t.Fatalf("an enabled service that fails is degraded, got %v", result["status"])
	}
}

func TestStatusKeepsAServiceThatCannotEmbedOkWithANote(t *testing.T) {
	handler, _ := controlledHandler(t)
	handler.WithServiceProbes(map[string]ServiceProbe{
		"knowledge": StatusProbe(fakeStatus{result: map[string]any{"embedder_reachable": false}}),
		"memory":    StatusProbe(fakeStatus{result: map[string]any{"embeddings": false, "embedder_reachable": false, "degraded": true}}),
	})

	result, services := statusServices(t, handler)

	knowledge := services["knowledge"].(map[string]any)
	memory := services["memory"].(map[string]any)
	if knowledge["status"] != "ok" || knowledge["detail"] != "embedder unreachable" {
		t.Fatalf("unexpected knowledge entry %#v", knowledge)
	}
	if memory["status"] != "ok" || memory["detail"] != "embeddings off" {
		t.Fatalf("unexpected memory entry %#v", memory)
	}
	if result["status"] != "degraded" {
		// chat_ai in controlledHandler always fails; the embedder notes alone must not add to it.
		t.Fatalf("unexpected platform status %v", result["status"])
	}
}

func TestStatusDoesNotProbeASwitchedOffService(t *testing.T) {
	handler, controls := controlledHandler(t)
	calls := 0
	handler.WithServiceProbes(map[string]ServiceProbe{
		"book": func(context.Context) (string, error) {
			calls++
			return "", errors.New("must not be called")
		},
	})
	if err := controls.SetService("book", false); err != nil {
		t.Fatal(err)
	}
	if err := controls.SetService("chat_ai", false); err != nil {
		t.Fatal(err)
	}

	result, services := statusServices(t, handler)

	if calls != 0 {
		t.Fatalf("a switched-off service must not be probed, got %d calls", calls)
	}
	if book := services["book"].(map[string]any); book["status"] != "disabled" || book["enabled"] != false {
		t.Fatalf("unexpected book entry %#v", book)
	}
	if result["status"] != "ok" {
		t.Fatalf("a switched-off service must not make the platform degraded, got %v", result["status"])
	}
}

func TestStatusLeavesServicesWithoutAProbeUnchecked(t *testing.T) {
	handler, _ := controlledHandler(t)
	handler.WithServiceProbes(map[string]ServiceProbe{"book": HealthProbe(fakeHealth{}), "memory": nil})

	_, services := statusServices(t, handler)

	if services["memory"].(map[string]any)["status"] != "unchecked" {
		t.Fatalf("a service with no probe stays unchecked: %#v", services["memory"])
	}
}

func TestStatusRunsProbesAtTheSameTime(t *testing.T) {
	handler, _ := controlledHandler(t)
	// Each probe waits until the other has started. Run one after the other, the first
	// would wait forever, so the deadline below is what turns that into a failure.
	var started sync.WaitGroup
	started.Add(3)
	meet := func(context.Context) (string, error) {
		started.Done()
		started.Wait()
		return "", nil
	}
	handler.WithServiceProbes(map[string]ServiceProbe{"book": meet, "knowledge": meet, "memory": meet})

	done := make(chan struct{})
	go func() {
		statusServices(t, handler)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("probes ran one after the other: a slow service would hold up the whole status")
	}
}

func TestFeedsProbeSaysWhenNothingIsFollowedOrSomethingFails(t *testing.T) {
	cases := map[string]struct {
		status map[string]any
		detail string
	}{
		"nothing enabled":          {map[string]any{"enabled": float64(0), "failed": float64(0)}, "no plugins enabled"},
		"no counts at all":         {map[string]any{}, "no plugins enabled"},
		"all plugins fine":         {map[string]any{"enabled": float64(2), "failed": float64(0)}, ""},
		"some plugins failing":     {map[string]any{"enabled": float64(3), "failed": float64(2)}, "2 of 3 plugins failing"},
		"every plugin failing":     {map[string]any{"enabled": float64(1), "failed": float64(1)}, "1 of 1 plugins failing"},
		"counts of the wrong type": {map[string]any{"enabled": "3", "failed": "1"}, "no plugins enabled"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			detail, err := FeedsProbe(fakeStatus{result: c.status})(context.Background())
			if err != nil || detail != c.detail {
				t.Fatalf("detail=%q err=%v, want %q", detail, err, c.detail)
			}
		})
	}

	if _, err := FeedsProbe(fakeStatus{err: errors.New("connection refused")})(context.Background()); err == nil {
		t.Fatal("a feeds service that does not answer must fail its probe")
	}
}

func TestStatusShowsTheFeedsServiceWithItsNote(t *testing.T) {
	handler, _ := controlledHandler(t)
	handler.WithServiceProbes(map[string]ServiceProbe{
		"feeds": FeedsProbe(fakeStatus{result: map[string]any{"enabled": float64(0)}}),
	})

	_, services := statusServices(t, handler)

	feeds := services["feeds"].(map[string]any)
	if feeds["status"] != "ok" || feeds["enabled"] != true || feeds["detail"] != "no plugins enabled" {
		t.Fatalf("unexpected feeds entry %#v", feeds)
	}
}
