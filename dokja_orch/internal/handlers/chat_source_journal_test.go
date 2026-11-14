package handlers

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	knowledge "read_books/dokja_domain/dokja_knowledge"
)

// A focused turn with a version candidate records one pinned source-read episode in the
// journal, and the pinned sources carry the validated version.
func TestFocusedSourcePreparationJournalsTheObservedVersionPin(t *testing.T) {
	candidate := sourceReadDigestText(bookPassage)
	base := &documentBase{hits: []any{versionedBookHit(candidate)}}
	handler := focusHandler(base)
	store := newSourceJournalStore()
	handler.WithSourceReadJournal(store)
	preparer := &applicationStore{documents: map[string]string{"file:book": bookPassage}}
	handler.WithApplicationSourcePreparer(NewKnowledgeDomainHandler(preparer))
	citedBook(handler, "what about the book?")

	sources := handler.lookupForTurn(context.Background(), "visões", "", focusedTurn(handler, "visões"))
	if len(sources) != 1 || sources[0].TextVersion != candidate {
		t.Fatalf("sources were not pinned with the journaled version: %+v", sources)
	}
	if len(preparer.calls) != 1 || preparer.calls[0] != "file:book" {
		t.Fatalf("the source was not read once through the application port: %v", preparer.calls)
	}
	if len(store.episodes) != 1 {
		t.Fatalf("the journaled preparation recorded no episode: %d calls", store.calls)
	}
	for _, episode := range store.episodes {
		if episode.Revision != 3 || episode.Attempt.Phase != "settled" {
			t.Fatalf("the episode was not settled: %+v", episode)
		}
		if len(episode.Observations) != 1 || episode.Observations[0].Outcome != "succeeded" {
			t.Fatalf("the episode has no succeeded receipt: %+v", episode.Observations)
		}
		raw, _ := json.Marshal(episode)
		if strings.Contains(string(raw), bookPassage) || strings.Contains(string(raw), "visões") {
			t.Fatal("journal persisted source text or goal")
		}
	}
}

// Reusing an unchanged pin performs no preparation, so the journal records nothing new.
func TestFocusedSourcePreparationReusesPinWithoutJournalingAgain(t *testing.T) {
	candidate := sourceReadDigestText(bookPassage)
	base := &documentBase{hits: []any{versionedBookHit(candidate)}}
	handler := focusHandler(base)
	store := newSourceJournalStore()
	handler.WithSourceReadJournal(store)
	preparer := &applicationStore{documents: map[string]string{"file:book": bookPassage}}
	handler.WithApplicationSourcePreparer(NewKnowledgeDomainHandler(preparer))
	citedBook(handler, "what about the book?")

	turn := focusedTurn(handler, "visões")
	sources := handler.lookupForTurn(context.Background(), "visões", "", turn)
	handler.focus.remember(turn, "visões", sources)
	journaled := store.calls

	sources = handler.lookupForTurn(context.Background(), "visões", "", focusedTurn(handler, "visões"))
	if len(preparer.calls) != 1 || store.calls != journaled || sources[0].TextVersion != candidate {
		t.Fatalf("an unchanged pin re-read or re-journaled the source: calls=%v store=%d/%d sources=%+v", preparer.calls, journaled, store.calls, sources)
	}
}

// Without any version candidate the preparation runs best effort and the journal stays out
// of the context entirely.
func TestFocusedSourcePreparationWithoutCandidateSkipsTheJournal(t *testing.T) {
	base := &documentBase{hits: []any{bookHit(true)}}
	handler := focusHandler(base)
	store := newSourceJournalStore()
	handler.WithSourceReadJournal(store)
	preparer := &focusedSourcePreparer{version: sourceReadDigestText(bookPassage)}
	handler.WithApplicationSourcePreparer(preparer)
	citedBook(handler, "what about the book?")

	sources := handler.lookupForTurn(context.Background(), "visões", "", focusedTurn(handler, "visões"))
	if preparer.calls != 1 || preparer.pin != "" {
		t.Fatalf("the pinless preparation carried a journal pin: %+v", preparer)
	}
	if store.calls != 0 {
		t.Fatalf("the journal recorded a pinless preparation: %d calls", store.calls)
	}
	if len(sources) != 1 || sources[0].TextVersion != preparer.version {
		t.Fatalf("the validated version was not kept: %+v", sources)
	}
}

// A failing journal store must not block the turn: preparation fails best effort and the
// sources simply stay unpinned.
func TestFocusedSourcePreparationSurvivesJournalFailure(t *testing.T) {
	claimed := "sha256:" + strings.Repeat("d", 64)
	base := &documentBase{hits: []any{versionedBookHit(claimed)}}
	handler := focusHandler(base)
	store := newSourceJournalStore()
	store.failAt = 1
	handler.WithSourceReadJournal(store)
	preparer := &applicationStore{documents: map[string]string{"file:book": bookPassage}}
	handler.WithApplicationSourcePreparer(NewKnowledgeDomainHandler(preparer))
	citedBook(handler, "what about the book?")

	sources := handler.lookupForTurn(context.Background(), "visões", "", focusedTurn(handler, "visões"))
	if len(sources) != 1 || sources[0].TextVersion != claimed {
		t.Fatalf("journal failure replaced the observed version: %+v", sources)
	}
	if store.calls != 1 || len(preparer.calls) != 0 {
		t.Fatalf("the journaled read was not refused before IO: store=%d calls=%v", store.calls, preparer.calls)
	}
}

// The Talk authorization grants exactly one pinned read of the focused document: no other
// capability, source or unpinned read passes, and the decision reference is opaque.
func TestTalkFocusedSourceAuthorize(t *testing.T) {
	authorize := talkFocusedSourceAuthorize("file:book")
	valid := knowledge.ApplicationSelection{SourceID: "file:book", TextVersion: sourceReadDigestText(bookPassage)}
	for name, permission := range map[string]SourceReadPermission{
		"wrong capability": {Capability: "knowledge.other", Source: valid},
		"wrong source":     {Capability: sourceReadCapability, Source: knowledge.ApplicationSelection{SourceID: "file:other", TextVersion: valid.TextVersion}},
		"unpinned read":    {Capability: sourceReadCapability, Source: knowledge.ApplicationSelection{SourceID: "file:book"}},
	} {
		if decision, err := authorize(context.Background(), permission); err == nil || decision != "" {
			t.Fatalf("%s was authorized: %q %v", name, decision, err)
		}
	}
	decision, err := authorize(context.Background(), SourceReadPermission{Capability: sourceReadCapability, Source: valid})
	if err != nil || decision != "decision:talk-focused-source" {
		t.Fatalf("the pinned read was not authorized: %q %v", decision, err)
	}
}
