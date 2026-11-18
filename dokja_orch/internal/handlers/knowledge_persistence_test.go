package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	chat "read_books/dokja_domain/dokja_chat"
	knowledgedomain "read_books/dokja_domain/dokja_knowledge"
	"read_books/internal/core"
)

func TestOwnerWebLinkIsPersistedToKnowledge(t *testing.T) {
	knowledge := &ingestRecorder{changed: true}
	h := NewChatDomainHandler(&fakeChatGenerator{result: map[string]any{"reply": "The estimate requires assumptions. [1]"}}).
		WithWebReading(&webReadingService{}, nil).
		WithKnowledgePersistence(knowledge, nil)
	event := chatEvent("Explain https://example.com/news")
	event.Context = map[string]any{"is_owner": true}
	if _, err := h.Handle(context.Background(), event, chatStep); err != nil {
		t.Fatal(err)
	}
	if len(knowledge.payloads) != 1 {
		t.Fatalf("owner link was not persisted: %v", knowledge.others)
	}
	payload := knowledge.payloads[0]
	sum := sha256.Sum256([]byte("https://example.com/news"))
	if payload["source_id"] != "web:"+hex.EncodeToString(sum[:12]) {
		t.Fatalf("durable source lost the citation identity: %v", payload["source_id"])
	}
	if payload["kind"] != "article" || payload["source_ref"] != "https://example.com/news" {
		t.Fatalf("durable source lost its reference: %v", payload)
	}
	if title, _ := payload["title"].(string); !strings.HasPrefix(title, "Law") {
		t.Fatalf("durable source lost its title: %v", payload["title"])
	}
	body, _ := payload["body"].(string)
	if !strings.Contains(body, "Art. 140") || !strings.Contains(body, "calculation assumptions") {
		t.Fatalf("durable source lost its passages: %q", body)
	}
	tags, _ := payload["tags"].([]string)
	if len(tags) != 2 || tags[0] != "origin:discord" || tags[1] != "via:chat-link" {
		t.Fatalf("unexpected provenance tags: %v", payload["tags"])
	}
	metadata := payload["metadata"].(map[string]any)
	provenance := metadata["provenance"].(knowledgedomain.Provenance)
	if provenance.Origin != knowledgedomain.OriginOwner || provenance.Channel != "c1" {
		t.Fatalf("the owner's reading lost its provenance: %+v", provenance)
	}
}

func TestStrangerWebLinkIsNotPersistedToKnowledge(t *testing.T) {
	for name, event := range map[string]core.Event{
		"discord stranger":   chatEvent("Explain https://example.com/news"),
		"whatsapp group":     whatsappTurn("Explain https://example.com/news", "m1", false),
		"whatsapp self-chat": whatsappTurn("Explain https://example.com/news", "m1", true),
	} {
		knowledge := &ingestRecorder{}
		h := NewChatDomainHandler(&fakeChatGenerator{result: map[string]any{"reply": "reading [1]"}}).
			WithWebReading(&webReadingService{}, nil).
			WithKnowledgePersistence(knowledge, nil)
		if _, err := h.Handle(context.Background(), event, chatStep); err != nil {
			t.Fatal(err)
		}
		want := 0
		if name == "whatsapp self-chat" {
			want = 1
		}
		if len(knowledge.payloads) != want {
			t.Fatalf("%s persisted %d readings, want %d", name, len(knowledge.payloads), want)
		}
		if want == 1 {
			tags, _ := knowledge.payloads[0]["tags"].([]string)
			if len(tags) != 2 || tags[0] != "origin:whatsapp" || tags[1] != "via:chat-link" {
				t.Fatalf("self-chat provenance lost: %v", knowledge.payloads[0]["tags"])
			}
		}
	}
}

func TestWebLinkPersistenceHonorsTheKnowledgeSwitch(t *testing.T) {
	knowledge := &ingestRecorder{}
	h := NewChatDomainHandler(&fakeChatGenerator{result: map[string]any{"reply": "reading [1]"}}).
		WithWebReading(&webReadingService{}, nil).
		WithKnowledgePersistence(knowledge, func() bool { return false })
	event := chatEvent("Explain https://example.com/news")
	event.Context = map[string]any{"is_owner": true}
	if _, err := h.Handle(context.Background(), event, chatStep); err != nil {
		t.Fatal(err)
	}
	if len(knowledge.payloads) != 0 {
		t.Fatalf("a disabled knowledge base still stored a reading: %v", knowledge.payloads)
	}
}

