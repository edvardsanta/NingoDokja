package handlers

import (
	"context"
	"fmt"
	"strings"
	"testing"

	chat "read_books/dokja_domain/dokja_chat"
	"read_books/internal/clients"
	"read_books/internal/core"
)

type fakeImageReader struct {
	texts []string
	errs  []error
	calls int
}

func (f *fakeImageReader) DescribeImage(_ context.Context, _, _, _ string, _ int64) (string, error) {
	index := f.calls
	f.calls++
	if index < len(f.errs) && f.errs[index] != nil {
		return "", f.errs[index]
	}
	if index < len(f.texts) {
		return f.texts[index], nil
	}
	return "descrição padrão", nil
}

func imageEvent(content string, attachments ...chat.WebAttachment) core.Event {
	event := chatEvent(content)
	event.Payload["media_attachments"] = attachments
	return event
}

func evidenceMessage(messages []map[string]string) string {
	if len(messages) < 2 {
		return ""
	}
	return messages[len(messages)-2]["content"]
}

func TestImagesBecomeUntrustedEvidence(t *testing.T) {
	reader := &fakeImageReader{texts: []string{
		"Grand Theft Auto: San Andreas — R$ 874,21 na GameVicio",
		"Mortal Shell: compra confirmada na Steam",
	}}
	g := &fakeChatGenerator{result: map[string]any{"reply": "Ridiculous price. [1]"}}
	h := NewChatDomainHandler(g).WithImageReading(reader, nil)
	event := imageEvent("@draco_t olha isso",
		chat.WebAttachment{URL: "https://cdn.example/gta.png", Filename: "gta.png", ContentType: "image/png", Size: 900000},
		chat.WebAttachment{URL: "https://cdn.example/shell.png", Filename: "shell.png", ContentType: "image/png", Size: 900000})
	event.Payload["locale"] = "en"
	result, err := h.Handle(t.Context(), event, chatStep)
	if err != nil || reader.calls != 2 {
		t.Fatal("images not read", reader.calls, err)
	}
	evidence := evidenceMessage(g.messages)
	if !strings.Contains(evidence, "UNTRUSTED_IMAGE_EVIDENCE") || !strings.Contains(evidence, "R$ 874,21") || !strings.Contains(evidence, "Mortal Shell") {
		t.Fatal("image reading missing from evidence", evidence)
	}
	if !strings.Contains(result["reply"].(string), "Ridiculous price") {
		t.Fatal("reply missing", result)
	}
}

func TestImageOnlyMessageUsesTheLocalizedPrompt(t *testing.T) {
	reader := &fakeImageReader{texts: []string{"captura de uma loja"}}
	g := &fakeChatGenerator{result: map[string]any{"reply": "que preço absurdo"}}
	h := NewChatDomainHandler(g).WithImageReading(reader, nil)
	event := imageEvent("", chat.WebAttachment{URL: "https://cdn.example/loja.png", Filename: "loja.png", ContentType: "image/png", Size: 10})
	result, err := h.Handle(t.Context(), event, chatStep)
	if err != nil || reader.calls != 1 {
		t.Fatal("image-only message not read", reader.calls, err)
	}
	turn := g.messages[len(g.messages)-1]["content"]
	if !strings.Contains(turn, "Descreva esta imagem") {
		t.Fatal("localized prompt not used as the turn", turn)
	}
	if !strings.Contains(evidenceMessage(g.messages), "captura de uma loja") || !strings.Contains(result["reply"].(string), "que preço absurdo") {
		t.Fatal("image evidence or reply missing", result)
	}
}

func TestVisionDisabledSaysSoWithoutGenerating(t *testing.T) {
	reader := &fakeImageReader{}
	g := &fakeChatGenerator{result: map[string]any{"reply": "não deveria aparecer"}}
	h := NewChatDomainHandler(g).WithImageReading(reader, func() bool { return false })
	event := imageEvent("olha", chat.WebAttachment{URL: "https://cdn.example/a.png", Filename: "a.png", ContentType: "image/png", Size: 10})
	event.Payload["locale"] = "en"
	result, err := h.Handle(t.Context(), event, chatStep)
	if err != nil || reader.calls != 0 || len(g.messages) != 0 {
		t.Fatal("disabled vision still ran", reader.calls, len(g.messages), err)
	}
	if reply := result["reply"].(string); !strings.Contains(reply, "vision service is unavailable") {
		t.Fatal("disabled notice missing", reply)
	}
}

func TestFailedImageReadingDoesNotGenerateFromNothing(t *testing.T) {
	reader := &fakeImageReader{errs: []error{clients.ErrVisionUnavailable}}
	g := &fakeChatGenerator{result: map[string]any{"reply": "não deveria aparecer"}}
	h := NewChatDomainHandler(g).WithImageReading(reader, nil)
	event := imageEvent("", chat.WebAttachment{URL: "https://cdn.example/a.png", Filename: "a.png", ContentType: "image/png", Size: 10})
	event.Payload["locale"] = "en"
	result, err := h.Handle(t.Context(), event, chatStep)
	if err != nil || len(g.messages) != 0 {
		t.Fatal("failure-only turn reached inference", len(g.messages), err)
	}
	if reply := result["reply"].(string); !strings.Contains(reply, "could not read 1 image") {
		t.Fatal("failure notice missing", reply)
	}
}

