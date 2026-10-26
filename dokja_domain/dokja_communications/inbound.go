package communications

import (
	"fmt"
	"regexp"
	"strings"
)

const ActionAuthorizeInbound = "authorize-inbound"

// InboundPolicy contains application eligibility, supplied by the orchestrator's
// configuration. Gateway adapters still reject echoes, duplicates and stale data.
type InboundPolicy struct {
	// DiscordGuildIDs are the servers Ningo answers in; none means any.
	DiscordGuildIDs    []string
	DiscordChannels    []string
	DiscordDMOpen      bool
	DiscordNeedMention bool
	WhatsAppSelfChat   bool
	WhatsAppNumbers    []string
	WhatsAppGroups     []string
}

type InboundIntent struct {
	Platform, Entry, ChannelID, GuildID, ChatKind string
	Direct, Mentioned, SelfChat                   bool
	SenderNumbers                                 []string
}

// AuthorizeInbound returns false for incomplete metadata. A rejected ingress
// request is silent so no application reply leaks into an unapproved channel.
func (p InboundPolicy) AuthorizeInbound(r InboundIntent) bool {
	if r.ChannelID == "" {
		return false
	}
	switch r.Platform {
	case "discord":
		if r.Entry != "message" && r.Entry != "command" {
			return false
		}
		if r.Direct {
			return p.DiscordDMOpen
		}
		if r.GuildID == "" {
			return false
		}
		if len(p.DiscordGuildIDs) > 0 && !contains(p.DiscordGuildIDs, r.GuildID) {
			return false
		}
		if len(p.DiscordChannels) > 0 && !contains(p.DiscordChannels, r.ChannelID) {
			return false
		}
		return r.Entry == "command" || !p.DiscordNeedMention || r.Mentioned
	case "whatsapp":
		if r.Entry != "message" {
			return false
		}
		if r.SelfChat {
			return r.ChatKind == "direct" && p.WhatsAppSelfChat
		}
		switch r.ChatKind {
		case "direct":
			return numberAllowed(p.WhatsAppNumbers, r.SenderNumbers)
		case "group":
			return contains(p.WhatsAppGroups, r.ChannelID) && r.Mentioned
		default:
			return false
		}
	default:
		return false
	}
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

var brazilianMobile = regexp.MustCompile(`^55(\d{2})9?([6-9]\d{7})$`)
var whatsappGroup = regexp.MustCompile(`^[0-9]+(-[0-9]+)*@g\.us$`)
var phoneFormatting = strings.NewReplacer(" ", "", "\t", "", "(", "", ")", "", ".", "", "+", "", "-", "")

func equivalentNumbers(number string) []string {
	match := brazilianMobile.FindStringSubmatch(number)
	if match == nil {
		return []string{number}
	}
	return []string{"55" + match[1] + "9" + match[2], "55" + match[1] + match[2]}
}

func numberAllowed(allowed, candidates []string) bool {
	wanted := map[string]bool{}
	for _, number := range allowed {
		for _, spelling := range equivalentNumbers(number) {
			wanted[spelling] = true
		}
	}
	for _, candidate := range candidates {
		for _, spelling := range equivalentNumbers(candidate) {
			if wanted[spelling] {
				return true
			}
		}
	}
	return false
}

// ParseInboundPolicy validates configuration before the server starts. An empty
// WhatsApp allowlist is valid only when self-chat is enabled, as in the gateway.
func ParseInboundPolicy(values map[string]string) (InboundPolicy, error) {
	selfChat, err := parseFlag(values["DOKJA_WHATSAPP_SELF_CHAT"], true)
	if err != nil {
		return InboundPolicy{}, fmt.Errorf("DOKJA_WHATSAPP_SELF_CHAT must be true or false")
	}
	needMention, err := parseFlag(values["DISCORD_REQUIRE_MENTION"], false)
	if err != nil {
		return InboundPolicy{}, fmt.Errorf("DISCORD_REQUIRE_MENTION must be true or false")
	}
	p := InboundPolicy{
		DiscordGuildIDs:    splitConfigured(values["DISCORD_GUILD_ID"]),
		DiscordChannels:    splitConfigured(values["DISCORD_ALLOWED_CHANNELS"]),
		DiscordDMOpen:      defaultValue(values["DISCORD_DM_POLICY"], "open") == "open",
		DiscordNeedMention: needMention,
		WhatsAppSelfChat:   selfChat,
		WhatsAppNumbers:    splitConfigured(values["DOKJA_WHATSAPP_ALLOW_FROM"]),
		WhatsAppGroups:     splitConfigured(values["DOKJA_WHATSAPP_ALLOWED_GROUPS"]),
	}
	for index, number := range p.WhatsAppNumbers {
		number = phoneFormatting.Replace(number)
		if len(number) < 7 || len(number) > 15 || strings.Trim(number, "0123456789") != "" {
			return InboundPolicy{}, fmt.Errorf("DOKJA_WHATSAPP_ALLOW_FROM has an invalid entry")
		}
		p.WhatsAppNumbers[index] = number
	}
	for _, group := range p.WhatsAppGroups {
		if !whatsappGroup.MatchString(group) {
			return InboundPolicy{}, fmt.Errorf("DOKJA_WHATSAPP_ALLOWED_GROUPS has an invalid entry")
		}
	}
	return p, nil
}

func parseFlag(value string, fallback bool) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return fallback, nil
	case "true", "1":
		return true, nil
	case "false", "0":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean")
	}
}

func defaultValue(value, fallback string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return fallback
}

func splitConfigured(value string) []string {
	result := []string{}
	for _, part := range strings.Split(value, ",") {
		if part = strings.TrimSpace(part); part != "" {
			result = append(result, part)
		}
	}
	return result
}
