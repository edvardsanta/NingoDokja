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

// OwnerProfileIdentityReply adds only the preferred name to a direct identity reply.
// Birthdays and other personal facts are not broadcast by this shortcut.
func OwnerProfileIdentityReply(content string, verified bool, profile OwnerProfile) string {
	reply := OwnerIdentityReply(content, verified)
	if reply == "" || profile.Name == "" {
		return reply
	}
	if strings.HasPrefix(reply, "Você") {
		return fmt.Sprintf("Você é %s, meu dono. Reconheço sua conta pelo Discord; apelido não muda isso.", profile.Name)
	}
	return fmt.Sprintf("You’re %s, my owner. I recognize your Discord account; changing your nickname doesn’t change that.", profile.Name)
}

// OwnerIdentityReply resolves a narrow identity question from verified metadata.
// It never infers ownership from a name, mention, quotation or identity claim.
func OwnerIdentityReply(content string, verifiedOwner bool) string {
	if !verifiedOwner {
		return ""
	}
	question := strings.Trim(strings.ToLower(strings.TrimSpace(content)), "?!. ")
	question = strings.Join(strings.Fields(question), " ")
	switch question {
	case "quem sou eu", "quem eu sou", "você sabe quem eu sou", "voce sabe quem eu sou", "sou seu dono", "eu sou seu dono":
		return "Você é meu dono. Reconheço sua conta pelo Discord; apelido não muda isso."
	case "who am i", "do you know who i am", "am i your owner":
		return "You're my owner. I recognize your Discord account; changing your nickname doesn't change that."
	default:
		return ""
	}
}
