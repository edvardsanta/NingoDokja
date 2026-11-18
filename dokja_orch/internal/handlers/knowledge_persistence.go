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
	"time"
)

// knowledgeProposer is what the chat handler needs from the proposals handler.
type knowledgeProposer interface {
	Propose(ctx context.Context, kind string, payload map[string]any, source knowledgedomain.ProposalSource, proposedBy, sentBy string) (string, bool, error)
}

// WithKnowledgeProposals routes what people send for Ningo to learn: the
// owner's readings store directly, a known person's readings are held as
// pending proposals, and a stranger's are read for the reply and kept nowhere.
// Nothing is announced to anyone in any case.
func (h *ChatDomainHandler) WithKnowledgeProposals(proposer knowledgeProposer) *ChatDomainHandler {
	h.knowledgeProposer = proposer
	return h
}

// WithKnowledgePersistence keeps the owner's readings durable in Knowledge:
// web links the owner asked about and documents the owner sent. It is the
// store half of WithKnowledgeProposals; the owner-only gate stays here.
func (h *ChatDomainHandler) WithKnowledgePersistence(service knowledgedomain.Service, enabled func() bool) *ChatDomainHandler {
	h.knowledgePersist = &knowledgePersistence{domain: knowledgedomain.New(service), enabled: enabled}
	return h
}

type knowledgePersistence struct {
	domain  *knowledgedomain.Domain
	enabled func() bool
}

// available is the shared knowledge switch; who may write is decided by the
// caller through ownerReadingTurn or a known-speaker match.
func (p *knowledgePersistence) available(event core.Event) bool {
	return p != nil && p.domain != nil && (p.enabled == nil || p.enabled())
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

// learnFromReading stores what the owner sent, or holds what a known person
// sent as a pending proposal for the owner to decide. A stranger's material is
// read for the reply and kept nowhere. Silence either way: no announcement is
// produced here, and the reply the turn already had is unchanged.
func (h *ChatDomainHandler) learnFromReading(ctx context.Context, event core.Event, kind string, payload map[string]any, store func(context.Context)) {
	if h.knowledgePersist == nil || !h.knowledgePersist.available(event) {
		return
	}
	if ownerReadingTurn(event) {
		store(ctx)
		return
	}
	if h.knowledgeProposer == nil {
		return
	}
	person, ok := h.knownSpeaker(event)
	if !ok {
		return
	}
	by := proposalBy(person, event.User.ID)
	payload["tags"] = viaTags(payload, "via:link")
	h.holdKnowledgeProposal(ctx, event, kind, payload, by, by)
}

// learnFromSearch holds every web-search result pending, whoever asked. A search is
// Ningo's own act and its hits can be junk, so nothing enters knowledge until the owner
// approves it; the proposal is Ningo's, with the speaker it searched for kept as the
// sender when known.
func (h *ChatDomainHandler) learnFromSearch(ctx context.Context, event core.Event, result webSearchResult, retrievedAt string) {
	if h.knowledgePersist == nil || !h.knowledgePersist.available(event) || h.knowledgeProposer == nil {
		return
	}
	sender := "Ningo"
	if boolFromContext(event.Context, "is_owner") {
		sender = "owner"
	} else if person, ok := h.knownSpeaker(event); ok {
		sender = proposalBy(person, event.User.ID)
	}
	h.holdKnowledgeProposal(ctx, event, "web_article", map[string]any{"url": result.url, "tags": []string{"via:search"}}, "Ningo", sender)
}

// proposalBy names a proposal after the person who sent the material, with the
// verified account id appended so same-name people stay distinguishable.
func proposalBy(person chat.KnownPerson, userID string) string {
	by := person.Name
	if person.RealName != "" {
		by = person.RealName
	}
	if userID != "" {
		by = fmt.Sprintf("%s <%s>", by, userID)
	}
	return by
}

// knownSpeaker matches the turn's speaker against the owner's people mapping,
// so a proposal can say who sent the material by name instead of by account id.
func (h *ChatDomainHandler) knownSpeaker(event core.Event) (chat.KnownPerson, bool) {
	id := event.User.ID
	if id == "" {
		return chat.KnownPerson{}, false
	}
	for _, person := range h.ningo.People {
		if person.Discord != "" && person.Discord == id {
			return person, true
		}
		if person.WhatsApp != "" && person.WhatsApp == id {
			return person, true
		}
	}
	return chat.KnownPerson{}, false
}

// holdKnowledgeProposal holds one submission pending. It answers nothing: the
// proposer learns only that, later, Ningo knows it or does not.
func (h *ChatDomainHandler) holdKnowledgeProposal(ctx context.Context, event core.Event, kind string, payload map[string]any, proposedBy, sentBy string) {
	source := knowledgedomain.ProposalSource{
		ChannelID: event.Channel.ID, MessageID: resultString(event.Context, "message_id"),
	}
	if event.Source == core.SourceDiscord && source.ChannelID != "" && source.MessageID != "" {
		if guild := resultString(event.Context, "guild_id"); guild != "" {
			source.Permalink = fmt.Sprintf("https://discord.com/channels/%s/%s/%s", guild, source.ChannelID, source.MessageID)
		}
	}
	id, duplicate, err := h.knowledgeProposer.Propose(ctx, kind, payload, source, proposedBy, sentBy)
	if err != nil {
		logger.Info(fmt.Sprintf("knowledge proposal was not held: %v", err))
		return
	}
	logger.Info(fmt.Sprintf("knowledge proposal held id=%s duplicate=%t", id, duplicate))
}

// persistWebReading stores one successfully decoded web document under the same stable
// web:<hash> identity the turn's citations use, so a re-read updates the durable copy
// instead of adding a new source. Only the owner's own readings reach this.
func (p *knowledgePersistence) persistWebReading(ctx context.Context, event core.Event, document chat.WebDocument) {
	if !p.available(event) {
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
			"tags":       []string{"origin:" + string(event.Source), "via:chat-link"},
		},
		Provenance: ownerReadingProvenance(event),
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
	if !p.available(event) || len(documents) == 0 {
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

// ownerReadingProvenance records who stored the reading and which message carried it:
// the owner, on the channel, at the exact Discord message when the turn has one.
func ownerReadingProvenance(event core.Event) *knowledgedomain.Provenance {
	provenance := knowledgedomain.Provenance{
		Origin:     knowledgedomain.OriginOwner,
		Channel:    event.Channel.ID,
		ProposedAt: time.Now().UTC().Format(rfc3339Nano),
	}
	if event.Source == core.SourceDiscord {
		if messageID := resultString(event.Context, "message_id"); messageID != "" {
			provenance.Permalink = fmt.Sprintf("https://discord.com/channels/%s/%s/%s",
				resultString(event.Context, "guild_id"), event.Channel.ID, messageID)
		}
	}
	return &provenance
}

// viaTags adds one provenance tag to a proposal or ingest payload, keeping any tags
// the caller already set.
func viaTags(payload map[string]any, via string) []string {
	tags, _ := payload["tags"].([]string)
	return append(tags, via)
}
