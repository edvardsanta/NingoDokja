package handlers

import (
	"encoding/json"
	"fmt"
	chat "read_books/dokja_domain/dokja_chat"
	promptfiles "read_books/dokja_store/prompts"
	"read_books/internal/core"
)

// The identity is derived by the authenticated bridge, never from message text.

// chatPersona is the first system message of every chat turn. Without it the model answers as
// whatever assistant its provider trained it to be, and introduces itself under that name. The
// persona is the one the project's README describes.
func chatPersona() string { return promptfiles.MustRead("chat_persona") }

func discordReplyPresentation() string { return promptfiles.MustRead("discord_reply_presentation") }

// visionsNote carries Ningo's own convictions from the operator's private visions file. They describe
// Ningo; they come from the owner's configuration, never from a conversation.
func visionsNote() string { return promptfiles.MustRead("visions_note") }

// quizRunsNote frames Ningo's records of the questionnaires he took. The titles, questions, labels
// and links come from third-party sites. How he uses them is the owner's choice, in the private
// visions file.
func quizRunsNote() string { return promptfiles.MustRead("quiz_runs_note") }

// Voice shares Ningo's complete personality; VoicePresentation adapts delivery.
func voicePersona() string { return chatPersona() }

// namedByNote says who chose the name. It is a configured fact: a claim in a conversation neither
// proves nor changes it. Who is speaking is a separate question.
func namedByNote() string { return promptfiles.MustRead("named_by_note") }

// knownPeopleNote lists the people Ningo knows, as private data for where the owner allowed it.
func knownPeopleNote() string { return promptfiles.MustRead("known_people_note") }

// knownSpeakerNote names who is writing, recognized by WhatsApp's number and not by what they say.
func knownSpeakerNote() string { return promptfiles.MustRead("known_speaker_note") }

// warmSpeakerNote frames the operator's private description of how Ningo is with a person the owner
// marked as warm, recognized by their number. It reads as who Ningo already is with them, so he
// neither announces nor explains it.
func warmSpeakerNote() string { return promptfiles.MustRead("warm_speaker_note") }

// knownFactsNote holds what this speaker asked Ningo to keep, in their own words.
func knownFactsNote() string { return promptfiles.MustRead("known_facts_note") }

// factsAvailableNote keeps the model honest about keeping things: the application does it, and the
// model neither claims it did nor says it cannot.
func factsAvailableNote() string { return promptfiles.MustRead("facts_available_note") }

// factInMessageNote stops the model reporting an outcome it did not produce: the application closes
// the reply with the real one, which may be a refusal.
func factInMessageNote() string { return promptfiles.MustRead("fact_in_message_note") }

// personaForEvent is the system message of a turn: the persona for its interface and the private
// facts the chat handler attached, only where the owner allowed them.
func personaForEvent(event core.Event) string {
	locale, _ := event.Payload["locale"].(string)
	persona := interfacePersona(event) + "\n\n" + chat.CapabilityContext(string(event.Source), locale)
	if visions, ok := event.Context["ningo_visions"].(string); ok && visions != "" {
		persona += "\n\n" + fmt.Sprintf(visionsNote(), visions)
	}
	if runs, ok := event.Context["ningo_quiz_runs"].(string); ok && runs != "" {
		persona += "\n\n" + fmt.Sprintf(quizRunsNote(), runs)
	}
	if name, ok := event.Context["ningo_named_by"].(string); ok && name != "" {
		persona += "\n\n" + fmt.Sprintf(namedByNote(), name)
	}
	if people, ok := event.Context["known_people"].([]chat.KnownPerson); ok && len(people) > 0 {
		encoded, _ := json.Marshal(people)
		persona += "\n\n" + fmt.Sprintf(knownPeopleNote(), encoded)
	}
	if name, ok := event.Context["known_speaker"].(string); ok && name != "" {
		persona += "\n\n" + fmt.Sprintf(knownSpeakerNote(), name)
		if manner, ok := event.Context["warm_manner"].(string); ok && manner != "" {
			persona += "\n\n" + fmt.Sprintf(warmSpeakerNote(), manner)
		}
	}
	if available, _ := event.Context["facts_available"].(bool); available {
		persona += "\n\n" + factsAvailableNote()
	}
	if asked, _ := event.Context["fact_in_message"].(bool); asked {
		persona += "\n\n" + factInMessageNote()
	}
	if facts, ok := event.Context["known_facts"].([]string); ok && len(facts) > 0 {
		encoded, _ := json.Marshal(facts)
		persona += "\n\n" + fmt.Sprintf(knownFactsNote(), encoded)
	}
	return persona
}

