package handlers

import (
	chat "read_books/dokja_domain/dokja_chat"
	communications "read_books/dokja_domain/dokja_communications"
	conversation "read_books/dokja_domain/dokja_conversation"
	"read_books/internal/core"
)

// WithOwnerProfile gives the handler the operator-configured owner profile, so the owner is known by
// name wherever the owner is recognized: on Discord through the gateway, and on the owner's own
// interfaces (desktop, CLI, TUI and the WhatsApp self-chat) by the source itself.
func (h *ChatDomainHandler) WithOwnerProfile(profile chat.OwnerProfile) *ChatDomainHandler {
	h.ownerProfile = profile
	return h
}

// WithNingoFacts gives the handler the private facts about Ningo and the people around it. They reach
// a turn only where privateFactsAllowed says so.
func (h *ChatDomainHandler) WithNingoFacts(facts chat.NingoFacts) *ChatDomainHandler {
	h.ningo = facts
	return h
}

// privateFactsAllowed is where the owner allowed the private facts: their own WhatsApp chat and the
// groups they listed. Nowhere else, Discord and the local interfaces included.
func (h *ChatDomainHandler) privateFactsAllowed(event core.Event) bool {
	if selfChatRequest(event) {
		return true
	}
	if event.Source != core.SourceWhatsApp || !whatsappGroupTurn(event) {
		return false
	}
	for _, group := range h.ningo.Groups {
		if group == event.Channel.ID {
			return true
		}
	}
	return false
}

func whatsappGroupTurn(event core.Event) bool {
	kind, _ := event.Context["chat_kind"].(string)
	return event.Source == core.SourceWhatsApp && kind == "group"
}

// attachPrivateFacts gives the turn who chose the name, the people Ningo knows, and who of them is
// speaking, recognized by the number WhatsApp reports. The number itself stays out of the turn.
func (h *ChatDomainHandler) attachPrivateFacts(event core.Event) {
	if !h.privateFactsAllowed(event) {
		return
	}
	if h.ningo.NamedBy != "" {
		event.Context["ningo_named_by"] = h.ningo.NamedBy
	}
	numbers, _ := event.Context["sender_numbers"].([]string)
	people := make([]chat.KnownPerson, 0, len(h.ningo.People))
	for _, person := range h.ningo.People {
		people = append(people, chat.KnownPerson{Name: person.Name, About: person.About})
		if person.WhatsApp != "" && communications.SameNumber(person.WhatsApp, numbers) {
			event.Context["known_speaker"] = person.Name
		}
	}
	if len(people) > 0 {
		event.Context["known_people"] = people
	}
}

// ownerChannel says whether the turn's speaker is the owner, and how that is known. Discord relies on
// the gateway's verification; the owner's interfaces are the owner's by the same rule that lets them
// keep conversations.
func ownerChannel(event core.Event) (chat.OwnerChannel, bool) {
	if event.Source == core.SourceDiscord {
		return chat.OwnerByDiscord, boolFromContext(event.Context, "is_owner")
	}
	if _, ok := conversation.ScopeFor(string(event.Source)); ok {
		return chat.OwnerByLocal, true
	}
	if selfChatRequest(event) {
		return chat.OwnerByWhatsApp, true
	}
	// WhatsApp marks what the linked account writes as its own; the interface passes that on.
	if own, _ := event.Context["is_own_account"].(bool); own && whatsappGroupTurn(event) {
		return chat.OwnerByWhatsAppGroup, true
	}
	return "", false
}

// withOwnerInterface marks a turn from the owner's own interface and attaches the profile. What an
// interface put in the context under these names is dropped first: only the handler grants them, and
// only the Discord gateway attaches a profile for Discord.
func (h *ChatDomainHandler) withOwnerInterface(event core.Event) core.Event {
	event.Context = cloneChatContext(event.Context)
	for _, granted := range []string{"owner_interface", "ningo_named_by", "known_people", "known_speaker", "known_facts", "facts_available", "fact_in_message"} {
		delete(event.Context, granted)
	}
	h.attachPrivateFacts(event)
	if event.Source != core.SourceDiscord {
		delete(event.Context, "owner_profile")
	}
	channel, owner := ownerChannel(event)
	if !owner || channel == chat.OwnerByDiscord {
		return event
	}
	event.Context["owner_interface"] = string(channel)
	if h.ownerProfile.Name != "" {
		event.Context["owner_profile"] = h.ownerProfile
	}
	return event
}

// ownerIdentityReply answers a direct "who am I" from the owner without asking the model.
func ownerIdentityReply(event core.Event, content string) string {
	channel, owner := ownerChannel(event)
	profile, _ := event.Context["owner_profile"].(chat.OwnerProfile)
	return chat.OwnerProfileIdentityReply(content, owner, profile, channel)
}
