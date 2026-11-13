package handlers

import (
	"context"

	chat "read_books/dokja_domain/dokja_chat"
	communications "read_books/dokja_domain/dokja_communications"
	conversation "read_books/dokja_domain/dokja_conversation"
	"read_books/internal/core"
	"read_books/internal/logger"
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

// WithVisions gives the handler Ningo's convictions from the operator's private visions file. Unlike
// the private facts they describe Ningo himself, so every turn carries them.
func (h *ChatDomainHandler) WithVisions(visions string) *ChatDomainHandler {
	h.visions = visions
	return h
}

// WithWarmth gives the handler the operator's private description of how Ningo talks with the
// people marked warm. Without it those people get the usual voice.
func (h *ChatDomainHandler) WithWarmth(manner string) *ChatDomainHandler {
	h.warm = manner
	return h
}

// SpeakerSessions remembers which stored session belongs to a person the application recognized.
type SpeakerSessions interface {
	NoteSession(ctx context.Context, key, person string) error
}

// WithSpeakerSessions notes, for the owner's Profiles, the WhatsApp sessions of the people the
// application recognized by number. A WhatsApp session holds one sender's messages and nothing in
// it says who that is, so without the note its exchanges could not be found again. Discord needs
// none: its history carries each account's alias.
func (h *ChatDomainHandler) WithSpeakerSessions(sessions SpeakerSessions) *ChatDomainHandler {
	h.speakers = sessions
	return h
}

// noteSpeaker records the session's person once per process; the store keeps it across restarts.
func (h *ChatDomainHandler) noteSpeaker(event core.Event, person string) {
	key := chatSessionBaseKey(event)
	if h.speakers == nil || key == "" || event.Source != core.SourceWhatsApp {
		return
	}
	if noted, _ := h.notedSpeakers.LoadOrStore(key, person); noted == person {
		if _, loaded := h.notedSpeakers.Load(key + "\x00done"); loaded {
			return
		}
	}
	h.notedSpeakers.Store(key, person)
	if err := h.speakers.NoteSession(context.Background(), key, person); err != nil {
		logger.Error("note a recognized speaker's session", err)
		return
	}
	h.notedSpeakers.Store(key+"\x00done", true)
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
			h.noteSpeaker(event, conversation.PersonKey(person.Name))
			if person.Warm && h.warm != "" {
				event.Context["warm_manner"] = h.warm
			}
		}
	}
	if len(people) > 0 {
		event.Context["known_people"] = people
	}
}

// knownDiscordName is the name the owner gave a Discord account in their private configuration: what
// to call the person and, for a nickname, their real name. It matches the gateway's numeric account
// id and nothing else: a display name, a handle or a message claiming to be someone never matches.
// Only names are returned, so About, Warm and kept facts stay where privateFactsAllowed puts them,
// and the id itself never reaches the model.
func (h *ChatDomainHandler) knownDiscordName(userID string) chat.KnownName {
	if userID == "" {
		return chat.KnownName{}
	}
	for _, person := range h.ningo.People {
		if person.Discord == userID {
			return chat.KnownName{Name: person.Name, RealName: person.RealName}
		}
	}
	return chat.KnownName{}
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
	for _, granted := range []string{"owner_interface", "ningo_visions", "ningo_quiz_runs", "ningo_named_by", "known_people", "known_speaker", "warm_manner", "known_facts", "facts_available", "fact_in_message"} {
		delete(event.Context, granted)
	}
	if h.visions != "" {
		event.Context["ningo_visions"] = h.visions
	}
	h.attachPrivateFacts(event)
	if event.Source != core.SourceDiscord {
		delete(event.Context, "owner_profile")
	}
	channel, owner := ownerChannel(event)
	if owner && (channel == chat.OwnerByWhatsApp || channel == chat.OwnerByWhatsAppGroup) {
		h.noteSpeaker(event, conversation.OwnerPerson)
	}
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
