package chat

import (
	"embed"
	"encoding/json"
	"fmt"
	"strings"
)

// OwnerProfile contains operator-configured personal facts, never account credentials.
type OwnerProfile struct {
	Name        string   `json:"name"`
	BirthDate   string   `json:"birth_date,omitempty"`
	Occupations []string `json:"occupations,omitempty"`
}

// NingoFacts are private facts about the assistant and the people around it. They stay in the owner's
// configuration because this repository is public, and they reach a conversation only where the owner
// allowed: the self-chat and the groups listed here.
type NingoFacts struct {
	// NamedBy is who chose Ningo's name.
	NamedBy string `json:"named_by,omitempty"`
	// Groups are the WhatsApp groups where these facts may be used.
	Groups []string      `json:"groups,omitempty"`
	People []KnownPerson `json:"people,omitempty"`
}

// KnownPerson is someone Ningo knows. WhatsApp recognizes them by the number and never reaches the
// model; Name and About do.
type KnownPerson struct {
	Name     string   `json:"name"`
	WhatsApp string   `json:"whatsapp,omitempty"`
	About    []string `json:"about,omitempty"`
}

// OwnerChannel is how the owner was recognized for this turn, and so what the reply says about it.
type OwnerChannel string

const (
	// The Discord gateway verified the owner's account.
	OwnerByDiscord OwnerChannel = "discord"
	// The owner's own local interfaces (desktop, CLI, TUI), which only the owner uses.
	OwnerByLocal OwnerChannel = "local"
	// The linked WhatsApp account's own "message yourself" chat, which only the account can write in.
	OwnerByWhatsApp OwnerChannel = "whatsapp"
	// The linked WhatsApp account writing in a group: WhatsApp marks the message as the account's own.
	OwnerByWhatsAppGroup OwnerChannel = "whatsapp_group"
)

//go:embed locales/identity_*.json
var identityLocales embed.FS

var identityCatalogs = func() map[string]map[string]string {
	catalogs := map[string]map[string]string{}
	for _, locale := range []string{"pt", "en"} {
		raw, err := identityLocales.ReadFile("locales/identity_" + locale + ".json")
		if err != nil {
			panic(err)
		}
		catalog := map[string]string{}
		if err := json.Unmarshal(raw, &catalog); err != nil {
			panic(fmt.Sprintf("identity catalog %s: %v", locale, err))
		}
		catalogs[locale] = catalog
	}
	return catalogs
}()

// identityQuestion answers the language of a direct identity question, or empty for anything else.
func identityQuestion(content string) string {
	question := strings.Trim(strings.ToLower(strings.TrimSpace(content)), "?!. ")
	question = strings.Join(strings.Fields(question), " ")
	switch question {
	case "quem sou eu", "quem eu sou", "você sabe quem eu sou", "voce sabe quem eu sou", "você sabe quem sou eu", "voce sabe quem sou eu",
		"sabe quem eu sou", "sabe quem sou eu", "sou seu dono", "eu sou seu dono":
		return "pt"
	case "who am i", "do you know who i am", "am i your owner":
		return "en"
	default:
		return ""
	}
}

// OwnerProfileIdentityReply adds only the preferred name to a direct identity reply.
// Birthdays and other personal facts are not broadcast by this shortcut.
func OwnerProfileIdentityReply(content string, verified bool, profile OwnerProfile, channel OwnerChannel) string {
	locale := identityQuestion(content)
	if !verified || locale == "" {
		return ""
	}
	catalog := identityCatalogs[locale]
	first := catalog["owner"]
	if profile.Name != "" {
		first = strings.ReplaceAll(catalog["owner_named"], "{name}", profile.Name)
	}
	return first + " " + catalog["by_"+string(channel)]
}

// OwnerIdentityReply resolves a narrow identity question from verified metadata.
// It never infers ownership from a name, mention, quotation or identity claim.
func OwnerIdentityReply(content string, verifiedOwner bool, channel OwnerChannel) string {
	return OwnerProfileIdentityReply(content, verifiedOwner, OwnerProfile{}, channel)
}
