package runtime

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type ChatBinding struct {
	Revision uint64 `json:"configuration_revision"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
}
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
	if err := ctx.Err(); err != nil {
		return ChatReply{}, err
	}
	b, err := d.ChatBinding()
	if err != nil {
		return ChatReply{}, err
	}
	if err = d.validateChat(b); err != nil {
		return ChatReply{}, err
	}
	if d.chatInference == nil {
		return ChatReply{}, fmt.Errorf("chat inference is unavailable")
	}
	if len(messages) == 0 || len(messages) > 128 {
		return ChatReply{}, fmt.Errorf("invalid chat history")
	}
	size := 0
	for _, m := range messages {
		size += len(m.Content)
		if (m.Role != "user" && m.Role != "assistant" && m.Role != "system") || strings.TrimSpace(m.Content) == "" {
			return ChatReply{}, fmt.Errorf("invalid chat message")
		}
	}
	if size > 65536 {
		return ChatReply{}, fmt.Errorf("chat history exceeds supported size")
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	reply, err := d.chatInference.Generate(ctx, b, messages)
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
