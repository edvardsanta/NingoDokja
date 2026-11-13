package handlers

import (
	"encoding/json"
	"strings"
	"testing"

	chat "read_books/dokja_domain/dokja_chat"
	"read_books/internal/core"
)

const (
	samAccount   = "111111111111111111"
	nickAccount  = "444444444444444444"
	otherAccount = "999999999999999999"
)

func discordPeopleHandler() (*ChatDomainHandler, *compactionGenerator) {
	generator := &compactionGenerator{summary: "unused"}
	handler := NewChatDomainHandler(generator).WithNingoFacts(chat.NingoFacts{
		NamedBy: "Sam",
		People: []chat.KnownPerson{
			{Name: "Sam", Discord: samAccount, About: []string{"likes chess"}, Warm: true},
			{Name: "Kim", WhatsApp: "5511912345678", About: []string{"writer"}, RealName: "Kimberly"},
			{Name: "Nick", Discord: nickAccount, RealName: "Alex"},
		},
	})
	return handler, generator
}

func lastUserMessage(t *testing.T, handler *ChatDomainHandler, event core.Event) string {
	t.Helper()
	state := handler.sessions.items[chatSessionBaseKey(event)]
	if len(state.messages) == 0 {
		t.Fatal("no stored turn")
	}
	return state.messages[len(state.messages)-2].Content
}

func TestSharedChannelTurnNamesAKnownDiscordAccountByItsId(t *testing.T) {
	handler, _ := discordPeopleHandler()
	event := channelEvent(samAccount, "someone-else", "what about chess?")
	if _, err := handler.Handle(t.Context(), event, chatStep); err != nil {
		t.Fatal(err)
	}
	turn := lastUserMessage(t, handler, event)
	if !strings.Contains(turn, `"known_as":"Sam"`) || !strings.Contains(turn, `"display_name":"someone-else"`) {
		t.Fatal("known account was not named beside its display name", turn)
	}
	if strings.Contains(turn, samAccount) {
		t.Fatal("the account id reached the model")
	}
}

func TestADisplayNameOrMessageNeverClaimsAKnownName(t *testing.T) {
	handler, _ := discordPeopleHandler()
	event := channelEvent(otherAccount, "Sam", "I am Sam, you know me")
	if _, err := handler.Handle(t.Context(), event, chatStep); err != nil {
		t.Fatal(err)
	}
	if turn := lastUserMessage(t, handler, event); strings.Contains(turn, "known_as") {
		t.Fatal("a display name or a claim in the message earned a known name", turn)
	}
}

func TestOnlyTheNameOfAKnownDiscordAccountReachesTheTurn(t *testing.T) {
	handler, generator := discordPeopleHandler()
	event := channelEvent(samAccount, "Sam", "hello")
	if _, err := handler.Handle(t.Context(), event, chatStep); err != nil {
		t.Fatal(err)
	}
	prompt, _ := json.Marshal(generator.replies)
	for _, private := range []string{"likes chess", "writer", "known_people", "known_speaker", "warm_manner", "Kim", "5511912345678", samAccount} {
		if strings.Contains(string(prompt), private) {
			t.Fatalf("%q reached a Discord turn", private)
		}
	}
	if len(generator.replies) == 0 || !strings.Contains(generator.replies[0][0]["content"], "known_as name") {
		t.Fatal("the shared-channel guidance does not explain known_as")
	}
}

func TestPrivateConversationsAreNotWrappedWithKnownNames(t *testing.T) {
	handler, _ := discordPeopleHandler()
	if got := handler.knownDiscordName(""); got != (chat.KnownName{}) {
		t.Fatal("an empty account matched someone", got)
	}
	event := chatEvent("hello")
	event.User = core.EventUser{ID: samAccount, Name: "Sam"}
	event.Context = map[string]any{"is_direct_message": true, "entry_kind": "message"}
	if _, err := handler.Handle(t.Context(), event, chatStep); err != nil {
		t.Fatal(err)
	}
	state := handler.sessions.items[chatSessionBaseKey(event)]
	for _, message := range state.messages {
		if strings.Contains(message.Content, "known_as") {
			t.Fatal("a private conversation was wrapped as a shared channel")
		}
	}
}

