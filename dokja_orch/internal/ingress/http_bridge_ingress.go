package ingress

import (
	"bufio"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"os"
	chat "read_books/dokja_domain/dokja_chat"
	"read_books/internal/core"
	"read_books/internal/logger"
	"read_books/internal/ownerconfig"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
)

const defaultHTTPBridgeEndpoint = ":8091"

type HTTPBridgeIngress struct {
	service        *core.Service
	endpoint       string
	token          string
	discordOwnerID string
	ownerProfile   chat.OwnerProfile
	app            *fiber.App
}

type BridgeRequest struct {
	EventType string `json:"event_type"`
	// Interface names the interface the request came from. Empty means Discord, which is
	// what every existing client sends; only the values in bridgeInterfaces are accepted.
	Interface string        `json:"interface"`
	Payload   BridgePayload `json:"payload"`
}

type BridgePayload struct {
	VoiceSessionID   string           `json:"voiceSessionId"`
	VoiceStartedBy   string           `json:"voiceStartedBy"`
	Realtime         map[string]any   `json:"realtime"`
	Voice            map[string]any   `json:"voice"`
	VoiceTurns       []chat.VoiceTurn `json:"voiceTurns"`
	VoiceChannelID   string           `json:"voiceChannelId"`
	Content          string           `json:"content"`
	ReplyToContent   string           `json:"replyToContent"`
	MentionedUserIDs []string         `json:"mentionedUserIds"`
	Limit            *int             `json:"limit"`
	MaxItems         *int             `json:"maxItemsPerScraper"`
	UserID           string           `json:"userId"`
	UserName         string           `json:"userName"`
	ChannelID        string           `json:"channelId"`
	MessageID        string           `json:"messageId"`
	GuildID          string           `json:"guildId"`
	SessionKey       string           `json:"sessionKey"`
	StartChat        bool             `json:"startSession"`
	AccountID        string           `json:"accountId"`
	EntryKind        string           `json:"entryKind"`
	Direct           *bool            `json:"isDirectMessage"`
	Mentioned        *bool            `json:"mentionsBot"`
	ChatKind         string           `json:"chatKind"`
	SelfChat         *bool            `json:"isSelfChat"`
	OwnAccount       *bool            `json:"isOwnAccount"`
	Numbers          []string         `json:"senderNumbers"`
}

type BridgeResponse struct {
	Realtime    map[string]any    `json:"realtime,omitempty"`
	Voice       map[string]any    `json:"voice,omitempty"`
	Status      string            `json:"status"`
	Reply       string            `json:"reply,omitempty"`
	Notice      string            `json:"notice,omitempty"`
	Error       string            `json:"error,omitempty"`
	Attachments []chat.Attachment `json:"attachments,omitempty"`
	// Music is a server's queue after a music command, for the interface that plays it.
	Music map[string]any `json:"music,omitempty"`
}

func NewHTTPBridgeIngress(service *core.Service, endpoint string) *HTTPBridgeIngress {
	return &HTTPBridgeIngress{
		service:  service,
		endpoint: firstNonEmpty(endpoint, os.Getenv("VA_HTTP_ENDPOINT"), defaultHTTPBridgeEndpoint),
		token:    os.Getenv("DOKJA_GATEWAY_INGRESS_TOKEN"),
	}
}

