package handlers

import (
	"context"
	"read_books/internal/core"
	"strings"
	"testing"
)

type fakeFeedsService struct {
	types    []string
	payloads []map[string]any
	reply    map[string]any
}

func (f *fakeFeedsService) Dispatch(_ context.Context, eventType string, payload map[string]any) (map[string]any, error) {
	f.types = append(f.types, eventType)
	f.payloads = append(f.payloads, payload)
	return f.reply, nil
}

func feedsWithItems() *fakeFeedsService {
	return &fakeFeedsService{reply: map[string]any{
		"items": []any{
			map[string]any{"id": "1", "plugin": "p", "title": "Older entry", "published": "2999-01-01T00:00:00Z", "source": "A"},
			map[string]any{"id": "2", "plugin": "p", "title": "Undated entry", "source": "A"},
		},
		"updated": "2026-10-02T11:00:00Z",
	}}
}

func TestDigestEventsFlowThroughTheOrchestrator(t *testing.T) {
	service := feedsWithItems()
	orchestrator := core.DefaultService(NewDigestDomainHandler(service))

	result, err := orchestrator.ProcessWithResult(context.Background(), core.Event{
		Source:  core.SourceCLI,
		Type:    "digest.items",
		Payload: map[string]any{"limit": float64(1)},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Join(service.types, ",") != "feeds.items" || service.payloads[0] != nil {
		t.Fatalf("the service was asked %v with %v", service.types, service.payloads)
	}
	if result.Workflow != "digest" {
		t.Fatalf("unexpected workflow %#v", result)
	}
	page, _ := result.Result["digest"].(map[string]any)
	items, _ := page["items"].([]map[string]any)
	if page["total"] != 2 || page["more"] != 1 || len(items) != 1 || items[0]["title"] != "Older entry" {
		t.Fatalf("unexpected page %#v", page)
	}
}

func TestDigestStatusIsTheServicesOwnStatus(t *testing.T) {
	service := &fakeFeedsService{reply: map[string]any{"enabled": 1, "plugins": []any{}}}
	orchestrator := core.DefaultService(NewDigestDomainHandler(service))

	result, err := orchestrator.ProcessWithResult(context.Background(), core.Event{Source: core.SourceCLI, Type: "digest.status"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	status, _ := result.Result["digest"].(map[string]any)
	if strings.Join(service.types, ",") != "feeds.status" || status["enabled"] != 1 {
		t.Fatalf("asked %v, got %#v", service.types, result.Result)
	}
}

func TestADigestRequestThatIsRefusedNeverReachesTheService(t *testing.T) {
	service := feedsWithItems()
	orchestrator := core.DefaultService(NewDigestDomainHandler(service))

	for name, event := range map[string]core.Event{
		"a text limit":       {Type: "digest.items", Payload: map[string]any{"limit": "ten"}},
		"an unknown action":  {Type: "digest.refresh"},
		"an unlisted action": {Type: "digest.purge"},
	} {
		event.Source = core.SourceCLI
		if _, err := orchestrator.ProcessWithResult(context.Background(), event); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
	if len(service.types) != 0 {
		t.Fatalf("the service was asked %v", service.types)
	}
}

func TestSwitchingFeedsOffSkipsTheDigest(t *testing.T) {
	service := feedsWithItems()
	controls, err := core.NewControls("")
	if err != nil {
		t.Fatalf("new controls: %v", err)
	}
	if err := controls.SetService("feeds", false); err != nil {
		t.Fatalf("set service: %v", err)
	}
	orchestrator := core.DefaultService(NewDigestDomainHandler(service)).WithControls(controls)

	for _, eventType := range []string{"digest.items", "digest.status"} {
		result, err := orchestrator.ProcessWithResult(context.Background(), core.Event{Source: core.SourceCLI, Type: eventType})
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", eventType, err)
		}
		if result.Result["skipped"] != true {
			t.Fatalf("%s: expected a skipped result: %#v", eventType, result.Result)
		}
	}
	if len(service.types) != 0 {
		t.Fatalf("a switched-off service was asked %v", service.types)
	}
}
