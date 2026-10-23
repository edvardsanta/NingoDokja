package communications

import (
	"context"
	"fmt"
	"strings"
)

// ChannelSender and ConversationSender are domain ports. Implementations own
// network protocols and credentials; calls do not grant delivery permission.
type ChannelSender interface {
	Deliver(ctx context.Context, target, content, attachmentURL string) error
	DeliverAttachment(ctx context.Context, target, content string, data []byte, contentType, attachmentURL string) error
}
type ConversationSender interface {
	List(ctx context.Context) ([]Conversation, error)
	Deliver(ctx context.Context, alias, content string) error
}

// SendRequest contains normalized application intent, never SDK or HTTP objects.
// Origin values are supplied by ingress; they are not an authentication mechanism.
type SendRequest struct {
	Action, Source, Interface string
	Content, AttachmentURL    string
	Targets                   []string
	All, ForceNSFW            bool
}
type SendPlan struct {
	Content, AttachmentURL string
	Targets, SwitchedOff   []string
	ForceNSFW              bool
}
type DeliveryPolicy struct {
	Channels []string
	SafeOnly map[string]struct{}
	Disabled map[string]bool
}

func PlatformOf(target string) string {
	if strings.HasPrefix(target, WhatsAppPrefix) {
		return "whatsapp"
	}
	return "discord"
}
func HasConversationTarget(targets []string) bool {
	for _, target := range targets {
		if PlatformOf(target) == "whatsapp" {
			return true
		}
	}
	return false
}

// PlanChannelSend validates the full target set before any delivery. The caller
// must route conversation targets to ValidateConversationSend first.
func (p DeliveryPolicy) PlanChannelSend(r SendRequest) (SendPlan, error) {
	plan := SendPlan{Content: strings.TrimSpace(r.Content), AttachmentURL: strings.TrimSpace(r.AttachmentURL), ForceNSFW: r.ForceNSFW}
	if plan.Content == "" && plan.AttachmentURL == "" {
		return SendPlan{}, fmt.Errorf("discord send requires content or attachment_url")
	}
	if r.ForceNSFW && plan.AttachmentURL == "" {
		return SendPlan{}, fmt.Errorf("force_nsfw requires attachment_url")
	}
	if r.ForceNSFW && !explicitBypass(r) {
		return SendPlan{}, fmt.Errorf("force_nsfw is only allowed for explicit desktop or TUI sends")
	}
	targets := r.Targets
	if r.All {
		targets = p.Channels
	} else {
		targets = deduplicate(targets)
	}
	if len(targets) == 0 {
		return SendPlan{}, fmt.Errorf("discord send requires channel_ids or all")
	}
	allowed := map[string]bool{}
	for _, target := range p.Channels {
		allowed[target] = true
	}
	for _, target := range targets {
		if !allowed[target] {
			return SendPlan{}, fmt.Errorf("channel %s is not a configured delivery channel", target)
		}
	}
	for _, target := range targets {
		if p.Disabled[PlatformOf(target)] {
			plan.SwitchedOff = append(plan.SwitchedOff, target)
		} else {
			plan.Targets = append(plan.Targets, target)
		}
	}
	if len(plan.Targets) == 0 {
		return SendPlan{}, fmt.Errorf("nothing sent: %s is switched off", strings.Join(plan.SwitchedOff, ","))
	}
	return plan, nil
}
func explicitBypass(r SendRequest) bool {
	return (r.Source == "cli" && r.Action == "discord.send" && strings.TrimSpace(r.Interface) == "tui") ||
		(r.Source == "desktop" && r.Action == "communications.send" && strings.TrimSpace(r.Interface) == "desktop")
}
func (p DeliveryPolicy) RequiresSafe(target string) bool { _, yes := p.SafeOnly[target]; return yes }

// Eligible is shared by manual and scheduled delivery. A bypass must already
// have been authorized by PlanChannelSend; scheduled workflows never request it.
func (p DeliveryPolicy) Eligible(target string, safe, bypass bool) bool {
	configured := false
	for _, channel := range p.Channels {
		if channel == target {
			configured = true
			break
		}
	}
	return configured && !p.Disabled[PlatformOf(target)] && (!p.RequiresSafe(target) || safe || bypass)
}

// ValidateConversationSend preserves the stricter single named text-message
// intent. Service availability is checked before resolving the current aliases.
func ValidateConversationSend(r SendRequest) (SendPlan, error) {
	if r.Action != "communications.send" {
		return SendPlan{}, fmt.Errorf("WhatsApp conversations can only be sent to through communications.send")
	}
	if r.All || len(r.Targets) != 1 || PlatformOf(r.Targets[0]) != "whatsapp" {
		return SendPlan{}, fmt.Errorf("send to one WhatsApp conversation at a time")
	}
	if strings.TrimSpace(r.AttachmentURL) != "" {
		return SendPlan{}, fmt.Errorf("WhatsApp conversations take text only")
	}
	if r.ForceNSFW {
		return SendPlan{}, fmt.Errorf("force_nsfw does not apply to WhatsApp conversations")
	}
	content := strings.TrimSpace(r.Content)
	if content == "" {
		return SendPlan{}, fmt.Errorf("send requires content")
	}
	return SendPlan{Content: content, Targets: append([]string{}, r.Targets...)}, nil
}

// AuthorizeConversation keeps routing enablement with the other domain delivery
// decisions. The caller still checks that a sender adapter is configured first.
func (p DeliveryPolicy) AuthorizeConversation(target string) error {
	if PlatformOf(target) != "whatsapp" {
		return fmt.Errorf("channel %s is not a named WhatsApp conversation", target)
	}
	if p.Disabled["whatsapp"] {
		return fmt.Errorf("nothing sent: whatsapp is switched off")
	}
	return nil
}

func NamedConversation(target string, conversations []Conversation) (string, error) {
	if PlatformOf(target) != "whatsapp" {
		return "", fmt.Errorf("channel %s is not a named WhatsApp conversation", target)
	}
	name := strings.TrimPrefix(target, WhatsAppPrefix)
	for _, conversation := range conversations {
		if conversation.Alias == name {
			return name, nil
		}
	}
	return "", fmt.Errorf("channel %s is not a named WhatsApp conversation", target)
}
func deduplicate(values []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, v := range values {
		if v != "" && !seen[v] {
			out = append(out, v)
			seen[v] = true
		}
	}
	return out
}