// WithOwnerConfig supplies the identity loaded from the private JSON file.
func (i *HTTPBridgeIngress) WithOwnerConfig(config ownerconfig.Config) *HTTPBridgeIngress {
	i.discordOwnerID = config.Owner.Accounts.Discord.UserID
	i.ownerProfile = config.Owner.Profile
	return i
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
	if i.token == "" {
		return fmt.Errorf("DOKJA_GATEWAY_INGRESS_TOKEN is required")
	}

	i.app = fiber.New()
	i.app.Post("/orchestrator", i.handle)
	i.app.Post("/orchestrator/chat/stream", i.handleChatStream)
	i.app.Post("/orchestrator/voice/chat/stream", i.handleVoiceChatStream)

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

	if len(bridgeRequest.Payload.VoiceTurns) > 0 {
		return c.Status(fiber.StatusBadRequest).JSON(BridgeResponse{Status: "error", Error: "voice batches require the voice stream endpoint"})
	}
	if _, source, ok := bridgeInterface(bridgeRequest.Interface); !ok {
		return c.Status(fiber.StatusBadRequest).JSON(BridgeResponse{
			Status: "error",
			Error:  "unknown interface",
		})
	} else if chatOnlySources[source] && !chatEventTypeAllowed(bridgeRequest.EventType) {
		return c.Status(fiber.StatusBadRequest).JSON(BridgeResponse{
			Status: "error",
			Error:  "event type is not allowed for this interface",
		})
	}
	if status := i.authorize(c); status != fiber.StatusOK {
		return c.SendStatus(status)
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
	if event.Source == core.SourceDiscord && i.discordOwnerID != "" {
		event.Context["owner_configured"] = true
		if event.User.ID == i.discordOwnerID {
			event.Context["is_owner"] = true
			event.Context["owner_profile"] = i.ownerProfile
		}
		for _, mentionedID := range bridgeRequest.Payload.MentionedUserIDs[:min(len(bridgeRequest.Payload.MentionedUserIDs), 20)] {
			if mentionedID == i.discordOwnerID {
				event.Context["owner_mentioned"] = true
				break
			}
		}
	}

	result, err := i.service.ProcessWithResult(c.UserContext(), event)
	if err != nil {
		logger.Error(fmt.Sprintf("http bridge failed type=%s user=%s", bridgeRequest.EventType, bridgeRequest.Payload.UserID), err)
		return c.Status(fiber.StatusBadGateway).JSON(BridgeResponse{
			Status: "error",
			Error:  err.Error(),
		})
	}

	reply := extractBridgeReply(result)
	if event.Source == core.SourceDiscord {
		reply = sanitizeDiscordOwnerID(reply, i.discordOwnerID)
	}
	notice := extractBridgeNotice(result)
	logger.Info(fmt.Sprintf(
		"http bridge completed event_id=%s workflow=%s reply_chars=%d",
		result.Event.EventID,
		result.Workflow,
		len(reply),
	))
	return c.Status(fiber.StatusOK).JSON(BridgeResponse{
		Status:      "ok",
		Reply:       reply,
		Notice:      notice,
		Attachments: extractChatAttachments(result),
		Music:       extractBridgeMusic(result),
		Voice:       extractBridgeVoice(result),
		Realtime:    extractBridgeRealtime(result),
	})
}

func extractBridgeRealtime(result core.ProcessResult) map[string]any {
	domain, _ := result.Result[string(core.DomainConversation)].(map[string]any)
	if accepted, _ := domain["accepted"].(bool); accepted {
		return map[string]any{"accepted": true}
	}
	return nil
}

func extractBridgeVoice(result core.ProcessResult) map[string]any {
	domain, _ := result.Result[string(core.DomainCommunications)].(map[string]any)
	voice, _ := domain["voice"].(map[string]any)
	return voice
}

func extractChatAttachments(result core.ProcessResult) []chat.Attachment {
	chatResult, _ := result.Result[string(core.DomainChat)].(map[string]any)
	attachments, _ := chatResult["attachments"].([]chat.Attachment)
	return attachments
}

// extractBridgeMusic is the queue a music command left, or nothing for any other event.
func extractBridgeMusic(result core.ProcessResult) map[string]any {
	if !strings.HasPrefix(strings.TrimSpace(result.Event.Type), "music.") {
		return nil
	}
	communicationsResult, _ := result.Result[string(core.DomainCommunications)].(map[string]any)
	music, _ := communicationsResult["music"].(map[string]any)
	return music
}

// authorize checks the bearer token every bridge route requires; it answers the status to send when
// the request may not go on.
func (i *HTTPBridgeIngress) authorize(c *fiber.Ctx) int {
	if i.token == "" {
		return fiber.StatusServiceUnavailable
	}
	authorization := c.Get("Authorization")
	if !strings.HasPrefix(authorization, "Bearer ") {
		return fiber.StatusUnauthorized
	}
	provided := strings.TrimPrefix(authorization, "Bearer ")
	if len(provided) != len(i.token) || subtle.ConstantTimeCompare([]byte(provided), []byte(i.token)) != 1 {
		return fiber.StatusUnauthorized
	}
	return fiber.StatusOK
}

// chatStreamTimeout covers a whole chat turn: the runtime gives a provider 90 seconds.
const chatStreamTimeout = 120 * time.Second

// handleChatStream takes a desktop chat turn, the same envelope the request socket takes, and answers
// it as newline-delimited JSON: {"preview": text so far} while Ningo writes, then the response the
// request socket would give. Only the requester sees the previews; they are never the reply. Anything
// but a desktop message.created is refused.
func (i *HTTPBridgeIngress) handleChatStream(c *fiber.Ctx) error {
	if status := i.authorize(c); status != fiber.StatusOK {
		return c.SendStatus(status)
	}
	var event core.Event
	if err := json.Unmarshal(c.Body(), &event); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(RequestResponse{Status: "error", Error: "decode request"})
	}
	if event.Type != "message.created" || event.Source != core.SourceDesktop {
		return c.Status(fiber.StatusBadRequest).JSON(RequestResponse{Status: "error", Error: "only desktop chat turns are streamed"})
	}
	logger.Info(fmt.Sprintf("http bridge chat stream received event_id=%s user=%s channel=%s", event.EventID, event.User.ID, event.Channel.ID))
	c.Set("Content-Type", "application/x-ndjson")
	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		ctx, cancel := context.WithTimeout(context.Background(), chatStreamTimeout)
		defer cancel()
		var mu sync.Mutex
		// A caller that went away is not written to again, and its turn stops.
		write := func(v any) {
			mu.Lock()
			defer mu.Unlock()
			line, err := json.Marshal(v)
			if err != nil || ctx.Err() != nil {
				return
			}
			if _, err := w.Write(append(line, '\n')); err != nil {
				cancel()
				return
			}
			if err := w.Flush(); err != nil {
				cancel()
			}
		}
		result, err := i.service.ProcessWithResult(core.WithReplyPreview(ctx, func(text string) {
			write(map[string]string{"preview": text})
		}), event)
		if err != nil {
			logger.Error(fmt.Sprintf("http bridge chat stream failed event_id=%s", event.EventID), err)
			write(RequestResponse{Status: "error", Error: err.Error()})
			return
		}
		write(RequestResponse{Status: "ok", Result: buildResponsePayload(result)})
	})
	return nil
}

