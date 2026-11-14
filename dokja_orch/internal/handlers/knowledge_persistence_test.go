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
	if len(tags) != 1 || tags[0] != "origin:discord" {
		t.Fatalf("unexpected provenance tags: %v", payload["tags"])
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
			if len(tags) != 1 || tags[0] != "origin:whatsapp" {
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