func TestOwnerIncomingDocumentIsPersistedToKnowledge(t *testing.T) {
	knowledge := &ingestRecorder{changed: true}
	reader := &incomingTestReader{}
	h := NewChatDomainHandler(&incomingTestGenerator{}).
		WithIncomingContent(NewIncomingContentWorkflow(reader, nil, func() bool { return true })).
		WithKnowledgePersistence(knowledge, nil)
	event := core.Event{Source: core.SourceWhatsApp, Context: map[string]any{"is_self_chat": true},
		Payload: map[string]any{"content": "focus on results", "locale": "en",
			"document": &chat.IncomingDocument{Filename: "memo.pdf", DataB64: base64.StdEncoding.EncodeToString([]byte("file"))}}}
	reply, err := h.Handle(t.Context(), event, core.WorkflowStep{Action: chat.ActionReadIncomingContent})
	if err != nil || reply["reply"] != "summary" {
		t.Fatalf("document turn failed: %v %+v", err, reply)
	}
	if len(knowledge.payloads) != 1 {
		t.Fatalf("owner document was not persisted: %v", knowledge.others)
	}
	payload := knowledge.payloads[0]
	if payload["source_id"] != "file:memo.pdf" {
		t.Fatalf("durable document lost its file identity: %v", payload["source_id"])
	}
	body, _ := payload["body"].(string)
	if !strings.Contains(body, "stable improvement") {
		t.Fatalf("durable document lost its text: %q", body)
	}
}

func TestStrangerIncomingDocumentIsNotPersistedToKnowledge(t *testing.T) {
	knowledge := &ingestRecorder{}
	reader := &incomingTestReader{}
	h := NewChatDomainHandler(&incomingTestGenerator{}).
		WithIncomingContent(NewIncomingContentWorkflow(reader, nil, func() bool { return true })).
		WithKnowledgePersistence(knowledge, nil)
	event := core.Event{Source: core.SourceWhatsApp,
		Payload: map[string]any{"content": "focus on results", "locale": "en",
			"document": &chat.IncomingDocument{Filename: "memo.pdf", DataB64: base64.StdEncoding.EncodeToString([]byte("file"))}}}
	reply, err := h.Handle(t.Context(), event, core.WorkflowStep{Action: chat.ActionReadIncomingContent})
	if err != nil || reply["reply"] != "summary" {
		t.Fatalf("document turn failed: %v %+v", err, reply)
	}
	if len(knowledge.payloads) != 0 {
		t.Fatalf("someone else's document became the owner's knowledge: %v", fmt.Sprint(knowledge.payloads))
	}
}

type recordedProposal struct {
	kind    string
	payload map[string]any
	source  knowledgedomain.ProposalSource
	by      string
	sentBy  string
}

type recordingProposer struct {
	calls []recordedProposal
}

func (r *recordingProposer) Propose(_ context.Context, kind string, payload map[string]any, source knowledgedomain.ProposalSource, proposedBy, sentBy string) (string, bool, error) {
	r.calls = append(r.calls, recordedProposal{kind: kind, payload: payload, source: source, by: proposedBy, sentBy: sentBy})
	return "proposal:test", false, nil
}

// A known person's link is held pending, never stored: the reply the turn had is
// unchanged and nothing reaches retrieval before the owner decides.
func TestKnownPersonWebLinkIsHeldAsAProposal(t *testing.T) {
	knowledge := &ingestRecorder{}
	proposer := &recordingProposer{}
	h := NewChatDomainHandler(&fakeChatGenerator{result: map[string]any{"reply": "The estimate requires assumptions. [1]"}}).
		WithWebReading(&webReadingService{}, nil).
		WithKnowledgePersistence(knowledge, nil).
		WithKnowledgeProposals(proposer).
		WithNingoFacts(chat.NingoFacts{People: []chat.KnownPerson{{Name: "Nick", RealName: "Sam", Discord: "sam-id"}}})
	event := chatEvent("Explain https://example.com/news")
	event.User.ID = "sam-id"
	event.Context = map[string]any{"is_owner": false, "guild_id": "guild", "message_id": "m9"}
	result, err := h.Handle(context.Background(), event, chatStep)
	if err != nil || result["reply"] == "" {
		t.Fatalf("the turn was changed: %v %+v", err, result)
	}
	if len(knowledge.payloads) != 0 {
		t.Fatalf("a pending proposal reached the store: %v", knowledge.payloads)
	}
	if len(proposer.calls) != 1 {
		t.Fatalf("the known person's link was not held: %+v", proposer.calls)
	}
	call := proposer.calls[0]
	if call.kind != "web_article" || call.payload["url"] != "https://example.com/news" {
		t.Fatalf("the proposal lost the link: %+v", call)
	}
	if call.by != "Sam <sam-id>" || call.source.ChannelID != "c1" || call.source.MessageID != "m9" ||
		!strings.Contains(call.source.Permalink, "/guild/c1/m9") {
		t.Fatalf("the proposal lost its trace: %+v", call)
	}
}