func extractBridgeReply(result core.ProcessResult) string {
	// The operator switched this off: say so instead of answering with silence.
	if skipped, _ := result.Result["skipped"].(bool); skipped {
		// An interface that is off answers nothing at all.
		if silent, _ := result.Result["silent"].(bool); silent {
			return ""
		}

		reason, _ := result.Result["reason"].(string)
		return fmt.Sprintf("That is switched off right now (%s).", strings.TrimSpace(reason))
	}

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

	if communicationsResult, ok := result.Result[string(core.DomainCommunications)].(map[string]any); ok {
		if music, ok := communicationsResult["music"].(map[string]any); ok {
			return formatMusicReply(strings.TrimSpace(result.Event.Type), music)
		}
	}

	return ""
}

func extractBridgeNotice(result core.ProcessResult) string {
	chatResult, ok := result.Result[string(core.DomainChat)].(map[string]any)
	if !ok {
		return ""
	}
	notice, _ := chatResult["notice"].(string)
	return strings.TrimSpace(notice)
}

func sanitizeDiscordOwnerID(reply, ownerID string) string {
	ownerID = strings.TrimSpace(ownerID)
	if ownerID == "" || reply == "" {
		return reply
	}
	const marker = "\x00DOKJA_OWNER_MENTION\x00"
	reply = strings.ReplaceAll(reply, "<@!"+ownerID+">", marker)
	reply = strings.ReplaceAll(reply, "<@"+ownerID+">", marker)
	reply = strings.ReplaceAll(reply, ownerID, marker)
	return strings.ReplaceAll(reply, marker, "<@"+ownerID+">")
}