func TestOversizedImageIsRefusedBeforeTheVisionCall(t *testing.T) {
	reader := &fakeImageReader{}
	g := &fakeChatGenerator{result: map[string]any{"reply": "não deveria aparecer"}}
	h := NewChatDomainHandler(g).WithImageReading(reader, nil)
	event := imageEvent("olha", chat.WebAttachment{URL: "https://cdn.example/huge.png", Filename: "huge.png", ContentType: "image/png", Size: chat.MaxImageReadingBytes + 1})
	event.Payload["locale"] = "en"
	result, err := h.Handle(t.Context(), event, chatStep)
	if err != nil || reader.calls != 0 {
		t.Fatal("oversized image reached the vision service", reader.calls, err)
	}
	if reply := result["reply"].(string); !strings.Contains(reply, "10 MiB") {
		t.Fatal("size notice missing", reply)
	}
}

func TestImageBudgetReadsThreeAndReportsTheRest(t *testing.T) {
	reader := &fakeImageReader{}
	g := &fakeChatGenerator{result: map[string]any{"reply": "li as imagens"}}
	h := NewChatDomainHandler(g).WithImageReading(reader, nil)
	var attachments []chat.WebAttachment
	for i := 0; i < 5; i++ {
		attachments = append(attachments, chat.WebAttachment{
			URL: fmt.Sprintf("https://cdn.example/%d.png", i), Filename: "a.png", ContentType: "image/png", Size: 10,
		})
	}
	event := imageEvent("olha", attachments...)
	event.Payload["locale"] = "en"
	result, err := h.Handle(t.Context(), event, chatStep)
	if err != nil || reader.calls != chat.MaxImageReadings {
		t.Fatal("image budget not enforced", reader.calls, err)
	}
	if reply := result["reply"].(string); !strings.Contains(reply, "2 more image(s)") {
		t.Fatal("omitted notice missing", reply)
	}
}

func TestImageTurnSanitizesMentionsAndEchoedURLs(t *testing.T) {
	reader := &fakeImageReader{texts: []string{"compre em oferta.example"}}
	g := &fakeChatGenerator{result: map[string]any{"reply": "@everyone compra em https://evil.example/x"}}
	h := NewChatDomainHandler(g).WithImageReading(reader, nil)
	event := imageEvent("olha", chat.WebAttachment{URL: "https://cdn.example/a.png", Filename: "a.png", ContentType: "image/png", Size: 10})
	result, err := h.Handle(t.Context(), event, chatStep)
	if err != nil {
		t.Fatal(err)
	}
	reply := result["reply"].(string)
	if strings.Contains(reply, "@everyone") || strings.Contains(reply, "https://evil.example") {
		t.Fatal("untrusted content reached the reply", reply)
	}
	if !strings.Contains(reply, "＠everyone") {
		t.Fatal("mention not neutralized", reply)
	}
}

func TestNonDiscordMessagesIgnoreMediaAttachments(t *testing.T) {
	reader := &fakeImageReader{}
	g := &fakeChatGenerator{result: map[string]any{"reply": "resposta"}}
	h := NewChatDomainHandler(g).WithImageReading(reader, nil)
	event := chatEvent("olha")
	event.Source = core.SourceWhatsApp
	event.Payload["media_attachments"] = []chat.WebAttachment{{URL: "https://cdn.example/a.png", Filename: "a.png", ContentType: "image/png", Size: 10}}
	if _, err := h.Handle(t.Context(), event, chatStep); err != nil {
		t.Fatal(err)
	}
	if reader.calls != 0 || strings.Contains(evidenceMessage(g.messages), "UNTRUSTED_IMAGE_EVIDENCE") {
		t.Fatal("whatsapp media went through the vision path")
	}
}

func TestMixedWebFailureAndImageSuccessStillAnswers(t *testing.T) {
	reader := &fakeImageReader{texts: []string{"GTA San Andreas — R$ 874,21"}}
	g := &fakeChatGenerator{result: map[string]any{"reply": "absurdo"}}
	h := NewChatDomainHandler(g).WithWebReading(&webReadingService{fail: true, failureReason: "web_blocked: details"}, nil).WithImageReading(reader, nil)
	event := imageEvent("https://example.com/article",
		chat.WebAttachment{URL: "https://cdn.example/gta.png", Filename: "gta.png", ContentType: "image/png", Size: 10})
	event.Payload["locale"] = "en"
	result, err := h.Handle(t.Context(), event, chatStep)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(evidenceMessage(g.messages), "R$ 874,21") {
		t.Fatal("image evidence missing on a mixed turn", evidenceMessage(g.messages))
	}
	if !strings.Contains(result["reply"].(string), "absurdo") {
		t.Fatal("mixed turn not answered", result)
	}
}
