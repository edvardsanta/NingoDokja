package handlers

import (
	"context"
	"errors"
	"testing"

	"read_books/internal/core"
)

type saveService struct {
	fakeKnowledgeService
	err error
}

func (s *saveService) Dispatch(_ context.Context, eventType string, payload map[string]any) (map[string]any, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.eventType, s.payload = eventType, payload
	return map[string]any{"created": true, "changed": true}, nil
}

func saveEvent(owner bool, payload map[string]any) core.Event {
	return core.Event{
		Source: core.SourceDiscord, Type: "knowledge.save", Payload: payload,
		Context: map[string]any{"is_owner": owner},
	}
}

func TestOwnerSaveIngestsTheLinkWithItsDiscordIdentity(t *testing.T) {
	service := &saveService{}
	orchestrator := core.DefaultService(NewKnowledgeDomainHandler(service))

	result, err := orchestrator.ProcessWithResult(context.Background(), saveEvent(true, map[string]any{
		"link": "https://example.com/dossie", "guildId": "guild-1", "channelId": "chan-1", "messageId": "msg-9",
	}))
	if err != nil {
		t.Fatal(err)
	}
	knowledge, _ := result.Result["knowledge"].(map[string]any)
	if knowledge["status"] != "ok" || knowledge["created"] != true {
		t.Fatalf("unexpected result %#v", knowledge)
	}
	if service.eventType != "knowledge.ingest" {
		t.Fatalf("service got %q", service.eventType)
	}
	if service.payload["source"] != "https://example.com/dossie" || service.payload["kind"] != "note" {
		t.Fatalf("payload lost its origin: %#v", service.payload)
	}
	if service.payload["source_id"] != "discord-message:guild-1/chan-1/msg-9" ||
		service.payload["source_ref"] != "https://discord.com/channels/guild-1/chan-1/msg-9" {
		t.Fatalf("the document must carry the Discord message identity: %#v", service.payload)
	}
	if _, exists := service.payload["metadata"]; exists {
		t.Fatal("save must not assert metadata: the service owns that schema")
	}
	tags, _ := service.payload["tags"].([]string)
	if len(tags) != 1 || tags[0] != "via:save" {
		t.Fatalf("the saved document lost its via tag: %v", service.payload)
	}
}

func TestSaveRefusesEveryoneButTheOwner(t *testing.T) {
	service := &saveService{}
	orchestrator := core.DefaultService(NewKnowledgeDomainHandler(service))

	result, err := orchestrator.ProcessWithResult(context.Background(), saveEvent(false, map[string]any{
		"link": "https://example.com/dossie",
	}))
	if err != nil {
		t.Fatal(err)
	}
	knowledge, _ := result.Result["knowledge"].(map[string]any)
	if knowledge["status"] != "refused" || knowledge["refused"] != saveRefusedNotOwner {
		t.Fatalf("unexpected result %#v", knowledge)
	}
	if service.payload != nil {
		t.Fatal("a refused save reached the service")
	}
}

func TestSaveRefusesAnInvalidLink(t *testing.T) {
	service := &saveService{}
	orchestrator := core.DefaultService(NewKnowledgeDomainHandler(service))

	for _, link := range []string{"", "ftp://example.com/x", "https://user@example.com/x", "not a link"} {
		result, err := orchestrator.ProcessWithResult(context.Background(), saveEvent(true, map[string]any{"link": link}))
		if err != nil {
			t.Fatal(err)
		}
		knowledge, _ := result.Result["knowledge"].(map[string]any)
		if knowledge["status"] != "refused" || knowledge["refused"] != saveRefusedInvalidLink {
			t.Fatalf("%q: unexpected result %#v", link, knowledge)
		}
	}
	if service.payload != nil {
		t.Fatal("an invalid link reached the service")
	}
}

func TestSaveWithoutMessageIdentityFallsBackToTheLink(t *testing.T) {
	service := &saveService{}
	orchestrator := core.DefaultService(NewKnowledgeDomainHandler(service))

	_, err := orchestrator.ProcessWithResult(context.Background(), saveEvent(true, map[string]any{
		"link": "https://example.com/dossie",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if service.payload["source_id"] != "discord-save:https://example.com/dossie" ||
		service.payload["source_ref"] != "https://example.com/dossie" {
		t.Fatalf("fallback identity must be the link itself: %#v", service.payload)
	}
}

func TestSaveReportsAnUnavailableLibrary(t *testing.T) {
	service := &saveService{err: errors.New("private failure")}
	orchestrator := core.DefaultService(NewKnowledgeDomainHandler(service))

	result, err := orchestrator.ProcessWithResult(context.Background(), saveEvent(true, map[string]any{
		"link": "https://example.com/dossie",
	}))
	if err != nil {
		t.Fatal(err)
	}
	knowledge, _ := result.Result["knowledge"].(map[string]any)
	if knowledge["status"] != "refused" || knowledge["refused"] != saveRefusedUnavailable {
		t.Fatalf("unexpected result %#v", knowledge)
	}
}
