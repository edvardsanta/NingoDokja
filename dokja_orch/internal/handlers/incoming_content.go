package handlers

import (
	"context"
	"encoding/base64"
	"fmt"
	chat "read_books/dokja_domain/dokja_chat"
	digest "read_books/dokja_domain/dokja_digest"
	"read_books/internal/core"
	"strings"
)

type IncomingContentWorkflow struct {
	reader  DocumentReader
	posts   *digest.Domain
	enabled func() bool
}

func NewIncomingContentWorkflow(reader DocumentReader, posts digest.Service, enabled func() bool) *IncomingContentWorkflow {
	return &IncomingContentWorkflow{reader: reader, posts: digest.New(posts), enabled: enabled}
}
func (h *ChatDomainHandler) WithIncomingContent(workflow *IncomingContentWorkflow) *ChatDomainHandler {
	h.incoming = workflow
	return h
}
func contentRefusal(reason string) map[string]any { return map[string]any{"content_error": reason} }

func (h *ChatDomainHandler) handleIncomingContent(ctx context.Context, event core.Event) (map[string]any, error) {
	if event.Source != core.SourceWhatsApp {
		return nil, fmt.Errorf("incoming content requires the WhatsApp gateway")
	}
	document, _ := event.Payload["document"].(*chat.IncomingDocument)
	content, _ := event.Payload["content"].(string)
	plan := chat.PlanIncomingContent(content, document)
	if plan.Error != "" {
		return contentRefusal(plan.Error), nil
	}
	if h.incoming == nil || (h.incoming.enabled != nil && !h.incoming.enabled()) {
		return contentRefusal("reader_unavailable"), nil
	}
	if plan.Kind == "post" {
		result, err := h.incoming.posts.Handle(ctx, digest.Request{Action: digest.ActionReadLinkDelivery, Event: digest.Event{Payload: map[string]any{"url": plan.URL}}})
		if err != nil {
			return contentRefusal("post_failed"), nil
		}
		if result["status"] != "ok" {
			reason, _ := result["refused"].(string)
			return contentRefusal("post_" + reason), nil
		}
		post, _ := result["post"].(map[string]any)
		caption, _ := post["caption"].(string)
		author, _ := post["author"].(string)
		address, _ := post["url"].(string)
		text := strings.TrimSpace(strings.Join([]string{author, caption, address}, "\n\n"))
		media, _ := result["media"].([]digest.DeliveryMedia)
		attachments := make([]chat.Attachment, 0, len(media))
		for _, item := range media {
			attachments = append(attachments, chat.Attachment{Filename: item.Filename, ContentType: item.ContentType, DataB64: item.DataB64})
		}
		return map[string]any{"reply": text, "attachments": attachments}, nil
	}
	if plan.Kind != "document" {
		return nil, fmt.Errorf("incoming content is required")
	}
	if plan.Document.DataB64 == "" {
		return map[string]any{"document_required": true}, nil
	}
	data, err := base64.StdEncoding.Strict().DecodeString(plan.Document.DataB64)
	if err != nil || len(data) == 0 {
		return contentRefusal("document_unreadable"), nil
	}
	if len(data) > chat.MaxIncomingDocumentBytes {
		return contentRefusal("document_too_large"), nil
	}
	if h.incoming.reader == nil {
		return contentRefusal("reader_unavailable"), nil
	}
	extracted, err := h.incoming.reader.Extract(ctx, plan.Document.Filename, plan.Document.DataB64)
	if err != nil {
		return contentRefusal("document_read_failed"), nil
	}
	documents := readDocuments(extracted["documents"])
	var text strings.Builder
	for _, document := range documents {
		body := document.Text
		if strings.TrimSpace(body) != "" {
			text.WriteString(body)
			text.WriteString("\n\n")
		}
	}
	if strings.TrimSpace(text.String()) == "" {
		return contentRefusal("document_unreadable"), nil
	}
	locale, _ := event.Payload["locale"].(string)
	summary, partial := chat.DocumentSummaryMessages(plan.Document.Filename, text.String(), content, locale)
	messages := make([]map[string]string, 0, len(summary))
	for _, message := range summary {
		messages = append(messages, map[string]string{"role": message.Role, "content": message.Content})
	}
	var answer map[string]any
	if generator, ok := h.generator.(ChatConversationGenerator); ok {
		answer, err = generator.GenerateMessages(ctx, messages)
	} else {
		answer, err = h.generator.Generate(ctx, messages[0]["content"]+"\n\n"+messages[1]["content"])
	}
	if err != nil {
		return contentRefusal("summary_failed"), nil
	}
	reply := chatReplyFromResult(answer)
	if strings.TrimSpace(reply) == "" {
		return contentRefusal("summary_failed"), nil
	}
	return map[string]any{"reply": reply, "summary_partial": partial}, nil
}
