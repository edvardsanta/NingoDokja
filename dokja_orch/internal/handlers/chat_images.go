package handlers

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	promptfiles "read_books/dokja_store/prompts"
	"strings"
	"time"

	chat "read_books/dokja_domain/dokja_chat"
	"read_books/internal/clients"
	"read_books/internal/core"
)

// imageReadBudget bounds one vision call; a GPU model answers in seconds, a cold
// CPU model can need most of this.
const imageReadBudget = 60 * time.Second

// Only this fixed application text is a system message. Filenames and the
// model's readings are serialized as an untrusted user message.
func imageConversationBrief() string { return promptfiles.MustRead("image_conversation_brief") }

type imageFailure struct {
	URL    string `json:"url"`
	Reason string `json:"reason"`
}
type imageDescription struct {
	Number   int    `json:"number"`
	Filename string `json:"filename"`
	Text     string `json:"text"`
}
type imageTurnData struct {
	FailureOnly  bool               `json:"-"`
	Failures     []imageFailure     `json:"failures,omitempty"`
	Omitted      int                `json:"omitted_images,omitempty"`
	Descriptions []imageDescription `json:"images,omitempty"`
}

// ImageReadingService reads one image the message carried. Implemented by the
// vision service client; tests substitute a fake.
type ImageReadingService interface {
	DescribeImage(ctx context.Context, url, filename, contentType string, size int64) (string, error)
}

type imageReading struct {
	describe ImageReadingService
	enabled  func() bool
}

// WithImageReading lets chat discuss images the vision service reads. Images are
// read only for Discord messages, after inbound authorization.
func (h *ChatDomainHandler) WithImageReading(describe ImageReadingService, enabled func() bool) *ChatDomainHandler {
	h.images = &imageReading{describe: describe, enabled: enabled}
	return h
}

// withImageSources reads the message's images, if any, and records the turn data
// in the event context so evidence injection and the status notice can find it.
func (h *ChatDomainHandler) withImageSources(ctx context.Context, event core.Event) (core.Event, *imageTurnData) {
	if event.Source != core.SourceDiscord || h.images == nil || h.images.describe == nil {
		return event, nil
	}
	attachments, _ := event.Payload["media_attachments"].([]chat.WebAttachment)
	planned := chat.PlanImageReadings(attachments)
	if len(planned) == 0 {
		return event, nil
	}
	data := &imageTurnData{}
	if h.images.enabled != nil && !h.images.enabled() {
		for _, request := range planned {
			data.Failures = append(data.Failures, imageFailure{request.URL, "vision_disabled"})
		}
		event.Context = cloneChatContext(event.Context)
		event.Context["image_reading_turn"] = data
		return event, data
	}
	data.Omitted = max(0, len(planned)-chat.MaxImageReadings)
	for _, request := range planned[:min(len(planned), chat.MaxImageReadings)] {
		if request.Error != "" {
			data.Failures = append(data.Failures, imageFailure{request.URL, request.Error})
			continue
		}
		readCtx, cancel := context.WithTimeout(ctx, imageReadBudget)
		text, err := h.images.describe.DescribeImage(readCtx, request.URL, request.Filename, request.ContentType, request.Size)
		cancel()
		if err != nil {
			data.Failures = append(data.Failures, imageFailure{request.URL, imageFailureReason(err)})
			continue
		}
		data.Descriptions = append(data.Descriptions, imageDescription{
			Number: len(data.Descriptions) + 1, Filename: request.Filename, Text: text,
		})
	}
	event.Context = cloneChatContext(event.Context)
	event.Context["image_reading_turn"] = data
	return event, data
}

func imageFailureReason(err error) string {
	switch {
	case errors.Is(err, clients.ErrVisionTooLarge):
		return "too_large"
	case errors.Is(err, clients.ErrVisionUnreadable):
		return "unreadable"
	case errors.Is(err, context.DeadlineExceeded):
		return "failed"
	default:
		return "failed"
	}
}

