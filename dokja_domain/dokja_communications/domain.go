// Package communications defines which conversations the operator may read.
package communications

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

const ActionChannels = "list-communication-channels"
const ActionHistory = "read-communication-history"

// WhatsAppPrefix marks the id of a conversation that an interface keeps the history of. The part
// after it is the alias the owner gave the conversation; the chat itself is never named.
const WhatsAppPrefix = "whatsapp:"

var snowflake = regexp.MustCompile(`^[0-9]{1,20}$`)
var alias = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)
var cursor = regexp.MustCompile(`^[0-9]{1,15}$`)

type Service interface {
	Dispatch(context.Context, string, map[string]any) (map[string]any, error)
}

// Conversation is a chat the owner named on an interface that keeps its own history.
type Conversation struct {
	Alias         string
	Kind          string
	RetentionDays int
}

// Conversations is an interface that keeps the text of the conversations the owner named. History
// returns a page shaped like a page of the read service: messages and the cursor of the older page.
type Conversations interface {
	List(ctx context.Context) ([]Conversation, error)
	History(ctx context.Context, alias, before string) (map[string]any, error)
}

type Domain struct {
	service       Service
	channels      []string
	conversations Conversations
}

func New(service Service, configured string) *Domain {
	d := &Domain{service: service}
	seen := map[string]bool{}
	for _, id := range strings.Split(configured, ",") {
		id = strings.TrimSpace(id)
		if snowflake.MatchString(id) && !seen[id] {
			d.channels = append(d.channels, id)
			seen[id] = true
		}
	}
	return d
}

// WithConversations adds the conversations an interface keeps, next to the configured channels.
func (d *Domain) WithConversations(conversations Conversations) *Domain {
	d.conversations = conversations
	return d
}

func (d *Domain) Handle(ctx context.Context, action string, payload map[string]any) (map[string]any, error) {
	if d == nil || (d.service == nil && d.conversations == nil) {
		return nil, fmt.Errorf("communications service is not configured")
	}
	switch action {
	case ActionChannels:
		return d.listChannels(ctx)
	case ActionHistory:
		channel, _ := payload["channel_id"].(string)
		before, _ := payload["before"].(string)
		if name, ok := strings.CutPrefix(channel, WhatsAppPrefix); ok {
			return d.conversationHistory(ctx, channel, name, before)
		}
		return d.channelHistory(ctx, channel, before)
	default:
		return nil, fmt.Errorf("unsupported communications action")
	}
}

func (d *Domain) listChannels(ctx context.Context) (map[string]any, error) {
	answer := map[string]any{}
	channels := []any{}
	unavailable := []any{}
	var discordErr error
	if d.service != nil && len(d.channels) > 0 {
		result, err := d.service.Dispatch(ctx, "channels", map[string]any{"channel_ids": d.channels})
		if err != nil {
			discordErr = err
		} else {
			for key, value := range result {
				answer[key] = value
			}
			if listed, ok := result["channels"].([]any); ok {
				channels = append(channels, listed...)
			}
		}
	}
	if d.conversations != nil {
		listed, err := d.conversations.List(ctx)
		if err != nil {
			// What could not be read is said apart; the other source still lists.
			unavailable = append(unavailable, "whatsapp")
		}
		for _, conversation := range listed {
			channels = append(channels, map[string]any{
				"id":             WhatsAppPrefix + conversation.Alias,
				"name":           conversation.Alias,
				"kind":           conversation.Kind,
				"retention_days": conversation.RetentionDays,
			})
		}
	}
	if discordErr != nil {
		// A Discord read service that is down must not hide the conversations that can be read. With
		// nothing else to show, the error stands, as it always did.
		if len(channels) == 0 {
			return nil, discordErr
		}
		unavailable = append(unavailable, "discord")
	}
	if len(unavailable) > 0 {
		answer["unavailable"] = unavailable
	}
	answer["channels"] = channels
	return answer, nil
}

func (d *Domain) channelHistory(ctx context.Context, channel, before string) (map[string]any, error) {
	if d.service == nil {
		return nil, fmt.Errorf("communications service is not configured")
	}
	allowed := false
	for _, id := range d.channels {
		if channel == id {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, fmt.Errorf("channel is not configured for communications")
	}
	if before != "" && !snowflake.MatchString(before) {
		return nil, fmt.Errorf("before must be a message id")
	}
	return d.service.Dispatch(ctx, "history", map[string]any{"channel_id": channel, "before": before, "limit": 30})
}

// conversationHistory reads a named conversation. The alias must be one the interface itself
// lists, so a made-up alias never reaches the history route.
func (d *Domain) conversationHistory(ctx context.Context, channel, name, before string) (map[string]any, error) {
	if d.conversations == nil || !alias.MatchString(name) {
		return nil, fmt.Errorf("channel is not configured for communications")
	}
	if before != "" && !cursor.MatchString(before) {
		return nil, fmt.Errorf("before must be a message id")
	}
	listed, err := d.conversations.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("conversations are not available")
	}
	named := false
	for _, conversation := range listed {
		if conversation.Alias == name {
			named = true
			break
		}
	}
	if !named {
		return nil, fmt.Errorf("channel is not configured for communications")
	}
	page, err := d.conversations.History(ctx, name, before)
	if err != nil {
		return nil, fmt.Errorf("conversation history is not available")
	}
	return map[string]any{"channel_id": channel, "messages": page["messages"], "before": page["before"]}, nil
}
