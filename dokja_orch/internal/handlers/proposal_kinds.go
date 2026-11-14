package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"

	chat "read_books/dokja_domain/dokja_chat"
	digest "read_books/dokja_domain/dokja_digest"
	knowledgedomain "read_books/dokja_domain/dokja_knowledge"
)

// webArticleIdentity is the same stable identity chat citations use for a web
// document, so what a proposal stores and what a turn cites address one item.
func webArticleIdentity(rawURL string) string {
	sum := sha256.Sum256([]byte(rawURL))
	return "web:" + hex.EncodeToString(sum[:12])
}

// webArticleKindRules is what a link proposal supplies: the proposal keeps only
// the link, validated, and approval resolves it through the web reader.
func webArticleKindRules() knowledgedomain.ProposalKindRules {
	return knowledgedomain.ProposalKindRules{
		Kind: "web_article",
		Validate: func(payload map[string]any) (map[string]any, error) {
			raw := strings.TrimSpace(payloadString(payload, "url"))
			address, err := url.Parse(raw)
			if err != nil || address.Hostname() == "" || address.User != nil || (address.Scheme != "http" && address.Scheme != "https") {
				return nil, fmt.Errorf("a proposal link must be an http(s) url")
			}
			return map[string]any{"url": raw}, nil
		},
		Identity: func(payload map[string]any) string {
			return webArticleIdentity(payloadString(payload, "url"))
		},
	}
}

// documentKindRules is what a file proposal supplies: the proposal keeps the
// file as it was sent, and approval re-reads it through the reading host.
func documentKindRules() knowledgedomain.ProposalKindRules {
	return knowledgedomain.ProposalKindRules{
		Kind: "document",
		Validate: func(payload map[string]any) (map[string]any, error) {
			filename := strings.TrimSpace(payloadString(payload, "filename"))
			if filename == "" || len([]rune(filename)) > 300 {
				return nil, fmt.Errorf("a document proposal needs a filename")
			}
			encoded := strings.TrimSpace(payloadString(payload, "content_b64"))
			data, err := base64.StdEncoding.Strict().DecodeString(encoded)
			if err != nil || len(data) == 0 || len(data) > chat.MaxIncomingDocumentBytes {
				return nil, fmt.Errorf("a document proposal needs a bounded base64 file")
			}
			canonical := map[string]any{"filename": filename, "content_b64": encoded}
			if format := strings.TrimSpace(payloadString(payload, "format")); format != "" {
				if len([]rune(format)) > 50 {
					return nil, fmt.Errorf("format is too long")
				}
				canonical["format"] = format
			}
			return canonical, nil
		},
		Identity: func(payload map[string]any) string {
			return knowledgedomain.FileSourceID(payloadString(payload, "filename"))
		},
	}
}

// workflowFor returns the decision flow of one kind. The built-in kinds run on
// this handler's own clients; other kinds register their flow with
// RegisterProposalKindWorkflow.
func (h *KnowledgeProposalsHandler) workflowFor(kind string) (proposalKindWorkflow, bool) {
	switch kind {
	case "web_article":
		return proposalKindWorkflow{Approve: h.approveWebArticle, Current: h.currentWebArticle}, true
	case "document":
		return proposalKindWorkflow{Approve: h.approveDocument, Current: h.currentDocument}, true
	default:
		return proposalWorkflowFor(kind)
	}
}

// approveWebArticle resolves the link at approval time, then stores the article
// with its provenance. A reader refusal keeps the proposal pending with its
// fixed reason; the link alone never enters retrieval before approval.
func (h *KnowledgeProposalsHandler) approveWebArticle(ctx context.Context, proposal knowledgedomain.Proposal, edit map[string]any) (map[string]any, error) {
	if len(edit) > 0 {
		return map[string]any{"refused": "uneditable"}, nil
	}
	raw := payloadString(proposal.Payload, "url")
	current := h.currentStoredRecord(ctx, "article", webArticleIdentity(raw))
	readCtx, cancel := context.WithTimeout(ctx, webReadBudget)
	defer cancel()
	result, err := digest.New(h.web).ReadWebLink(readCtx, raw)
	if err != nil {
		return map[string]any{"refused": digest.WebFailureReason(err)}, nil
	}
	document, err := decodeWebDocument(result)
	if err != nil {
		return map[string]any{"refused": "unreadable"}, nil
	}
	var body strings.Builder
	for _, passage := range document.Passages {
		if strings.TrimSpace(passage.Text) == "" {
			continue
		}
		if body.Len() > 0 {
			body.WriteString("\n\n")
		}
		body.WriteString(passage.Text)
	}
	if strings.TrimSpace(body.String()) == "" {
		return map[string]any{"refused": "unreadable"}, nil
	}
	provenance := provenanceFor(proposal)
	_, err = h.knowledge.StoreReading(readCtx, knowledgedomain.Reading{
		Filename: raw,
		Documents: []knowledgedomain.ReadDocument{{
			Title: document.Title, Text: body.String(), SourceRef: raw,
		}},
		Payload: map[string]any{
			"source_id": webArticleIdentity(raw), "source_ref": raw, "kind": "article",
		},
		Provenance: &provenance,
	})
	if err != nil {
		return nil, err
	}
	outcome := map[string]any{"stored": 1}
	if origin, found := current["origin"]; found {
		outcome["replaced_origin"] = origin
	}
	return outcome, nil
}