func TestStrangerWebLinkIsNeitherStoredNorProposed(t *testing.T) {
	knowledge := &ingestRecorder{}
	proposer := &recordingProposer{}
	h := NewChatDomainHandler(&fakeChatGenerator{result: map[string]any{"reply": "reading [1]"}}).
		WithWebReading(&webReadingService{}, nil).
		WithKnowledgePersistence(knowledge, nil).
		WithKnowledgeProposals(proposer).
		WithNingoFacts(chat.NingoFacts{People: []chat.KnownPerson{{Name: "Nick", Discord: "sam-id"}}})
	event := chatEvent("Explain https://example.com/news")
	event.User.ID = "someone-else"
	if _, err := h.Handle(context.Background(), event, chatStep); err != nil {
		t.Fatal(err)
	}
	if len(proposer.calls) != 0 || len(knowledge.payloads) != 0 {
		t.Fatalf("a stranger's link was kept: proposals=%d stored=%d", len(proposer.calls), len(knowledge.payloads))
	}
}

func TestKnownPersonDocumentIsHeldAsAProposal(t *testing.T) {
	knowledge := &ingestRecorder{}
	proposer := &recordingProposer{}
	h := NewChatDomainHandler(&incomingTestGenerator{}).
		WithIncomingContent(NewIncomingContentWorkflow(&incomingTestReader{}, nil, func() bool { return true })).
		WithKnowledgePersistence(knowledge, nil).
		WithKnowledgeProposals(proposer).
		WithNingoFacts(chat.NingoFacts{People: []chat.KnownPerson{{Name: "Tiago", WhatsApp: "55-999"}}})
	event := core.Event{Source: core.SourceWhatsApp, User: core.EventUser{ID: "55-999"},
		Payload: map[string]any{"content": "focus on results", "locale": "en",
			"document": &chat.IncomingDocument{Filename: "memo.pdf", DataB64: base64.StdEncoding.EncodeToString([]byte("file"))}}}
	reply, err := h.Handle(t.Context(), event, core.WorkflowStep{Action: chat.ActionReadIncomingContent})
	if err != nil || reply["reply"] != "summary" {
		t.Fatalf("document turn failed: %v %+v", err, reply)
	}
	if len(knowledge.payloads) != 0 {
		t.Fatalf("a pending document reached the store: %v", knowledge.payloads)
	}
	if len(proposer.calls) != 1 || proposer.calls[0].kind != "document" ||
		proposer.calls[0].payload["filename"] != "memo.pdf" || proposer.calls[0].payload["content_b64"] == "" {
		t.Fatalf("the known person's document was not held: %+v", proposer.calls)
	}
	if proposer.calls[0].by != "Tiago <55-999>" {
		t.Fatalf("the proposal lost its proposer: %+v", proposer.calls[0])
	}
}

// runSearchTurns drives the two-turn flow a web search needs: a link read the
// follow-up cannot answer, which triggers the search once. Both turns speak as
// the second event's user: the kept readings are per user, and a different
// speaker would start with no context and never reach the search.
func runSearchTurns(t *testing.T, h *ChatDomainHandler, second core.Event) {
	t.Helper()
	first := chatEvent("https://example.com/book")
	first.User.ID = second.User.ID
	if _, err := h.Handle(context.Background(), first, chatStep); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Handle(context.Background(), second, chatStep); err != nil {
		t.Fatal(err)
	}
}

