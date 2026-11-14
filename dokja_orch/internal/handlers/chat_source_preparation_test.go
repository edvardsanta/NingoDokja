package handlers

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

type focusedSourcePreparer struct {
	calls          int
	sourceID, goal string
	version        string
	err            error
}

func (p *focusedSourcePreparer) PrepareFocusedSource(_ context.Context, sourceID, goal string) (PreparedKnowledgeSource, error) {
	p.calls++
	p.sourceID, p.goal = sourceID, goal
	if p.err != nil {
		return PreparedKnowledgeSource{}, p.err
	}
	return PreparedKnowledgeSource{Source: map[string]any{
		"source_id": sourceID, "title": "The book", "text": bookPassage, "text_version": p.version,
	}}, nil
}

func versionedBookHit(version string) map[string]any {
	hit := bookHit(true)
	hit["locator"] = map[string]any{"version": version, "start": 3}
	return hit
}

func TestFocusedSourcePreparationPinsAndReusesValidatedVersions(t *testing.T) {
	first := "sha256:" + strings.Repeat("a", 64)
	revised := "sha256:" + strings.Repeat("b", 64)
	base := &documentBase{hits: []any{bookHit(true)}}
	preparer := &focusedSourcePreparer{version: first}
	handler := focusHandler(base)
	handler.WithApplicationSourcePreparer(preparer)
	citedBook(handler, "what about the book?")

	turn := focusedTurn(handler, "visões")
	sources := handler.lookupForTurn(context.Background(), "visões", "", turn)
	if preparer.calls != 1 || preparer.sourceID != "file:book" || preparer.goal != "visões" {
		t.Fatalf("preparation was not asked once for the focus: %+v", preparer)
	}
	if len(sources) != 1 || sources[0].TextVersion != first {
		t.Fatalf("sources were not pinned: %+v", sources)
	}
	if kept := turnSources(sources); len(kept) != 1 || kept[0].TextVersion != first {
		t.Fatalf("the turn did not keep the source pin: %+v", kept)
	}

	handler.focus.remember(turn, "visões", sources)
	base.hits = []any{versionedBookHit(first)}
	sources = handler.lookupForTurn(context.Background(), "visões", "", focusedTurn(handler, "visões"))
	if preparer.calls != 1 || sources[0].TextVersion != first {
		t.Fatalf("an unchanged version re-read the source: calls=%d sources=%+v", preparer.calls, sources)
	}

	base.hits = []any{versionedBookHit(revised)}
	preparer.version = revised
	sources = handler.lookupForTurn(context.Background(), "visões", "", focusedTurn(handler, "visões"))
	if preparer.calls != 2 || sources[0].TextVersion != revised {
		t.Fatalf("a changed version did not revalidate the source: calls=%d sources=%+v", preparer.calls, sources)
	}
}

func TestFocusedSourcePreparationFailureLeavesTheTurnUsable(t *testing.T) {
	base := &documentBase{hits: []any{bookHit(true)}}
	preparer := &focusedSourcePreparer{err: fmt.Errorf("knowledge unavailable")}
	handler := focusHandler(base)
	handler.WithApplicationSourcePreparer(preparer)
	citedBook(handler, "the book")

	sources := handler.lookupForTurn(context.Background(), "visões", "", focusedTurn(handler, "visões"))
	if preparer.calls != 1 || len(sources) != 1 || sources[0].TextVersion != "" {
		t.Fatalf("failed preparation changed the ordinary search: calls=%d sources=%+v", preparer.calls, sources)
	}
}

func TestOwnerSuppliedDiscordAndWhatsAppLinksKeepPinnedVersions(t *testing.T) {
	reader := &webReadingService{}
	model := &fakeChatGenerator{result: map[string]any{"reply": "The estimate requires assumptions. [1]"}}
	h := NewChatDomainHandler(model).WithWebReading(reader, nil)
	discord, err := h.Handle(context.Background(), chatEvent("Explain https://example.com/news"), chatStep)
	if err != nil {
		t.Fatal(err)
	}
	discordSources, _ := discord["sources"].([]map[string]any)
	if len(discordSources) == 0 {
		t.Fatalf("Discord link left no cited source: %v", discord)
	}
	version, _ := discordSources[0]["text_version"].(string)
	if !strings.HasPrefix(version, "sha256:") {
		t.Fatalf("Discord link was not version-pinned: %v", discordSources[0])
	}

	archive := archiveFor(t)
	selfChatModel := &fakeChatGenerator{result: map[string]any{"reply": "The estimate requires assumptions. [1]"}}
	selfChat := NewChatDomainHandler(selfChatModel).WithArchive(archive).WithWebReading(reader, nil)
	if _, err := selfChat.Handle(context.Background(), whatsappTurn("Explain https://example.com/news", "m1", true), chatStep); err != nil {
		t.Fatal(err)
	}
	held := listed(t, archive)[0]
	turns := turnsOf(t, archive, held.ID)
	if len(turns) != 1 || len(turns[0].Sources) == 0 || !strings.HasPrefix(turns[0].Sources[0].TextVersion, "sha256:") {
		t.Fatalf("WhatsApp self-chat link was not archive-pinned: %+v", turns)
	}
}
