package handlers

import (
	"context"
	"fmt"
	"strings"
	"testing"

	chat "read_books/dokja_domain/dokja_chat"
	digest "read_books/dokja_domain/dokja_digest"
	"read_books/internal/core"
)

type whatsappPostReader struct {
	mode  string
	calls int
}

func (s *whatsappPostReader) Dispatch(_ context.Context, kind string, payload map[string]any) (map[string]any, error) {
	s.calls++
	if kind == "feeds.status" {
		if s.mode == "no_reader" {
			return map[string]any{"providers": []any{}}, nil
		}
		return map[string]any{"providers": []any{map[string]any{"id": "example", "enabled": true, "provides": []any{digest.SocialCapability}}}}, nil
	}
	if kind != "feeds.invoke" {
		return nil, fmt.Errorf("unexpected event %s", kind)
	}
	if payload["action"] == digest.ActionMedia {
		return map[string]any{"data_b64": "eA==", "content_type": "image/png"}, nil
	}
	if payload["action"] == digest.ActionMediaFile {
		return nil, fmt.Errorf("unsupported action")
	}
	if s.mode == "no_media" {
		return map[string]any{"handled": true, "post": map[string]any{"url": "https://example.com/news", "caption": "News caption", "media": []any{}}}, nil
	}
	if s.mode == "unsupported" {
		return map[string]any{"handled": false}, nil
	}
	if s.mode == "login_required" {
		return map[string]any{"handled": true, "refused": "login_required"}, nil
	}
	return map[string]any{"handled": true, "post": map[string]any{"url": "https://example.com/post", "caption": "Original post caption", "author": "Example author", "media": []any{map[string]any{"kind": "image", "url": "https://media.example/image.png"}}}}, nil
}
func TestWhatsAppBareLinksFallBackToWebReadingAndKeepPostDelivery(t *testing.T) {
	for _, mode := range []string{"unsupported", "no_reader", "no_media", "post", "login_required"} {
		t.Run(mode, func(t *testing.T) {
			reader := &webReadingService{}
			posts := &whatsappPostReader{mode: mode}
			model := &fakeChatGenerator{result: map[string]any{"reply": "The estimate requires assumptions. [1]"}}
			h := NewChatDomainHandler(model).WithWebReading(reader, nil).WithIncomingContent(NewIncomingContentWorkflow(nil, posts, nil))
			event := whatsappTurn("https://example.com/news", "message-1", false)
			result, err := h.Handle(t.Context(), event, core.WorkflowStep{Action: chat.ActionReadIncomingContent})
			if err != nil {
				t.Fatal(err)
			}
			if mode == "unsupported" || mode == "no_reader" || mode == "no_media" {
				if len(reader.calls) != 1 || !strings.Contains(fmt.Sprint(result["reply"]), "https://example.com/news") || !strings.Contains(fmt.Sprint(model.messages), "UNTRUSTED_WEB_EVIDENCE") {
					t.Fatal(result, reader.calls)
				}
			} else if mode == "post" {
				if len(reader.calls) != 0 || !strings.Contains(fmt.Sprint(result["reply"]), "Original post caption") || len(model.messages) > 0 {
					t.Fatal(result)
				}
			} else if result["content_error"] != "post_login_required" || len(reader.calls) > 0 {
				t.Fatal("post refusal was lost", result)
			}
		})
	}
}
func TestWhatsAppWebReadingFollowupsStayWithinTheirConversation(t *testing.T) {
	reader := &webReadingService{}
	model := &fakeChatGenerator{result: map[string]any{"reply": "The estimate requires assumptions. [1]"}}
	h := NewChatDomainHandler(model).WithWebReading(reader, nil)
	first := whatsappTurn("Explain https://example.com/news", "m1", false)
	if _, err := h.Handle(t.Context(), first, chatStep); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Handle(t.Context(), whatsappTurn("Explain its assumptions", "m2", false), chatStep); err != nil {
		t.Fatal(err)
	}
	if len(reader.calls) != 1 || !strings.Contains(fmt.Sprint(model.messages), "UNTRUSTED_WEB_EVIDENCE") {
		t.Fatal("follow-up lost reading")
	}
	other := whatsappTurn("Explain its assumptions", "m3", false)
	other.Channel.ID = "another-chat"
	if _, err := h.Handle(t.Context(), other, chatStep); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fmt.Sprint(model.messages), "UNTRUSTED_WEB_EVIDENCE") {
		t.Fatal("reading crossed chats")
	}
	discord := chatEvent("Explain its assumptions")
	if _, err := h.Handle(t.Context(), discord, chatStep); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fmt.Sprint(model.messages), "UNTRUSTED_WEB_EVIDENCE") {
		t.Fatal("reading crossed interfaces")
	}
}
func TestWhatsAppSelfChatReadsLinksAndArchivesOnlyValidatedCitations(t *testing.T) {
	archive := archiveFor(t)
	reader := &webReadingService{}
	model := &fakeChatGenerator{result: map[string]any{"reply": "The estimate requires assumptions. [1] [999] https://attacker.example/collect"}}
	h := NewChatDomainHandler(model).WithArchive(archive).WithWebReading(reader, nil)
	first := whatsappTurn("Explain https://example.com/news", "m1", true)
	result, err := h.Handle(t.Context(), first, chatStep)
	if err != nil {
		t.Fatal(err)
	}
	reply := fmt.Sprint(result["reply"])
	if len(reader.calls) != 1 || !strings.Contains(reply, "https://example.com/news") || strings.Contains(reply, "attacker.example") || strings.Contains(reply, "[999]") {
		t.Fatal(reply)
	}
	held := listed(t, archive)[0]
	turns := turnsOf(t, archive, held.ID)
	if turns[0].Reply != reply {
		t.Fatal("archive kept unvalidated reply", turns)
	}
	if _, err := h.Handle(t.Context(), whatsappTurn("Explain its assumptions", "m2", true), chatStep); err != nil {
		t.Fatal(err)
	}
	if len(reader.calls) != 1 || !strings.Contains(fmt.Sprint(model.messages), "UNTRUSTED_WEB_EVIDENCE") {
		t.Fatal("self-chat follow-up lost reading")
	}
}
func TestWhatsAppReadingFailureUsesLocalesWithoutModelSpeculation(t *testing.T) {
	for _, self := range []bool{false, true} {
		for _, locale := range []string{"pt-BR", "en"} {
			t.Run(fmt.Sprintf("%t/%s", self, locale), func(t *testing.T) {
				model := &fakeChatGenerator{result: map[string]any{"reply": "Must not be generated"}}
				h := NewChatDomainHandler(model).WithWebReading(&webReadingService{fail: true}, nil)
				if self {
					h.WithArchive(archiveFor(t))
				}
				event := whatsappTurn("Read https://example.com/unavailable", "m1", self)
				event.Payload["locale"] = locale
				result, err := h.Handle(t.Context(), event, chatStep)
				if err != nil {
					t.Fatal(err)
				}
				reply := fmt.Sprint(result["reply"])
				want := "Não consegui ler"
				if locale == "en" {
					want = "could not"
				}
				if !strings.Contains(strings.ToLower(reply), strings.ToLower(want)) || len(model.messages) > 0 || strings.Contains(reply, "private service error") {
					t.Fatal(reply)
				}
			})
		}
	}
}

