package handlers

import (
	chat "read_books/dokja_domain/dokja_chat"
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

// WithNamedBy gives the handler who chose Ningo's name, a fact kept in the private configuration.
func (h *ChatDomainHandler) WithNamedBy(name string) *ChatDomainHandler {
	h.namedBy = name
	return h
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
	return "", false
}

// withOwnerInterface marks a turn from the owner's own interface and attaches the profile. What an
// interface put in the context under these names is dropped first: only the handler grants them, and
// only the Discord gateway attaches a profile for Discord.
func (h *ChatDomainHandler) withOwnerInterface(event core.Event) core.Event {
	event.Context = cloneChatContext(event.Context)
	delete(event.Context, "owner_interface")
	delete(event.Context, "ningo_named_by")
	if h.namedBy != "" {
		event.Context["ningo_named_by"] = h.namedBy
	}
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
