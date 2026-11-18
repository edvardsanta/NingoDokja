package handlers

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	knowledgedomain "read_books/dokja_domain/dokja_knowledge"
	"read_books/internal/core"
	"read_books/internal/logger"
)

// actionSaveKnowledge stores one link the owner chose as a document of their own
// library. It belongs to the internal handler, not the domain: the handler gates
// the owner, gives the document its Discord identity and wording, and reuses the
// domain's ingest contract unchanged.
const actionSaveKnowledge = "save-knowledge"

// saveRefusal codes. Interfaces word them; the handler's own text never reaches a person.
const (
	saveRefusedNotOwner    = "not_owner"
	saveRefusedInvalidLink = "invalid_link"
	saveRefusedUnavailable = "unavailable"
)

// saveLinkMaxChars bounds a link to the knowledge source_ref field it also feeds.
const saveLinkMaxChars = 2000

// saveParseLink accepts an http(s) address without credentials, the only thing a
// person supplies to the save command.
func saveParseLink(raw string) (string, error) {
	link := strings.TrimSpace(raw)
	parsed, err := url.Parse(link)
	if err != nil || link == "" || len(link) > saveLinkMaxChars || parsed.User != nil || parsed.Hostname() == "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("save link must be an http(s) address without credentials")
	}
	return link, nil
}

// saveDocument stores the owner's link. The Discord message that carried it is the
// document's reference, so a later citation points at the exact message; the link
// itself stays in the metadata and feeds the fetch.
func (h *KnowledgeDomainHandler) saveDocument(ctx context.Context, event core.Event) (map[string]any, error) {
	if !boolFromContext(event.Context, "is_owner") {
		return saveRefused(saveRefusedNotOwner), nil
	}
	link, err := saveParseLink(payloadString(event.Payload, "link"))
	if err != nil {
		return saveRefused(saveRefusedInvalidLink), nil
	}
	guildID := payloadString(event.Payload, "guildId")
	channelID := payloadString(event.Payload, "channelId")
	messageID := payloadString(event.Payload, "messageId")
	sourceID, reference := saveIdentity(link, guildID, channelID, messageID)
	result, err := h.domain.Handle(ctx, knowledgedomain.Request{
		Action: knowledgedomain.ActionIngestKnowledge,
		Event: knowledgedomain.Event{
			ID:     event.EventID,
			Source: string(event.Source),
			Type:   event.Type,
			Payload: map[string]any{
				"source":     link,
				"kind":       "note",
				"source_id":  sourceID,
				"source_ref": reference,
				"tags":       []string{"via:save"},
			},
			Context: event.Context,
		},
	})
	if err != nil {
		logger.Error("knowledge save was not ingested", err)
		return saveRefused(saveRefusedUnavailable), nil
	}
	return map[string]any{
		"status":  "ok",
		"created": result["created"],
		"changed": result["changed"],
	}, nil
}

// saveIdentity pins the document to the Discord message that carried the link, so
// citations reference that message exactly. Without the full message identity (for
// example a direct message), the document falls back to the link itself.
func saveIdentity(link, guildID, channelID, messageID string) (sourceID, reference string) {
	if guildID != "" && channelID != "" && messageID != "" {
		return fmt.Sprintf("discord-message:%s/%s/%s", guildID, channelID, messageID),
			fmt.Sprintf("https://discord.com/channels/%s/%s/%s", guildID, channelID, messageID)
	}
	return "discord-save:" + link, link
}

func saveRefused(reason string) map[string]any {
	return map[string]any{"status": "refused", "refused": reason}
}
