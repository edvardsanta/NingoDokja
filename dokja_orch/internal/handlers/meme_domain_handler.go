package handlers

import (
	"context"
	"fmt"
	memedomain "read_books/dokja_domain/dokja_meme"
	"read_books/internal/core"
	"read_books/internal/logger"
)

type MemeDomainHandler struct {
	domain        *memedomain.Domain
	hashtags      MemeHashtagActions
	hashtagNotes  HashtagExperienceNotes
	tagTask       HashtagSuggestionTask
	tagExperience *HashtagExperience
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

type MemeHashtagActions interface {
	Handle(ctx context.Context, action string, payload map[string]any) (map[string]any, error)
}

func (h *MemeDomainHandler) WithHashtagWorkflow(workflow MemeHashtagActions) *MemeDomainHandler {
	h.hashtags = workflow
	return h
}

// WithTagEvaluation freezes a shadow suggestion before an operator's label enters Knowledge.
// Successful tagging then records and judges it; optional memory never determines the label.
func (h *MemeDomainHandler) WithTagEvaluation(task HashtagSuggestionTask, experience *HashtagExperience) *MemeDomainHandler {
	h.tagTask, h.tagExperience = task, experience
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

	var attempt HashtagAttempt
	evaluate := workflow.Action == memedomain.ActionTagMemeHashtag && h.tagTask != nil && h.tagExperience.active()
	if evaluate {
		attempt = h.tagTask.Suggest(ctx, event, resultString(event.Payload, "url"))
	}

	var response map[string]any
	var err error
	if isMemeHashtagAction(workflow.Action) {
		if h.hashtags == nil {
			return nil, fmt.Errorf("meme hashtag workflow is not configured")
		}
		payload := event.Payload
		// Reuse the text already read for evaluation rather than performing OCR twice.
		if evaluate && resultString(payload, "text") == "" {
			if text := resultString(attempt.Task.Outputs["read"], "text"); text != "" {
				payload = make(map[string]any, len(event.Payload)+1)
				for key, value := range event.Payload {
					payload[key] = value
				}
				payload["text"] = text
			}
		}
		response, err = h.hashtags.Handle(ctx, workflow.Action, payload)
	} else {
		response, err = h.domain.Handle(ctx, memedomain.Request{
			Action: workflow.Action,
			Event: memedomain.Event{
				ID:      event.EventID,
				Source:  string(event.Source),
				Type:    event.Type,
				Payload: event.Payload,
				Context: event.Context,
			},
		})
	}
	if err != nil {
		return nil, err
	}

	if (h.hashtagNotes != nil || evaluate) && workflow.Action == memedomain.ActionTagMemeHashtag {
		// The service returns the hashtag normalized ("Name" becomes "#Name"), which is how a
		// suggestion was stored.
		url := firstNonEmptyString(resultString(response, "source_url"), resultString(event.Payload, "url"))
		observed := firstNonEmptyString(resultString(response, "hashtag"), resultString(event.Payload, "hashtag"))
		if evaluate {
			h.tagExperience.TagEvaluated(url, attempt, observed)
		} else {
			h.hashtagNotes.Tagged(url, observed)
		}
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

func isMemeHashtagAction(action string) bool {
	switch action {
	case memedomain.ActionTagMemeHashtag, memedomain.ActionSuggestMemeHashtag,
		memedomain.ActionListMemeHashtags, memedomain.ActionUntagMemeHashtag:
		return true
	default:
		return false
	}
}
