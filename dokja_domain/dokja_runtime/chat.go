package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type ChatBinding struct {
	Revision uint64 `json:"configuration_revision"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	// Format asks one request for a reply format: "" for text, ChatFormatJSON for one JSON
	// value, or a JSON Schema object the reply must match. It belongs to a request, never to
	// the saved chat configuration.
	Format string `json:"format,omitempty"`
}

// ChatFormatJSON asks for a reply that is one JSON value. Providers that can enforce a format
// do (Ollama's --format); the caller still checks the reply against its own contract.
const ChatFormatJSON = "json"

// maxChatFormat bounds a JSON Schema sent as a reply format.
const maxChatFormat = 4096

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type ChatReply struct {
	Reply    string `json:"reply"`
	Provider string `json:"provider"`
	Model    string `json:"requested_model"`
}
type ChatSettingsStore interface {
	ReadChat() (ChatBinding, error)
	WriteChat(ChatBinding) (ChatBinding, error)
}
type ChatInference interface {
	Generate(context.Context, ChatBinding, []ChatMessage) (string, error)
}

// ChatStreamInference is an inference that can show its text while it writes. The final string is
// the reply; what reaches onPreview is only a preview of it.
type ChatStreamInference interface {
	GenerateStream(context.Context, ChatBinding, []ChatMessage, func(string)) (string, error)
}

func (d *Domain) WithChat(store ChatSettingsStore, inference ChatInference) *Domain {
	d.chatSettings = store
	d.chatInference = inference
	return d
}
func (d *Domain) ChatBinding() (ChatBinding, error) {
	if d.chatSettings == nil {
		return ChatBinding{}, fmt.Errorf("chat settings are unavailable")
	}
	return d.chatSettings.ReadChat()
}
func (d *Domain) validateChat(b ChatBinding) error {
	if !validChatFormat(b.Format) {
		return fmt.Errorf("unsupported chat reply format")
	}
	if b.Provider == "" || !modelName.MatchString(b.Model) {
		return fmt.Errorf("select a chat provider and model in Inference providers")
	}
	if d.providerSettings == nil {
		return fmt.Errorf("provider configuration is unavailable")
	}
	p, err := d.providerSettings.ReadProviders()
	if err != nil {
		return err
	}
	for _, v := range p.Providers {
		if v.ID == b.Provider {
			if !v.Enabled {
				return fmt.Errorf("selected chat provider is disabled")
			}
			for _, m := range v.Models {
				if m == b.Model {
					return nil
				}
			}
			return fmt.Errorf("declare the selected chat model in its provider registration")
		}
	}
	return fmt.Errorf("unsupported chat provider")
}
func (d *Domain) ConfigureChat(b ChatBinding) (ChatBinding, error) {
	if d.chatSettings == nil {
		return ChatBinding{}, fmt.Errorf("chat settings are unavailable")
	}
	if b.Revision < 1 || b.Revision >= MaxProviderRevision {
		return ChatBinding{}, fmt.Errorf("invalid chat configuration revision")
	}
	b.Format = "" // a reply format is asked per request, never saved
	if err := d.validateChat(b); err != nil {
		return ChatBinding{}, err
	}
	return d.chatSettings.WriteChat(b)
}
func (d *Domain) ChatReady() error {
	b, err := d.ChatBinding()
	if err != nil {
		return err
	}
	return d.validateChat(b)
}
func (d *Domain) Chat(ctx context.Context, messages []ChatMessage) (ChatReply, error) {
	return d.ChatStream(ctx, messages, nil)
}

// ChatWithBinding runs one explicitly validated provider/model binding. It is used by
// bounded internal tasks that have their own provider fallback order.
func (d *Domain) ChatWithBinding(ctx context.Context, binding ChatBinding, messages []ChatMessage) (ChatReply, error) {
	return d.chatStreamWithBinding(ctx, binding, messages, nil)
}

// ChatStream is Chat with a preview: while the provider writes, onPreview receives the text so far.
// The preview is provisional and never the reply: it is not validated, and the reply is only what
// ChatStream returns, under the same rules as Chat. An inference that cannot stream gives no preview.
func (d *Domain) ChatStream(ctx context.Context, messages []ChatMessage, onPreview func(string)) (ChatReply, error) {
	b, err := d.ChatBinding()
	if err != nil {
		return ChatReply{}, err
	}
	return d.chatStreamWithBinding(ctx, b, messages, onPreview)
}

func (d *Domain) chatStreamWithBinding(ctx context.Context, b ChatBinding, messages []ChatMessage, onPreview func(string)) (ChatReply, error) {
	if err := ctx.Err(); err != nil {
		return ChatReply{}, err
	}
	if err := d.validateChat(b); err != nil {
		return ChatReply{}, err
	}
	if d.chatInference == nil {
		return ChatReply{}, fmt.Errorf("chat inference is unavailable")
	}
	if err := validateChatMessages(messages); err != nil {
		return ChatReply{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	var reply string
	var err error
	if streaming, ok := d.chatInference.(ChatStreamInference); ok && onPreview != nil {
		reply, err = streaming.GenerateStream(ctx, b, messages, onPreview)
	} else {
		reply, err = d.chatInference.Generate(ctx, b, messages)
	}
	if ctx.Err() != nil {
		return ChatReply{}, ctx.Err()
	}
	if err != nil {
		return ChatReply{}, err
	}
	if strings.TrimSpace(reply) == "" || len(reply) > 65536 {
		return ChatReply{}, fmt.Errorf("invalid chat provider response")
	}
	return ChatReply{Reply: reply, Provider: b.Provider, Model: b.Model}, nil
}

// validChatFormat accepts no format, "json", or one JSON Schema object of bounded size.
func validChatFormat(format string) bool {
	if format == "" || format == ChatFormatJSON {
		return true
	}
	var schema map[string]any
	return len(format) <= maxChatFormat && json.Unmarshal([]byte(format), &schema) == nil && schema["type"] != nil
}