func TestWhatsAppReadingSharesLinkLimitsAndBlockedPageNotices(t *testing.T) {
	reader := &webReadingService{}
	model := &fakeChatGenerator{result: map[string]any{"reply": "The estimate requires assumptions. [1]"}}
	h := NewChatDomainHandler(model).WithWebReading(reader, nil)
	links := []string{}
	for i := 0; i < chat.MaxWebLinks+1; i++ {
		links = append(links, fmt.Sprintf("https://example.com/news/%d", i))
	}
	event := whatsappTurn(strings.Join(links, " "), "bounded", false)
	event.Payload["locale"] = "en"
	result, err := h.Handle(t.Context(), event, chatStep)
	if err != nil || len(reader.calls) != chat.MaxWebLinks || !strings.Contains(fmt.Sprint(result["reply"]), "additional links unread") {
		t.Fatal(result, reader.calls, err)
	}
	blocked := &webReadingService{fail: true, failureReason: "web_blocked: private backend diagnostic"}
	h = NewChatDomainHandler(model).WithWebReading(blocked, nil)
	result, err = h.Handle(t.Context(), whatsappTurn("Read https://example.com/news", "blocked", false), chatStep)
	if err != nil || !strings.Contains(fmt.Sprint(result["reply"]), "bloqueou") || strings.Contains(fmt.Sprint(result), "private backend") {
		t.Fatal(result, err)
	}
}
