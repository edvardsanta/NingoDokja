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
	guilds        []string
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

// WithGuilds lets every text channel of these servers be listed, read and written to by hand,
// next to the configured channels. No server means the configured channels only: never every
// server the bot happens to be in.
func (d *Domain) WithGuilds(configured string) *Domain {
	d.guilds = splitConfigured(configured)
	return d
}

// Readable decides whether a Discord channel may be read and written to by hand: a configured
// channel, or one Discord places in a configured server and that holds messages. The id the
// operator sends is never trusted alone.
func (d *Domain) Readable(ctx context.Context, channel string) (bool, error) {
	for _, id := range d.channels {
		if channel == id {
			return true, nil
		}
	}
	if len(d.guilds) == 0 || d.service == nil || !snowflake.MatchString(channel) {
		return false, nil
	}
	info, err := d.service.Dispatch(ctx, "channel_info", map[string]any{"channel_id": channel})
	if err != nil {
		return false, err
	}
	text, _ := info["text"].(bool)
	guild, _ := info["guild_id"].(string)
	return text && contains(d.guilds, guild), nil
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
	listed := map[string]bool{}
	if d.service != nil && len(d.guilds) > 0 {
		guildChannels, unavailableGuilds, err := d.guildChannels(ctx)
		if err != nil {
			discordErr = err
		}
		for _, channel := range guildChannels {
			listed[channel["id"].(string)] = true
			channels = append(channels, channel)
		}
		if len(unavailableGuilds) > 0 {
			answer["unavailable_guilds"] = unavailableGuilds
		}
	}
	// Configured channels on no configured server are still listed, by id or name.
	var rest []string
	for _, id := range d.channels {
		if !listed[id] {
			rest = append(rest, id)
		}
	}
	if d.service != nil && len(rest) > 0 {
		result, err := d.service.Dispatch(ctx, "channels", map[string]any{"channel_ids": rest})
		if err != nil {
			discordErr = err
		} else {
			if named, ok := result["channels"].([]any); ok {
				channels = append(channels, named...)
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
	allowed, err := d.Readable(ctx, channel)
	if err != nil {
		return nil, err
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

// guildChannels lists the text channels of the configured servers, each tagged with its server.
func (d *Domain) guildChannels(ctx context.Context) ([]map[string]any, []any, error) {
	result, err := d.service.Dispatch(ctx, "guild_channels", map[string]any{})
	if err != nil {
		return nil, nil, err
	}
	var channels []map[string]any
	guilds, _ := result["guilds"].([]any)
	for _, raw := range guilds {
		guild, _ := raw.(map[string]any)
		guildID, _ := guild["id"].(string)
		guildName, _ := guild["name"].(string)
		// The service answers for the servers it was told; one it names unasked is ignored.
		if !contains(d.guilds, guildID) {
			continue
		}
		listed, _ := guild["channels"].([]any)
		for _, rawChannel := range listed {
			channel, _ := rawChannel.(map[string]any)
			id, _ := channel["id"].(string)
			if !snowflake.MatchString(id) {
				continue
			}
			name, _ := channel["name"].(string)
			channels = append(channels, map[string]any{"id": id, "name": name, "guild_id": guildID, "guild_name": guildName})
		}
	}
	unavailable, _ := result["unavailable_guilds"].([]any)
	return channels, unavailable, nil
}
