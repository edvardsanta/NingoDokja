package handlers

import (
	"context"
	"encoding/base64"
	"errors"
	chat "read_books/dokja_domain/dokja_chat"
	"read_books/internal/core"
	"strings"
	"testing"
)

type incomingTestReader struct {
	calls    int
	filename string
	fail     bool
	empty    bool
}

func (r *incomingTestReader) Extract(_ context.Context, filename, encoded string) (map[string]any, error) {
	r.calls++
	r.filename = filename
	if r.fail {
		return nil, errors.New("private service detail")
	}
	if r.empty {
		return map[string]any{"documents": []any{}}, nil
	}
	return map[string]any{"documents": []any{map[string]any{"title": "Memo", "text": "The experiment showed a stable improvement.", "source_ref": "", "key": ""}}}, nil
}

type incomingTestGenerator struct {
	calls    int
	messages []map[string]string
}

func (g *incomingTestGenerator) Generate(context.Context, string) (map[string]any, error) {
	g.calls++
	return map[string]any{"reply": "summary"}, nil
}
func (g *incomingTestGenerator) GenerateMessages(_ context.Context, messages []map[string]string) (map[string]any, error) {
	g.calls++
	g.messages = messages
	return map[string]any{"reply": "summary"}, nil
}
func TestIncomingDocumentAuthorizesMetadataBeforeExtractingOrGenerating(t *testing.T) {
	reader := &incomingTestReader{}
	generator := &incomingTestGenerator{}
	handler := NewChatDomainHandler(generator).WithIncomingContent(NewIncomingContentWorkflow(reader, nil, func() bool { return true }))
	event := core.Event{Source: core.SourceWhatsApp, Payload: map[string]any{"content": "focus on results", "locale": "en", "document": &chat.IncomingDocument{Filename: "memo.pdf", Size: 1}}}
	step := core.WorkflowStep{Action: chat.ActionReadIncomingContent}
	reply, err := handler.Handle(t.Context(), event, step)
	if err != nil || reply["document_required"] != true || reader.calls != 0 || generator.calls != 0 {
		t.Fatal("metadata caused a download or generation")
	}
	event.Payload["document"].(*chat.IncomingDocument).DataB64 = base64.StdEncoding.EncodeToString([]byte("file"))
	reply, err = handler.Handle(t.Context(), event, step)
	if err != nil || reply["reply"] != "summary" || reader.calls != 1 || generator.calls != 1 || reader.filename != "memo.pdf" {
		t.Fatalf("document summary failed: %v %+v", err, reply)
	}
	if !strings.Contains(generator.messages[0]["content"], "English") || !strings.Contains(generator.messages[1]["content"], "stable improvement") {
		t.Fatal("extracted text or locale not delivered")
	}
}
func TestIncomingDocumentFailureNeverInventsASummary(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		document              *chat.IncomingDocument
		fail, empty, disabled bool
		reason                string
	}{
		{name: "unsupported", document: &chat.IncomingDocument{Filename: "app.exe"}, reason: "document_unsupported"},
		{name: "invalid bytes", document: &chat.IncomingDocument{Filename: "memo.pdf", DataB64: "!"}, reason: "document_unreadable"},
		{name: "too large", document: &chat.IncomingDocument{Filename: "memo.pdf", Size: chat.MaxIncomingDocumentBytes + 1}, reason: "document_too_large"},
		{name: "failed reader", document: &chat.IncomingDocument{Filename: "memo.pdf", DataB64: "eA=="}, fail: true, reason: "document_read_failed"},
		{name: "unreadable", document: &chat.IncomingDocument{Filename: "memo.pdf", DataB64: "eA=="}, empty: true, reason: "document_unreadable"},
		{name: "disabled", document: &chat.IncomingDocument{Filename: "memo.pdf"}, disabled: true, reason: "reader_unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			generator := &incomingTestGenerator{}
			reader := &incomingTestReader{fail: tc.fail, empty: tc.empty}
			handler := NewChatDomainHandler(generator).WithIncomingContent(NewIncomingContentWorkflow(reader, nil, func() bool { return !tc.disabled }))
			reply, err := handler.Handle(t.Context(), core.Event{Source: core.SourceWhatsApp, Payload: map[string]any{"document": tc.document}}, core.WorkflowStep{Action: chat.ActionReadIncomingContent})
			if err != nil || reply["content_error"] != tc.reason || generator.calls != 0 {
				t.Fatalf("failure not preserved: %v %+v", err, reply)
			}
		})
	}
}
