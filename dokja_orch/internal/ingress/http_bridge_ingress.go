package ingress

import (
	"fmt"
	"os"
	"read_books/internal/core"
	"read_books/internal/logger"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
)

const defaultHTTPBridgeEndpoint = ":8091"

type HTTPBridgeIngress struct {
	service  *core.Service
	endpoint string
	app      *fiber.App
}

type BridgeRequest struct {
	EventType string `json:"event_type"`
	Payload   struct {
		Content    string `json:"content"`
		Limit      *int   `json:"limit"`
		MaxItems   *int   `json:"maxItemsPerScraper"`
		UserID     string `json:"userId"`
		ChannelID  string `json:"channelId"`
		MessageID  string `json:"messageId"`
		GuildID    string `json:"guildId"`
		SessionKey string `json:"sessionKey"`
		StartChat  bool   `json:"startSession"`
		AccountID  string `json:"accountId"`
	} `json:"payload"`
}

type BridgeResponse struct {
	Status string `json:"status"`
	Reply  string `json:"reply,omitempty"`
	Error  string `json:"error,omitempty"`
}

func NewHTTPBridgeIngress(service *core.Service, endpoint string) *HTTPBridgeIngress {
	return &HTTPBridgeIngress{
		service:  service,
		endpoint: firstNonEmpty(endpoint, os.Getenv("VA_HTTP_ENDPOINT"), defaultHTTPBridgeEndpoint),
	}
}

func (i *HTTPBridgeIngress) Name() string {
	return "va-orchestrator-http-bridge"
}

func (i *HTTPBridgeIngress) Start() error {
	if i == nil {
		return fmt.Errorf("http bridge ingress is nil")
	}
	if i.service == nil {
		return fmt.Errorf("orchestrator service is not configured")
	}

	i.app = fiber.New()
	i.app.Post("/orchestrator", i.handle)

	go func() {
		logger.Info(fmt.Sprintf("http bridge ingress listening endpoint=%s", i.endpoint))
		if err := i.app.Listen(i.endpoint); err != nil {
			logger.Error("http bridge ingress stopped unexpectedly", err)
		}
	}()

	return nil
}

func (i *HTTPBridgeIngress) Stop() error {
	if i == nil || i.app == nil {
		return nil
	}

	err := i.app.Shutdown()
	i.app = nil
	return err
}

func (i *HTTPBridgeIngress) handle(c *fiber.Ctx) error {
	var bridgeRequest BridgeRequest
	if err := c.BodyParser(&bridgeRequest); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(BridgeResponse{
			Status: "error",
			Error:  fmt.Sprintf("decode request: %v", err),
		})
	}

	logger.Info(fmt.Sprintf(
		"http bridge received event_type=%s user=%s channel=%s message_id=%s content_chars=%d",
		bridgeRequest.EventType,
		bridgeRequest.Payload.UserID,
		bridgeRequest.Payload.ChannelID,
		bridgeRequest.Payload.MessageID,
		len(strings.TrimSpace(bridgeRequest.Payload.Content)),
	))

	event := bridgeEventFromRequest(bridgeRequest)

	result, err := i.service.ProcessWithResult(c.UserContext(), event)
	if err != nil {
		logger.Error(fmt.Sprintf("http bridge failed type=%s user=%s", bridgeRequest.EventType, bridgeRequest.Payload.UserID), err)
		return c.Status(fiber.StatusBadGateway).JSON(BridgeResponse{
			Status: "error",
			Error:  err.Error(),
		})
	}

	reply := extractBridgeReply(result)
	logger.Info(fmt.Sprintf(
		"http bridge completed event_id=%s workflow=%s reply_chars=%d",
		result.Event.EventID,
		result.Workflow,
		len(reply),
	))
	return c.Status(fiber.StatusOK).JSON(BridgeResponse{
		Status: "ok",
		Reply:  reply,
	})
}

func extractBridgeReply(result core.ProcessResult) string {
	chatResult, ok := result.Result[string(core.DomainChat)].(map[string]any)
	if ok {
		reply, _ := chatResult["reply"].(string)
		return strings.TrimSpace(reply)
	}

	systemResult, ok := result.Result[string(core.DomainSystem)].(map[string]any)
	if ok {
		return formatSystemReply(systemResult)
	}

	memeResult, ok := result.Result[string(core.DomainMeme)].(map[string]any)
	if ok {
		return formatMemeReply(memeResult)
	}

	return ""
}

func bridgeEventFromRequest(request BridgeRequest) core.Event {
	content := strings.TrimSpace(request.Payload.Content)
	eventType, payload := mapBridgeRequestToEvent(request, content)
	context := map[string]any{
		"interface":   "discord",
		"message_id":  strings.TrimSpace(request.Payload.MessageID),
		"guild_id":    strings.TrimSpace(request.Payload.GuildID),
		"session_key": strings.TrimSpace(request.Payload.SessionKey),
		"force_new_session": request.Payload.StartChat,
		"account_id":  strings.TrimSpace(request.Payload.AccountID),
		"event_type":  strings.TrimSpace(request.EventType),
	}
	if eventType == "message.created" {
		context["content"] = content
	}

	return core.Event{
		Source: core.SourceDiscord,
		Type:   eventType,
		User: core.EventUser{
			ID:   strings.TrimSpace(request.Payload.UserID),
			Name: strings.TrimSpace(request.Payload.UserID),
		},
		Channel: core.EventChannel{
			ID: strings.TrimSpace(request.Payload.ChannelID),
		},
		Payload: payload,
		Context: context,
	}
}