func TestOwnerWebSearchIsHeldAsANingoProposal(t *testing.T) {
	knowledge := &ingestRecorder{changed: true}
	proposer := &recordingProposer{}
	h := NewChatDomainHandler(&triagingGenerator{triageModel: &fakeChatGenerator{result: map[string]any{"reply": "ok"}}, verdict: true}).
		WithWebReading(&shopService{}, nil).
		WithKnowledgePersistence(knowledge, nil).
		WithKnowledgeProposals(proposer)
	second := chatEvent("me fale do kian")
	second.Context = map[string]any{"is_owner": true}
	runSearchTurns(t, h, second)
	if len(knowledge.payloads) != 0 {
		t.Fatalf("a search result reached the store without approval: %v", knowledge.payloads)
	}
	if len(proposer.calls) != 1 || proposer.calls[0].by != "Ningo" ||
		proposer.calls[0].payload["url"] != "https://example.com/kian" {
		t.Fatalf("the owner's search was not held as Ningo's: %+v", proposer.calls)
	}
	tags, _ := proposer.calls[0].payload["tags"].([]string)
	if len(tags) != 1 || tags[0] != "via:search" {
		t.Fatalf("the search proposal lost its via tag: %+v", proposer.calls[0].payload)
	}
}

func TestKnownPersonWebSearchIsHeldAsAProposal(t *testing.T) {
	knowledge := &ingestRecorder{}
	proposer := &recordingProposer{}
	h := NewChatDomainHandler(&triagingGenerator{triageModel: &fakeChatGenerator{result: map[string]any{"reply": "ok"}}, verdict: true}).
		WithWebReading(&shopService{}, nil).
		WithKnowledgePersistence(knowledge, nil).
		WithKnowledgeProposals(proposer).
		WithNingoFacts(chat.NingoFacts{People: []chat.KnownPerson{{Name: "Nick", RealName: "Sam", Discord: "sam-id"}}})
	second := chatEvent("me fale do kian")
	second.User.ID = "sam-id"
	second.Context = map[string]any{"is_owner": false, "guild_id": "guild", "message_id": "m9"}
	runSearchTurns(t, h, second)
	if len(knowledge.payloads) != 0 {
		t.Fatalf("a pending search reached the store: %v", knowledge.payloads)
	}
	// The known speaker's link read is also held; the search is the kian one.
	var held *recordedProposal
	for i := range proposer.calls {
		if proposer.calls[i].payload["url"] == "https://example.com/kian" {
			held = &proposer.calls[i]
		}
	}
	if held == nil {
		t.Fatalf("the known person's search was not held: %+v", proposer.calls)
	}
	if held.kind != "web_article" {
		t.Fatalf("the search proposal lost its kind: %+v", held)
	}
	if held.by != "Ningo" || held.sentBy != "Sam <sam-id>" {
		t.Fatalf("the search proposal lost its attribution: %+v", held)
	}
}

// A search is Ningo's own act: even a stranger's question holds its results for
// the owner, attributed to Ningo itself.
func TestStrangerWebSearchIsHeldAsANingoProposal(t *testing.T) {
	knowledge := &ingestRecorder{}
	proposer := &recordingProposer{}
	h := NewChatDomainHandler(&triagingGenerator{triageModel: &fakeChatGenerator{result: map[string]any{"reply": "ok"}}, verdict: true}).
		WithWebReading(&shopService{}, nil).
		WithKnowledgePersistence(knowledge, nil).
		WithKnowledgeProposals(proposer).
		WithNingoFacts(chat.NingoFacts{People: []chat.KnownPerson{{Name: "Nick", Discord: "sam-id"}}})
	second := chatEvent("me fale do kian")
	second.User.ID = "someone-else"
	runSearchTurns(t, h, second)
	if len(knowledge.payloads) != 0 {
		t.Fatalf("a stranger's search reached the store: %v", knowledge.payloads)
	}
	if len(proposer.calls) != 1 || proposer.calls[0].by != "Ningo" ||
		proposer.calls[0].payload["url"] != "https://example.com/kian" {
		t.Fatalf("the stranger's search was not held as Ningo's: %+v", proposer.calls)
	}
}

func TestFailedWebSearchHoldsNothing(t *testing.T) {
	knowledge := &ingestRecorder{}
	proposer := &recordingProposer{}
	h := NewChatDomainHandler(&triagingGenerator{triageModel: &fakeChatGenerator{result: map[string]any{"reply": "ok"}}, verdict: true}).
		WithWebReading(&shopService{searchFail: true}, nil).
		WithKnowledgePersistence(knowledge, nil).
		WithKnowledgeProposals(proposer)
	runSearchTurns(t, h, chatEvent("me fale do kian"))
	if len(proposer.calls) != 0 || len(knowledge.payloads) != 0 {
		t.Fatalf("a failed search kept something: proposals=%d stored=%d", len(proposer.calls), len(knowledge.payloads))
	}
}
