package handlers

import (
	"context"
	"fmt"
	memedomain "read_books/dokja_domain/dokja_meme"
	"read_books/internal/core"
	"read_books/internal/logger"
)

type MemeDomainHandler struct {
	domain       *memedomain.Domain
	hashtagNotes HashtagExperienceNotes
}

func NewMemeDomainHandler(service memedomain.Service) *MemeDomainHandler {
	return &MemeDomainHandler{domain: memedomain.New(service)}
}

// WithHashtagExperience notes every hashtag the operator gives a meme, so the experience memory can
// tell whether it was the one the bot had suggested.
func (h *MemeDomainHandler) WithHashtagExperience(notes HashtagExperienceNotes) *MemeDomainHandler {
	h.hashtagNotes = notes
	return h
}

func (h *MemeDomainHandler) Domain() core.Domain {
	return core.DomainMeme
}

func (h *MemeDomainHandler) Handle(ctx context.Context, event core.Event, workflow core.WorkflowStep) (map[string]any, error) {
	if h == nil || h.domain == nil {
		return nil, fmt.Errorf("meme domain is not configured")
	}
	logger.Info(fmt.Sprintf(
		"meme handler received event_id=%s type=%s action=%s source=%s",
		event.EventID,
		event.Type,
		workflow.Action,
		event.Source,
	))

	response, err := h.domain.Handle(ctx, memedomain.Request{
		Action: workflow.Action,
		Event: memedomain.Event{
			ID:      event.EventID,
			Source:  string(event.Source),
			Type:    event.Type,
			Payload: event.Payload,
			Context: event.Context,
		},
	})
	if err != nil {
		return nil, err
	}

	if h.hashtagNotes != nil && workflow.Action == memedomain.ActionTagMemeHashtag {
		// The service returns the hashtag normalized ("Name" becomes "#Name"), which is how a
		// suggestion was stored.
		h.hashtagNotes.Tagged(
			firstNonEmptyString(resultString(response, "source_url"), resultString(event.Payload, "url")),
			firstNonEmptyString(resultString(response, "hashtag"), resultString(event.Payload, "hashtag")),
		)
	}

	logger.Info(fmt.Sprintf(
		"meme handler completed event_id=%s type=%s action=%s result_keys=%d",
		event.EventID,
		event.Type,
		workflow.Action,
		len(response),
	))
	return response, nil
}
