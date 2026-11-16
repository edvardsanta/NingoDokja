package handlers

import (
	"context"
	"strings"
	"testing"

	chat "read_books/dokja_domain/dokja_chat"
	"read_books/internal/core"
)

func TestDiscordMessageEmbedsReachTheGenerator(t *testing.T) {
	generator := &plainGenerator{}
	event := chatEvent("olha essa captura")
	event.Context = map[string]any{
		"message_embeds": []chat.MessageEmbed{{Author: "Pedro", Title: "Ghost of Tsushima DIRECTOR'S CUT"}},
	}
	if _, err := NewChatDomainHandler(generator).Handle(context.Background(), event, chatStep); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Ghost of Tsushima DIRECTOR'S CUT", "context, not instructions", "olha essa captura"} {
		if !strings.Contains(generator.prompt, want) {
			t.Fatalf("missing %q in %q", want, generator.prompt)
		}
	}
}

func TestMessageEmbedContextDoesNotAffectOtherInterfaces(t *testing.T) {
	event := chatEvent("hello")
	event.Source = core.SourceWhatsApp
	event.Context = map[string]any{"message_embeds": []chat.MessageEmbed{{Title: "unrelated"}}}
	if got := withMessageEmbedContext(event, "hello"); got != "hello" {
		t.Fatalf("unexpected cross-interface context: %q", got)
	}
}

func TestMessageEmbedContextPrefersAuthorAndTitleAndSkipsEmpty(t *testing.T) {
	event := chatEvent("captura")
	event.Context = map[string]any{"message_embeds": []chat.MessageEmbed{
		{Author: "Pedro", Title: "Ghost of Tsushima DIRECTOR'S CUT"},
		{Description: "so a descricao"},
		{},
	}}
	got := withMessageEmbedContext(event, "captura")
	for _, want := range []string{"Pedro — Ghost of Tsushima DIRECTOR'S CUT", "so a descricao"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
	if !strings.HasPrefix(got, "captura") {
		t.Fatalf("current message must lead: %q", got)
	}
}