func withImageEvidence(messages []map[string]string, data *imageTurnData) []map[string]string {
	if data == nil {
		return messages
	}
	encoded, _ := json.Marshal(map[string]any{
		"kind": "UNTRUSTED_IMAGE_EVIDENCE", "images": data.Descriptions,
		"reading_status": map[string]any{"failures": data.Failures, "omitted_images": data.Omitted},
	})
	last := max(0, len(messages)-1)
	out := make([]map[string]string, 0, len(messages)+2)
	out = append(out, messages[:last]...)
	out = append(out, map[string]string{"role": "system", "content": imageConversationBrief()}, map[string]string{"role": "user", "content": string(encoded)})
	return append(out, messages[last:]...)
}

//go:embed locales/image_reading_*.json
var imageReadingLocales embed.FS

var imageReadingCatalogs = func() map[string]selfChatCatalog {
	catalogs := map[string]selfChatCatalog{}
	for _, locale := range []string{"pt", "en"} {
		raw, err := imageReadingLocales.ReadFile("locales/image_reading_" + locale + ".json")
		if err != nil {
			panic(err)
		}
		catalog := selfChatCatalog{}
		if err := json.Unmarshal(raw, &catalog); err != nil {
			panic(err)
		}
		catalogs[locale] = catalog
	}
	return catalogs
}()

func imageReadingLocale(event core.Event) string {
	locale, _ := event.Payload["locale"].(string)
	if strings.HasPrefix(locale, "en") {
		return "en"
	}
	return "pt"
}

func imageReadingStatus(event core.Event, data *imageTurnData) string {
	if data == nil {
		return ""
	}
	catalog := imageReadingCatalogs[imageReadingLocale(event)]
	var lines []string
	tooLarge, unreadable, disabled := 0, 0, 0
	for _, failure := range data.Failures {
		switch failure.Reason {
		case "too_large":
			tooLarge++
		case "unreadable":
			unreadable++
		case "vision_disabled":
			disabled++
		}
	}
	if disabled > 0 {
		lines = append(lines, catalog.say("vision_disabled", "count", fmt.Sprint(disabled)))
	}
	if tooLarge > 0 {
		lines = append(lines, catalog.say("too_large", "count", fmt.Sprint(tooLarge), "limit", fmt.Sprint(chat.MaxImageReadingBytes>>20)))
	}
	if unreadable > 0 {
		lines = append(lines, catalog.say("unreadable", "count", fmt.Sprint(unreadable)))
	}
	if other := len(data.Failures) - tooLarge - unreadable - disabled; other > 0 {
		lines = append(lines, catalog.say("failed", "count", fmt.Sprint(other)))
	}
	if data.Omitted > 0 {
		lines = append(lines, catalog.say("omitted", "count", fmt.Sprint(data.Omitted), "limit", fmt.Sprint(chat.MaxImageReadings)))
	}
	if data.FailureOnly {
		lines = append(lines, catalog.say("recovery"))
	}
	return strings.Join(lines, "\n")
}

func imageAttachmentPrompt(event core.Event) string {
	return imageReadingCatalogs[imageReadingLocale(event)].say("image_prompt")
}

// Image evidence is untrusted data too: mentions and model-echoed URLs from a
// reading never reach Discord as live markup or destinations. Bracketed numbers
// keep their source meaning: on an image turn they survive only when they cite
// an actually supplied source, so a model that writes [1] meaning 'image 1'
// does not get a text source's citation label attached.
func sanitizeImageReply(reply string, sources []knowledgeSource) string {
	reply = webMention.ReplaceAllStringFunc(reply, func(s string) string { return strings.ReplaceAll(s, "@", "＠") })
	reply = strings.NewReplacer("@everyone", "＠everyone", "@here", "＠here").Replace(reply)
	reply = generatedWebLink.ReplaceAllString(reply, "$1")
	reply = generatedWebURL.ReplaceAllString(reply, quoteRemoved)
	valid := map[string]bool{}
	for _, s := range sources {
		valid[fmt.Sprint(s.Number)] = true
	}
	return citationPattern.ReplaceAllStringFunc(reply, func(mark string) string {
		var kept []string
		for _, n := range strings.Split(strings.Trim(mark, "[]"), ",") {
			n = strings.TrimSpace(n)
			if valid[n] {
				kept = append(kept, n)
			}
		}
		if len(kept) == 0 {
			return quoteRemoved
		}
		return "[" + strings.Join(kept, ", ") + "]"
	})
}
