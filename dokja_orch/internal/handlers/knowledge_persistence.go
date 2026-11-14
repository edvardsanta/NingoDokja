package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	chat "read_books/dokja_domain/dokja_chat"
	knowledgedomain "read_books/dokja_domain/dokja_knowledge"
	"read_books/internal/core"
	"read_books/internal/logger"
)

// WithKnowledgePersistence keeps owner-supplied readings durable in Knowledge: web links the
// owner asked about and documents the owner sent. Persistence is best effort and never
// changes the reply; eligibility is limited to the owner (ownerReadingTurn).
func (h *ChatDomainHandler) WithKnowledgePersistence(service knowledgedomain.Service, enabled func() bool) *ChatDomainHandler {
	h.knowledgePersist = &knowledgePersistence{domain: knowledgedomain.New(service), enabled: enabled}
	return h
}

type knowledgePersistence struct {
	domain  *knowledgedomain.Domain
	enabled func() bool
}

// ownerReadingTurn gates durable knowledge on who supplied the content: the owner's own
// WhatsApp self-chat, or a Discord turn whose speaker the gateway verified as the owner.
// Someone else's message is read for the reply but never becomes the owner's knowledge just
// because Ningo saw it.
func ownerReadingTurn(event core.Event) bool {
	if event.Source == core.SourceDiscord {
		return boolFromContext(event.Context, "is_owner")
	}
	return selfChatRequest(event)
}

func (p *knowledgePersistence) ready(event core.Event) bool {
	return p != nil && p.domain != nil && ownerReadingTurn(event) && (p.enabled == nil || p.enabled())
}

// persistWebReading stores one successfully decoded web document under the same stable
// web:<hash> identity the turn's citations use, so a re-read updates the durable copy
// instead of adding a new source.
func (p *knowledgePersistence) persistWebReading(ctx context.Context, event core.Event, document chat.WebDocument) {
	if !p.ready(event) {
		return
	}
	var text strings.Builder
	for _, passage := range document.Passages {
		if strings.TrimSpace(passage.Text) == "" {
			continue
		}
		if text.Len() > 0 {
			text.WriteString("\n\n")
		}
		text.WriteString(passage.Text)
	}
	if strings.TrimSpace(text.String()) == "" {
		return
	}
	hash := sha256.Sum256([]byte(document.URL))
	stored, err := p.domain.StoreReading(ctx, knowledgedomain.Reading{
		Filename: document.URL,
		Documents: []knowledgedomain.ReadDocument{{
			Title: document.Title, Text: text.String(), SourceRef: document.URL,
		}},
		Payload: map[string]any{
			"source_id":  "web:" + hex.EncodeToString(hash[:12]),
			"source_ref": document.URL,
			"kind":       "article",
			"tags":       []string{"origin:" + string(event.Source)},
		},
	})
	if err != nil {
		logger.Info(fmt.Sprintf("web reading was not persisted: %v", err))
		return
	}
	logger.Info(fmt.Sprintf("web reading persisted stored=%d", len(stored)))
}

// persistExtractedReading stores the documents the reading host extracted from a file the
// owner sent, keeping the reading's format and reader provenance like knowledge.read does.
func (p *knowledgePersistence) persistExtractedReading(ctx context.Context, event core.Event, filename string, answer map[string]any, contentB64 string, documents []knowledgedomain.ReadDocument) {
	if !p.ready(event) || len(documents) == 0 {
		return
	}
	stored, err := p.domain.StoreReading(ctx, knowledgedomain.Reading{
		Filename: filename, ContentB64: contentB64,
		Format: resultString(answer, "format"), Reader: resultString(answer, "plugin"), ReaderVersion: resultString(answer, "version"),
		Documents: documents,
		Payload:   map[string]any{"tags": []string{"origin:" + string(event.Source)}},
	})
	if err != nil {
		logger.Info(fmt.Sprintf("extracted reading was not persisted: %v", err))
		return
	}
	logger.Info(fmt.Sprintf("extracted reading persisted stored=%d", len(stored)))
}