// formatMusicReply answers a music command in one plain line, or a short list for the queue.
func formatMusicReply(eventType string, music map[string]any) string {
	if ok, _ := music["ok"].(bool); !ok {
		reason := strings.TrimSpace(stringValue(music["reason"]))
		if reason == "" {
			return "That could not be done."
		}
		return strings.ToUpper(reason[:1]) + reason[1:] + "."
	}
	now, _ := music["now_playing"].(map[string]any)
	waiting, _ := music["waiting"].([]any)
	switch eventType {
	case "music.play":
		added, _ := music["added"].(map[string]any)
		if place, _ := intFromValue(music["place"]); place > 0 {
			return fmt.Sprintf("Queued #%d: %s.", place, trackLine(added))
		}
		return fmt.Sprintf("Now playing: %s.", trackLine(added))
	case "music.skip":
		if now == nil {
			return "Skipped. The queue is empty."
		}
		return fmt.Sprintf("Skipped. Now playing: %s.", trackLine(now))
	case "music.pause":
		return "Paused."
	case "music.resume":
		return "Resumed."
	case "music.stop":
		return "Stopped. The queue is empty."
	case "music.volume":
		volume, _ := intFromValue(music["volume"])
		return fmt.Sprintf("Volume: %d%%.", volume)
	}
	if eventType == "music.history" {
		history, _ := music["history"].([]any)
		lines := make([]string, 0, len(history))
		for _, raw := range history {
			play, _ := raw.(map[string]any)
			lines = append(lines, fmt.Sprintf("%s (%s)", stringValue(play["song"]), stringValue(play["ended"])))
		}
		return strings.Join(lines, "\n")
	}
	if now == nil {
		return "Nothing is playing."
	}
	lines := []string{"Now playing: " + trackLine(now)}
	for i, raw := range waiting {
		track, _ := raw.(map[string]any)
		lines = append(lines, fmt.Sprintf("%d. %s", i+1, trackLine(track)))
	}
	return strings.Join(lines, "\n")
}

func trackLine(track map[string]any) string {
	title := strings.TrimSpace(stringValue(track["title"]))
	if title == "" {
		title = "an untitled track"
	}
	if uploader := strings.TrimSpace(stringValue(track["uploader"])); uploader != "" {
		return title + " – " + uploader
	}
	return title
}

// bridgeInterfaces are the interfaces allowed to use the bridge, with the source their
// events carry. An unknown name is refused rather than mapped to a source of its own.
var bridgeInterfaces = map[string]core.EventSource{
	"discord":  core.SourceDiscord,
	"whatsapp": core.SourceWhatsApp,
}

// chatOnlySources are the interfaces whose text is chat and nothing else. Their text is user
// input in a shared or personal chat, so it must not pick another event type or reach the
// Discord text commands.
var chatOnlySources = map[core.EventSource]bool{
	core.SourceWhatsApp: true,
}

// bridgeInterface resolves the request's interface, treating an empty one as Discord.
func bridgeInterface(name string) (string, core.EventSource, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		name = "discord"
	}
	source, ok := bridgeInterfaces[name]
	return name, source, ok
}

// chatEventTypeAllowed limits the chat-only interfaces to chat messages, so their text cannot
// pick any other event type for the generic branch of mapBridgeRequestToEvent.
func chatEventTypeAllowed(eventType string) bool {
	return strings.TrimSpace(eventType) == "message.created"
}