func mapBridgeRequestToEvent(request BridgeRequest, content string) (string, map[string]any) {
	switch strings.TrimSpace(request.EventType) {
	case "message.created", "chat_message", "":
		return mapDiscordContentToEvent(content)
	case "meme.fetch":
		payload := map[string]any{}
		if request.Payload.Limit != nil && *request.Payload.Limit > 0 {
			payload["limit"] = *request.Payload.Limit
		}
		return "meme.fetch", payload
	case "meme.status":
		return "meme.status", map[string]any{}
	case "meme.pool.refresh":
		payload := map[string]any{}
		if request.Payload.MaxItems != nil && *request.Payload.MaxItems > 0 {
			payload["max_items_per_scraper"] = *request.Payload.MaxItems
		}
		return "meme.pool.refresh", payload
	case "ningo.status":
		return "ningo.status", map[string]any{}
	default:
		return strings.TrimSpace(request.EventType), map[string]any{"content": content}
	}
}

func mapDiscordContentToEvent(content string) (string, map[string]any) {
	fields := strings.Fields(strings.TrimLeft(strings.TrimSpace(content), "/!"))
	if len(fields) == 0 {
		return "message.created", map[string]any{"content": strings.TrimSpace(content)}
	}

	if !strings.EqualFold(fields[0], "meme") {
		return "message.created", map[string]any{"content": strings.TrimSpace(content)}
	}

	if len(fields) == 1 {
		return "meme.fetch", map[string]any{}
	}

	switch strings.ToLower(fields[1]) {
	case "fetch":
		payload := map[string]any{}
		if len(fields) > 2 {
			if limit, err := strconv.Atoi(fields[2]); err == nil && limit > 0 {
				payload["limit"] = limit
			}
		}
		return "meme.fetch", payload
	case "status":
		return "meme.status", map[string]any{}
	case "refresh":
		payload := map[string]any{}
		if len(fields) > 2 {
			if maxItems, err := strconv.Atoi(fields[2]); err == nil && maxItems > 0 {
				payload["max_items_per_scraper"] = maxItems
			}
		}
		return "meme.pool.refresh", payload
	default:
		return "message.created", map[string]any{"content": strings.TrimSpace(content)}
	}
}

func formatMemeReply(result map[string]any) string {
	memes, _ := result["memes"].([]any)
	if len(memes) > 0 {
		lines := make([]string, 0, len(memes))
		for _, raw := range memes {
			meme, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			title := strings.TrimSpace(stringValue(meme["title"]))
			url := strings.TrimSpace(stringValue(meme["url"]))
			switch {
			case title != "" && url != "":
				lines = append(lines, fmt.Sprintf("%s\n%s", title, url))
			case url != "":
				lines = append(lines, url)
			case title != "":
				lines = append(lines, title)
			}
		}
		if len(lines) > 0 {
			return strings.Join(lines, "\n\n")
		}
	}

	if unsentCount, ok := intFromValue(result["unsent_count"]); ok {
		return fmt.Sprintf("meme status: %d unsent", unsentCount)
	}
	if count, ok := intFromValue(result["count"]); ok {
		return fmt.Sprintf("meme fetch completed: %d items", count)
	}
	if status := strings.TrimSpace(stringValue(result["status"])); status != "" {
		return status
	}

	return "meme request completed"
}

func formatSystemReply(result map[string]any) string {
	status := strings.TrimSpace(stringValue(result["status"]))
	if status == "" {
		status = "unknown"
	}

	lines := []string{fmt.Sprintf("ningo status: %s", status)}
	services, _ := result["services"].(map[string]any)

	if chatAI, ok := services["chat_ai"].(map[string]any); ok {
		line := fmt.Sprintf("chat ai: %s", strings.TrimSpace(stringValue(chatAI["status"])))
		if err := strings.TrimSpace(stringValue(chatAI["error"])); err != "" {
			line = fmt.Sprintf("%s (%s)", line, err)
		}
		lines = append(lines, line)
	}

	if meme, ok := services["meme"].(map[string]any); ok {
		line := fmt.Sprintf("meme: %s", strings.TrimSpace(stringValue(meme["status"])))
		if unsentCount, ok := intFromValue(meme["unsent_count"]); ok {
			line = fmt.Sprintf("%s (%d unsent)", line, unsentCount)
		}
		if err := strings.TrimSpace(stringValue(meme["error"])); err != "" {
			line = fmt.Sprintf("%s (%s)", line, err)
		}
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return fmt.Sprint(value)
	}
}

func intFromValue(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int8:
		return int(typed), true
	case int16:
		return int(typed), true
	case int32:
		return int(typed), true
	case int64:
		return int(typed), true
	case float32:
		return int(typed), true
	case float64:
		return int(typed), true
	default:
		return 0, false
	}
}
