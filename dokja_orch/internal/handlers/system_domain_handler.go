package handlers

import (
	"context"
	"fmt"
	"read_books/internal/core"
	"read_books/internal/logger"
)

type ChatAIHealthChecker interface {
	Health(ctx context.Context) error
}

type MemeStatusReader interface {
	Status(ctx context.Context) (map[string]any, error)
}

type MemeFetcher interface {
	Fetch(ctx context.Context, limit *int) (map[string]any, error)
}

type DiscordMessageDeliverer interface {
	Deliver(ctx context.Context, channelID, content, attachmentURL string) error
}

type SystemDomainHandler struct {
	chatAI          ChatAIHealthChecker
	memeStatus      MemeStatusReader
	memeFetch       MemeFetcher
	discord         DiscordMessageDeliverer
	deliveryChannel string
}

func NewSystemDomainHandler(
	chatAI ChatAIHealthChecker,
	memeStatus MemeStatusReader,
	memeFetch MemeFetcher,
	discord DiscordMessageDeliverer,
	deliveryChannel string,
) *SystemDomainHandler {
	return &SystemDomainHandler{
		chatAI:          chatAI,
		memeStatus:      memeStatus,
		memeFetch:       memeFetch,
		discord:         discord,
		deliveryChannel: deliveryChannel,
	}
}

func (h *SystemDomainHandler) Domain() core.Domain {
	return core.DomainSystem
}

func (h *SystemDomainHandler) Handle(ctx context.Context, event core.Event, workflow core.WorkflowStep) (map[string]any, error) {
	if h == nil {
		return nil, fmt.Errorf("system domain handler is nil")
	}

	logger.Info(fmt.Sprintf(
		"system handler received event_id=%s type=%s action=%s",
		event.EventID,
		event.Type,
		workflow.Action,
	))

	if event.Type == "meme.dispatch.scheduled" {
		return h.handleScheduledMemeDispatch(ctx, event, workflow)
	}

	services := map[string]any{}
	overallStatus := "ok"

	if h.chatAI != nil {
		if err := h.chatAI.Health(ctx); err != nil {
			overallStatus = "degraded"
			services["chat_ai"] = map[string]any{
				"status": "error",
				"error":  err.Error(),
			}
		} else {
			services["chat_ai"] = map[string]any{
				"status": "ok",
			}
		}
	}

	if h.memeStatus != nil {
		result, err := h.memeStatus.Status(ctx)
		if err != nil {
			overallStatus = "degraded"
			services["meme"] = map[string]any{
				"status": "error",
				"error":  err.Error(),
			}
		} else {
			entry := map[string]any{
				"status": firstNonEmptyString(resultString(result, "status"), "ok"),
			}
			if unsentCount, ok := result["unsent_count"]; ok {
				entry["unsent_count"] = unsentCount
			}
			services["meme"] = entry
		}
	}

	response := map[string]any{
		"action":   workflow.Action,
		"status":   overallStatus,
		"services": services,
	}
	logger.Info(fmt.Sprintf(
		"system handler completed event_id=%s type=%s action=%s service_count=%d status=%s",
		event.EventID,
		event.Type,
		workflow.Action,
		len(services),
		overallStatus,
	))
	return response, nil
}

func (h *SystemDomainHandler) handleScheduledMemeDispatch(ctx context.Context, event core.Event, workflow core.WorkflowStep) (map[string]any, error) {
	if h.memeFetch == nil {
		return nil, fmt.Errorf("meme fetch client is not configured")
	}
	if h.discord == nil {
		return nil, fmt.Errorf("discord deliverer is not configured")
	}
	if h.deliveryChannel == "" {
		return nil, fmt.Errorf("scheduled meme delivery channel is not configured")
	}

	limit := intPointer(event.Payload["limit"])
	result, err := h.memeFetch.Fetch(ctx, limit)
	if err != nil {
		return nil, err
	}

	delivered := 0
	for _, meme := range memesFromResult(result) {
		content, attachmentURL := formatScheduledMeme(meme)
		if content == "" {
			continue
		}
		if err := h.discord.Deliver(ctx, h.deliveryChannel, content, attachmentURL); err != nil {
			return nil, err
		}
		delivered++
	}

	response := map[string]any{
		"action":          workflow.Action,
		"channel_id":      h.deliveryChannel,
		"delivered_count": delivered,
		"count":           delivered,
	}
	logger.Info(fmt.Sprintf(
		"system handler completed event_id=%s type=%s action=%s delivered_count=%d channel_id=%s",
		event.EventID,
		event.Type,
		workflow.Action,
		delivered,
		h.deliveryChannel,
	))
	return response, nil
}

func resultString(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, _ := values[key].(string)
	return value
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func intPointer(value any) *int {
	switch typed := value.(type) {
	case int:
		return &typed
	case float64:
		parsed := int(typed)
		return &parsed
	default:
		return nil
	}
}

func memesFromResult(result map[string]any) []map[string]any {
	raw, _ := result["memes"].([]any)
	memes := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		meme, ok := item.(map[string]any)
		if ok {
			memes = append(memes, meme)
		}
	}
	return memes
}

func formatScheduledMeme(meme map[string]any) (string, string) {
	title := firstNonEmptyString(resultString(meme, "title"))
	url := firstNonEmptyString(resultString(meme, "url"))
	switch {
	case title != "":
		return title, url
	case url != "":
		return "scheduled meme", url
	default:
		return "", ""
	}
}