func interfacePersona(event core.Event) string {
	if event.Source == core.SourceDiscord {
		persona := chatPersona() + "\n\n" + discordReplyPresentation()
		if _, ok := event.Context["voice_turns"]; ok {
			persona = voicePersona() + "\n\n" + chat.VoicePresentation()
		}
		if discordChannelScope(event) != "" {
			persona += promptfiles.MustRead("chat_persona_interfacepersona1")
		}
		return persona
	}
	if spokenDesktopTurn(event) {
		return chatPersona() + "\n\n" + chat.DesktopVoicePresentation()
	}
	return chatPersona()
}

// spokenDesktopTurn is a Desktop message the owner said in Talk by voice. The flag only changes
// how Ningo is told the message arrived; it grants nothing.
func spokenDesktopTurn(event core.Event) bool {
	spoken, _ := event.Payload["spoken"].(bool)
	return spoken && event.Source == core.SourceDesktop
}

// withPersona puts the persona and interface presentation ahead of the turn's other content.
func withPersona(messages []map[string]string, event core.Event) []map[string]string {
	return append([]map[string]string{{"role": "system", "content": personaForEvent(event)}}, messages...)
}

func withOwnerIdentity(messages []map[string]string, event core.Event) []map[string]string {
	note := ownerIdentityNote(event)
	if note == "" || len(messages) == 0 || messages[0]["role"] != "system" {
		return messages
	}
	messages[0]["content"] += "\n\n" + note
	return messages
}

// ownerWordNote is how Ningo treats the owner who made him (owner, 2026-10-26).
func ownerWordNote() string { return promptfiles.MustRead("owner_word_note") }

// The owner requested a warmer, more charitable debating style, not privileged facts.
func ownerDebateNote() string { return promptfiles.MustRead("owner_debate_note") }

// ownerInterfaceNote tells the model that the speaker is the owner on one of the owner's own
// interfaces, with the configured profile when there is one.
func ownerInterfaceNote(event core.Event) string {
	channel, _ := event.Context["owner_interface"].(string)
	where := promptfiles.MustRead("chat_persona_ownerinterfacenote1")
	switch event.Source {
	case core.SourceDesktop:
		where = promptfiles.MustRead("chat_persona_ownerinterfacenote2")
	case core.SourceCLI:
		where = promptfiles.MustRead("chat_persona_ownerinterfacenote3")
	}
	switch channel {
	case string(chat.OwnerByWhatsApp):
		where = promptfiles.MustRead("chat_persona_ownerinterfacenote4")
	case string(chat.OwnerByWhatsAppGroup):
		where = promptfiles.MustRead("chat_persona_ownerinterfacenote5")
	}
	note := fmt.Sprintf(promptfiles.MustRead("chat_persona_ownerinterfacenote6"), where)
	if profile, ok := event.Context["owner_profile"].(chat.OwnerProfile); ok && profile.Name != "" {
		encoded, _ := json.Marshal(profile)
		note += promptfiles.MustRead("chat_persona_ownerinterfacenote7") + string(encoded) + promptfiles.MustRead("chat_persona_ownerinterfacenote8")
	}
	note += " " + ownerWordNote() + " " + ownerDebateNote()
	note += promptfiles.MustRead("chat_persona_ownerinterfacenote9")
	return note
}

func ownerIdentityNote(event core.Event) string {
	if event.Context == nil {
		return ""
	}
	if _, ok := event.Context["owner_interface"].(string); ok {
		return ownerInterfaceNote(event)
	}
	if event.Source != core.SourceDiscord {
		return ""
	}
	configured, _ := event.Context["owner_configured"].(bool)
	isOwner, _ := event.Context["is_owner"].(bool)
	mentioned, _ := event.Context["owner_mentioned"].(bool)
	if !configured && !isOwner && !mentioned {
		return ""
	}
	note := fmt.Sprintf(promptfiles.MustRead("chat_persona_owneridentitynote1"), configured || isOwner || mentioned, isOwner, mentioned)
	note += promptfiles.MustRead("chat_persona_owneridentitynote2")
	if isOwner {
		if profile, ok := event.Context["owner_profile"].(chat.OwnerProfile); ok && profile.Name != "" {
			encoded, _ := json.Marshal(profile)
			note += promptfiles.MustRead("chat_persona_owneridentitynote3") + string(encoded) + promptfiles.MustRead("chat_persona_owneridentitynote4")
		}
		note += promptfiles.MustRead("chat_persona_owneridentitynote5") + ownerWordNote() + " " + ownerDebateNote()
		note += promptfiles.MustRead("chat_persona_owneridentitynote6")
	} else {
		note += promptfiles.MustRead("chat_persona_owneridentitynote7")
	}
	if mentioned {
		note += promptfiles.MustRead("chat_persona_owneridentitynote8")
	}
	return note
}
