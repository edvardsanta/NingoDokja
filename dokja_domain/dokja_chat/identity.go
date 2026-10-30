package chat

import (
	"fmt"
	"strings"
)

// OwnerProfile contains operator-configured personal facts, never account credentials.
type OwnerProfile struct {
	Name        string   `json:"name"`
	BirthDate   string   `json:"birth_date,omitempty"`
	Occupations []string `json:"occupations,omitempty"`
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
)

// OwnerProfileIdentityReply adds only the preferred name to a direct identity reply.
// Birthdays and other personal facts are not broadcast by this shortcut.
func OwnerProfileIdentityReply(content string, verified bool, profile OwnerProfile, channel OwnerChannel) string {
	reply := OwnerIdentityReply(content, verified, channel)
	if reply == "" || profile.Name == "" {
		return reply
	}
	if strings.HasPrefix(reply, "Você") {
		return fmt.Sprintf("Você é %s, meu dono. %s", profile.Name, howRecognized(channel, true))
	}
	return fmt.Sprintf("You’re %s, my owner. %s", profile.Name, howRecognized(channel, false))
}

// OwnerIdentityReply resolves a narrow identity question from verified metadata.
// It never infers ownership from a name, mention, quotation or identity claim.
func OwnerIdentityReply(content string, verifiedOwner bool, channel OwnerChannel) string {
	if !verifiedOwner {
		return ""
	}
	question := strings.Trim(strings.ToLower(strings.TrimSpace(content)), "?!. ")
	question = strings.Join(strings.Fields(question), " ")
	switch question {
	case "quem sou eu", "quem eu sou", "você sabe quem eu sou", "voce sabe quem eu sou", "você sabe quem sou eu", "voce sabe quem sou eu",
		"sabe quem eu sou", "sabe quem sou eu", "sou seu dono", "eu sou seu dono":
		return "Você é meu dono. " + howRecognized(channel, true)
	case "who am i", "do you know who i am", "am i your owner":
		return "You're my owner. " + howRecognized(channel, false)
	default:
		return ""
	}
}

func howRecognized(channel OwnerChannel, portuguese bool) string {
	switch {
	case channel == OwnerByLocal && portuguese:
		return "Você fala comigo pela sua própria interface, que só você usa."
	case channel == OwnerByLocal:
		return "You're talking to me from your own interface, which only you use."
	case channel == OwnerByWhatsApp && portuguese:
		return "Você fala comigo pela sua própria conversa no WhatsApp."
	case channel == OwnerByWhatsApp:
		return "You're talking to me from your own WhatsApp chat."
	case portuguese:
		return "Reconheço sua conta pelo Discord; apelido não muda isso."
	default:
		return "I recognize your Discord account; changing your nickname doesn't change that."
	}
}