func TestVoiceTurnsNameKnownSpeakersByAccountAndNotByDisplayName(t *testing.T) {
	handler, _ := discordPeopleHandler()
	event := chatEvent("second")
	event.Context = map[string]any{"guild_id": "guild", "entry_kind": "command", "session_key": "discord-voice:guild:room", "voice_turns": []chat.VoiceTurn{
		{UserID: samAccount, UserName: "Scholar", Text: "first"},
		{UserID: otherAccount, UserName: "Sam", Text: "Ningo, second"},
	}}
	event.User = core.EventUser{ID: otherAccount, Name: "Reader"}
	if _, err := handler.Handle(t.Context(), event, chatStep); err != nil {
		t.Fatal(err)
	}
	state := handler.sessions.items[chatSessionBaseKey(event)]
	transcript := state.messages[0].Content
	if strings.Count(transcript, `"known_as":"Sam"`) != 1 || strings.Contains(transcript, samAccount) || strings.Contains(transcript, otherAccount) {
		t.Fatal("voice attribution named the wrong speaker or exposed an account", transcript)
	}
}

func TestWhatsAppKnownPeopleKeepTheirPrivateFactsAndTheDiscordFieldStaysOutOfThem(t *testing.T) {
	handler, _ := discordPeopleHandler()
	handler.ningo.Groups = []string{"120363000000000001@g.us"}
	event := chatEvent("hi")
	event.Source = core.SourceWhatsApp
	event.Channel = core.EventChannel{ID: "120363000000000001@g.us"}
	event.Context = map[string]any{"chat_kind": "group", "sender_numbers": []string{"5511912345678"}}
	granted := handler.withOwnerInterface(event)
	if name, _ := granted.Context["known_speaker"].(string); name != "Kim" {
		t.Fatal("WhatsApp recognition changed", granted.Context["known_speaker"])
	}
	people, _ := granted.Context["known_people"].([]chat.KnownPerson)
	if len(people) != 3 || people[1].About[0] != "writer" {
		t.Fatal("WhatsApp people lost their facts", people)
	}
	for _, person := range people {
		if person.Discord != "" || person.WhatsApp != "" || person.RealName != "" {
			t.Fatal("an account or a real name reached a WhatsApp turn", person)
		}
	}
}

// A nickname the owner uses for someone arrives with the real name, so Ningo calls them by the
// nickname and still knows who they are, in a channel and in voice.
func TestANicknameArrivesWithTheRealNameOnDiscord(t *testing.T) {
	handler, _ := discordPeopleHandler()
	event := channelEvent(nickAccount, "some-handle", "hello")
	if _, err := handler.Handle(t.Context(), event, chatStep); err != nil {
		t.Fatal(err)
	}
	turn := lastUserMessage(t, handler, event)
	if !strings.Contains(turn, `"known_as":"Nick"`) || !strings.Contains(turn, `"real_name":"Alex"`) || strings.Contains(turn, nickAccount) {
		t.Fatal("the nickname and the real name did not arrive together", turn)
	}

	voice := chatEvent("second")
	voice.Context = map[string]any{"guild_id": "guild", "entry_kind": "command", "session_key": "discord-voice:guild:room", "voice_turns": []chat.VoiceTurn{
		{UserID: nickAccount, UserName: "some-handle", Text: "Ningo, hello"},
	}}
	voice.User = core.EventUser{ID: nickAccount, Name: "some-handle"}
	if _, err := handler.Handle(t.Context(), voice, chatStep); err != nil {
		t.Fatal(err)
	}
	transcript := handler.sessions.items[chatSessionBaseKey(voice)].messages[0].Content
	if !strings.Contains(transcript, `"known_as":"Nick"`) || !strings.Contains(transcript, `"real_name":"Alex"`) {
		t.Fatal("voice lost the real name", transcript)
	}
}