func bridgeEventFromRequest(request BridgeRequest) core.Event {
	content := strings.TrimSpace(request.Payload.Content)
	interfaceName, source, _ := bridgeInterface(request.Interface)
	eventType, payload := mapBridgeRequestToEvent(request, content)
	if chatOnlySources[source] {
		// WhatsApp text is chat and nothing else. The Discord text commands ("meme fetch",
		// "meme refresh") would let anyone allowed to write to a personal number drive the
		// operator's commands, so they are not parsed here.
		eventType, payload = "message.created", map[string]any{"content": content}
	}
	context := map[string]any{
		"gateway_inbound":   true,
		"interface":         interfaceName,
		"entry_kind":        strings.TrimSpace(request.Payload.EntryKind),
		"message_id":        strings.TrimSpace(request.Payload.MessageID),
		"guild_id":          strings.TrimSpace(request.Payload.GuildID),
		"session_key":       strings.TrimSpace(request.Payload.SessionKey),
		"force_new_session": request.Payload.StartChat,
		"account_id":        strings.TrimSpace(request.Payload.AccountID),
		"event_type":        strings.TrimSpace(request.EventType),
	}
	if source == core.SourceDiscord && len(request.Payload.VoiceTurns) > 0 {
		context["voice_turns"] = request.Payload.VoiceTurns
		context["voice_channel_id"] = strings.TrimSpace(request.Payload.VoiceChannelID)
		context["session_key"] = "discord-voice:" + strings.TrimSpace(request.Payload.GuildID) + ":" + strings.TrimSpace(request.Payload.VoiceChannelID)
		context["force_new_session"] = false
	}
	if request.Payload.Direct != nil {
		context["is_direct_message"] = *request.Payload.Direct
	}
	if request.Payload.Mentioned != nil {
		context["mentions_bot"] = *request.Payload.Mentioned
	}
	if request.Payload.SelfChat != nil {
		context["is_self_chat"] = *request.Payload.SelfChat
	}
	if request.Payload.OwnAccount != nil {
		context["is_own_account"] = *request.Payload.OwnAccount
	}
	if request.Payload.ChatKind != "" {
		context["chat_kind"] = request.Payload.ChatKind
	}
	if request.Payload.Numbers != nil {
		context["sender_numbers"] = request.Payload.Numbers
	}
	if eventType == "message.created" {
		context["content"] = content
		if source == core.SourceDiscord && request.Payload.ReplyToContent != "" {
			quoted := []rune(request.Payload.ReplyToContent)
			if len(quoted) > 8000 {
				quoted = quoted[:8000]
			}
			context["reply_to_content"] = string(quoted)
		}
	}

	displayName := []rune(strings.TrimSpace(request.Payload.UserName))
	name := string(displayName[:min(len(displayName), 100)])
	if name == "" {
		name = strings.TrimSpace(request.Payload.UserID)
	}
	return core.Event{
		Source: source,
		Type:   eventType,
		User: core.EventUser{
			ID:   strings.TrimSpace(request.Payload.UserID),
			Name: name,
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
	case "realtime.start", "realtime.join", "realtime.leave", "realtime.stop", "realtime.observe":
		return strings.TrimSpace(request.EventType), request.Payload.Realtime
	case "voice.synthesize", "voice.transcribe", "voice.wakeword", "voice.activity":
		return strings.TrimSpace(request.EventType), request.Payload.Voice
	case "music.play", "music.skip", "music.pause", "music.resume", "music.stop", "music.queue", "music.volume", "music.ended", "music.comment", "music.history":
		// What follows the command (words, a link, a volume) is read by the Communications
		// domain; the bridge passes it on as typed.
		return strings.TrimSpace(request.EventType), map[string]any{"content": content}
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
	if reply := strings.TrimSpace(stringValue(result["reply"])); reply != "" {
		return reply
	}

	status := strings.TrimSpace(stringValue(result["status"]))
	if status == "" {
		status = "unknown"
	}

	lines := []string{fmt.Sprintf("ningo status: %s", status)}
	services, _ := result["services"].(map[string]any)

	if chatProvider, ok := services["chat"].(map[string]any); ok {
		line := fmt.Sprintf("chat: %s", strings.TrimSpace(stringValue(chatProvider["status"])))
		if err := strings.TrimSpace(stringValue(chatProvider["error"])); err != "" {
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
