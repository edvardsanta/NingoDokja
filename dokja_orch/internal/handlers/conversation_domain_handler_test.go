package handlers

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	conversation "read_books/dokja_domain/dokja_conversation"
	"read_books/internal/core"
	"read_books/internal/infrastructure/conversations"
)

func ask(t *testing.T, h *ConversationDomainHandler, source core.EventSource, kind string, payload map[string]any) (map[string]any, error) {
	t.Helper()
	return h.Handle(context.Background(), core.Event{Source: source, Type: kind, Payload: payload}, core.WorkflowStep{Domain: core.DomainConversation})
}

func TestTheOwnerStartsListsReadsRenamesAndDeletesConversations(t *testing.T) {
	h := NewConversationDomainHandler(archiveFor(t))
	started, err := ask(t, h, core.SourceDesktop, "conversation.start", map[string]any{"title": "On reading"})
	if err != nil {
		t.Fatal(err)
	}
	c := started["conversation"].(map[string]any)
	id := c["id"].(string)
	if c["kind"] != "general" || c["title"] != "On reading" || c["revision"] != uint64(1) {
		t.Fatal(c)
	}
	reading, _ := ask(t, h, core.SourceCLI, "conversation.start", map[string]any{"reading_id": "item-1"})
	if reading["conversation"].(map[string]any)["kind"] != "reading" {
		t.Fatal(reading)
	}

	// The CLI and the TUI (which speaks as the CLI) see what the desktop started.
	listed, err := ask(t, h, core.SourceCLI, "conversation.list", map[string]any{"limit": float64(10)})
	if err != nil || len(listed["conversations"].([]map[string]any)) != 2 || listed["next"] != "" {
		t.Fatal(listed, err)
	}
	read, err := ask(t, h, core.SourceDesktop, "conversation.read", map[string]any{"conversation_id": id})
	if err != nil || read["conversation"].(map[string]any)["id"] != id || len(read["turns"].([]map[string]any)) != 0 {
		t.Fatal(read, err)
	}

	if _, err := ask(t, h, core.SourceDesktop, "conversation.rename", map[string]any{"conversation_id": id, "revision": float64(7), "title": "x"}); !errors.Is(err, conversation.ErrConflict) {
		t.Fatal("a stale revision is a conflict", err)
	}
	renamed, err := ask(t, h, core.SourceDesktop, "conversation.rename", map[string]any{"conversation_id": id, "revision": float64(1), "title": "Plato"})
	if err != nil || renamed["conversation"].(map[string]any)["title"] != "Plato" {
		t.Fatal(renamed, err)
	}
	if _, err := ask(t, h, core.SourceDesktop, "conversation.delete", map[string]any{"conversation_id": id, "revision": float64(2)}); err != nil {
		t.Fatal(err)
	}
	if _, err := ask(t, h, core.SourceDesktop, "conversation.read", map[string]any{"conversation_id": id}); !errors.Is(err, conversation.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestOnlyTheOwnersInterfacesReachTheArchive(t *testing.T) {
	h := NewConversationDomainHandler(archiveFor(t))
	for _, source := range []core.EventSource{core.SourceDiscord, core.SourceWhatsApp, core.SourceAPI, ""} {
		if _, err := ask(t, h, source, "conversation.list", nil); err == nil {
			t.Fatalf("%q must not reach the archive", source)
		}
	}
	if _, err := ask(t, NewConversationDomainHandler(nil), core.SourceDesktop, "conversation.list", nil); err == nil {
		t.Fatal("without an archive the handler says so")
	}
	if _, err := ask(t, h, core.SourceDesktop, "conversation.purge", nil); err == nil {
		t.Fatal("an unknown action is refused")
	}
}

func TestOddNumbersReadAsTheDefault(t *testing.T) {
	for _, value := range []any{"10", 1.5, -3.0, 2e12, nil} {
		if payloadInt(map[string]any{"limit": value}, "limit") != 0 {
			t.Fatal(value)
		}
	}
	if payloadRevision(map[string]any{"revision": 0.5}) != 0 || payloadRevision(map[string]any{"revision": float64(3)}) != 3 {
		t.Fatal("revision")
	}
}

func TestConversationRequestsAreRoutedToTheArchiveByTheService(t *testing.T) {
	service := core.DefaultService(NewConversationDomainHandler(archiveFor(t)))
	result, err := service.ProcessWithResult(context.Background(), core.Event{EventID: "e-1", Source: core.SourceDesktop, Type: "conversation.start", Payload: map[string]any{"title": "Routed"}})
	if err != nil || result.Workflow != "conversation-archive" {
		t.Fatal(result, err)
	}
	listed, err := service.ProcessWithResult(context.Background(), core.Event{EventID: "e-2", Source: core.SourceDesktop, Type: "conversation.list"})
	if err != nil {
		t.Fatal(err)
	}
	archived, _ := listed.Result["conversation"].(map[string]any)
	items, _ := archived["conversations"].([]map[string]any)
	if len(items) != 1 || items[0]["title"] != "Routed" {
		t.Fatal(listed)
	}
}

// onlyStore hides the optional capabilities of a store, as a custom store without them would.
type onlyStore struct{ conversation.Store }

func capabilitiesOf(t *testing.T, handler *ConversationDomainHandler, source core.EventSource) (map[string]any, error) {
	t.Helper()
	return handler.Handle(context.Background(), core.Event{Source: source, Type: "conversation.capabilities", Payload: map[string]any{}}, core.WorkflowStep{})
}

func TestTheCapabilityReportFollowsTheOrchestratorsWiring(t *testing.T) {
	archive := archiveFor(t)
	wired, err := capabilitiesOf(t, NewConversationDomainHandler(archive).WithRunningSends(core.NewRunningSends()), core.SourceCLI)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"send": "supported", "retry": "supported", "stop": "supported", "steer": "unsupported"}
	if wired["view"] != 1 || !reflect.DeepEqual(wired["operations"], want) {
		t.Fatal(wired)
	}

	unwired, err := capabilitiesOf(t, NewConversationDomainHandler(archive), core.SourceDesktop)
	if err != nil {
		t.Fatal(err)
	}
	if operations := unwired["operations"].(map[string]string); operations["stop"] != "unsupported" || operations["steer"] != "unsupported" {
		t.Fatal("without running sends nothing can be stopped", unwired)
	}
}

func TestTheCapabilityReportSaysRetryIsUnsupportedOnAStoreWithoutIt(t *testing.T) {
	store, err := conversations.Open(filepath.Join(t.TempDir(), conversations.FileName))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	report, err := capabilitiesOf(t, NewConversationDomainHandler(conversation.New(onlyStore{store})), core.SourceDesktop)
	if err != nil {
		t.Fatal(err)
	}
	if report["operations"].(map[string]string)["retry"] != "unsupported" {
		t.Fatal(report)
	}
}

func TestOnlyTheOwnersInterfacesAreToldTheCapabilities(t *testing.T) {
	handler := NewConversationDomainHandler(archiveFor(t)).WithRunningSends(core.NewRunningSends())
	for _, source := range []core.EventSource{core.SourceDiscord, core.SourceWhatsApp} {
		if report, err := capabilitiesOf(t, handler, source); err == nil {
			t.Fatalf("%s was told %v", source, report)
		}
	}
}