func (h *KnowledgeProposalsHandler) currentWebArticle(ctx context.Context, payload map[string]any) (map[string]any, bool) {
	if current := h.currentStoredRecord(ctx, "article", webArticleIdentity(payloadString(payload, "url"))); current != nil {
		return current, true
	}
	return nil, false
}

// approveDocument re-reads the file through the reading host and stores its
// documents with their provenance, like knowledge.read does.
func (h *KnowledgeProposalsHandler) approveDocument(ctx context.Context, proposal knowledgedomain.Proposal, edit map[string]any) (map[string]any, error) {
	if len(edit) > 0 {
		return map[string]any{"refused": "uneditable"}, nil
	}
	filename := payloadString(proposal.Payload, "filename")
	encoded := payloadString(proposal.Payload, "content_b64")
	current := h.currentStoredRecord(ctx, "", knowledgedomain.FileSourceID(filename))
	extracted, err := h.reader.Extract(ctx, filename, encoded)
	if err != nil {
		return map[string]any{"refused": "document_read_failed"}, nil
	}
	documents := readDocuments(extracted["documents"])
	if len(documents) == 0 {
		return map[string]any{"refused": "document_unreadable"}, nil
	}
	provenance := provenanceFor(proposal)
	stored, err := h.knowledge.StoreReading(ctx, knowledgedomain.Reading{
		Filename: filename, ContentB64: encoded,
		Format: resultString(extracted, "format"), Reader: resultString(extracted, "plugin"), ReaderVersion: resultString(extracted, "version"),
		Documents: documents, Provenance: &provenance,
	})
	if err != nil {
		return nil, err
	}
	outcome := map[string]any{"stored": len(stored)}
	if origin, found := current["origin"]; found {
		outcome["replaced_origin"] = origin
	}
	return outcome, nil
}

func (h *KnowledgeProposalsHandler) currentDocument(ctx context.Context, payload map[string]any) (map[string]any, bool) {
	if current := h.currentStoredRecord(ctx, "", knowledgedomain.FileSourceID(payloadString(payload, "filename"))); current != nil {
		return current, true
	}
	return nil, false
}

// provenanceFor turns an approved proposal into the record's provenance. Origin
// precedence is the owner's problem at a glance: an approval of an item the
// owner already stored is the owner's explicit decision to replace it, and the
// approval answer says what it replaced.
func provenanceFor(proposal knowledgedomain.Proposal) knowledgedomain.Provenance {
	return knowledgedomain.Provenance{
		Origin:     knowledgedomain.OriginGroup,
		ProposedBy: proposal.ProposedBy, SentBy: proposal.SentBy,
		Channel: proposal.Source.ChannelID, Permalink: proposal.Source.Permalink,
		ProposedAt: proposal.ProposedAt.Format(rfc3339Nano),
	}
}

const rfc3339Nano = "2006-01-02T15:04:05.999999999Z07:00"

// currentStoredRecord finds the stored record one decision would affect, with
// where it came from; absent means nothing is stored yet.
func (h *KnowledgeProposalsHandler) currentStoredRecord(ctx context.Context, kind, sourceID string) map[string]any {
	payload := map[string]any{"include_text": false, "limit": 200}
	if kind != "" {
		payload["kind"] = kind
	}
	answer, err := h.knowledge.Handle(ctx, knowledgedomain.Request{
		Action: knowledgedomain.ActionListKnowledge,
		Event:  knowledgedomain.Event{Source: "orchestrator", Type: "knowledge.list", Payload: payload},
	})
	if err != nil {
		return nil
	}
	documents, _ := answer["documents"].([]any)
	for _, raw := range documents {
		document, _ := raw.(map[string]any)
		if payloadString(document, "source_id") != sourceID {
			continue
		}
		metadata, _ := document["metadata"].(map[string]any)
		return map[string]any{"origin": string(knowledgedomain.MetadataOrigin(metadata)), "title": payloadString(document, "title")}
	}
	return nil
}
