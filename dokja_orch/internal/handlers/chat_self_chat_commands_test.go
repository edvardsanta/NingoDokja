package handlers

import (
	"context"

	"sort"
	"strings"
	"testing"
	"time"

	conversation "read_books/dokja_domain/dokja_conversation"
)

// selfChat sends one self-chat message and gives the reply.
func selfChat(t *testing.T, handler *ChatDomainHandler, content string) string {
	t.Helper()
	result, err := handler.Handle(context.Background(), whatsappTurn(content, "", true), archiveStep)
	if err != nil {
		t.Fatal(content, err)
	}
	reply, _ := result["reply"].(string)
	return reply
}

func TestBothCommandCatalogsHoldTheSameMessages(t *testing.T) {
	keys := func(locale string) []string {
		var names []string
		for key, text := range selfChatCatalogs[locale] {
			if strings.TrimSpace(text) == "" {
				t.Errorf("%s: %s is empty", locale, key)
			}
			names = append(names, key)
		}
		sort.Strings(names)
		return names
	}
	if pt, en := keys("pt"), keys("en"); strings.Join(pt, ",") != strings.Join(en, ",") {
		t.Fatalf("catalogs differ:\npt %v\nen %v", pt, en)
	}
}

func TestACommandAnswersInTheLanguageOfItsName(t *testing.T) {
	handler := NewChatDomainHandler(&scriptedModel{}).WithArchive(archiveFor(t))
	if reply := selfChat(t, handler, "/ajuda"); !strings.Contains(reply, "/nova — começa uma conversa nova") {
		t.Fatal(reply)
	}
	if reply := selfChat(t, handler, "/HELP"); !strings.Contains(reply, "/new — start a new conversation") {
		t.Fatal(reply)
	}
}

func TestNewStartsTheConversationTheNextMessageGoesTo(t *testing.T) {
	archive := archiveFor(t)
	model := &scriptedModel{answers: []map[string]any{{"reply": "First."}, {"reply": "Fresh."}}}
	handler := NewChatDomainHandler(model).WithArchive(archive)
	selfChat(t, handler, "Old topic")
	if reply := selfChat(t, handler, "/nova"); reply != "Conversa nova começada. A próxima mensagem já vai para ela." {
		t.Fatal(reply)
	}
	selfChat(t, handler, "New topic")
	items := listed(t, archive)
	if len(items) != 2 || items[0].Turns != 1 || items[1].Turns != 1 {
		t.Fatal("the next message must go to the new conversation", items)
	}
	if turns := turnsOf(t, archive, items[0].ID); turns[0].Content != "New topic" {
		t.Fatal(turns)
	}
}

func TestTheListIsNumberedAndOpeningMovesThePhoneThere(t *testing.T) {
	archive := archiveFor(t)
	model := &scriptedModel{answers: []map[string]any{{"reply": "a"}, {"reply": "b"}, {"reply": "c"}}}
	handler := NewChatDomainHandler(model).WithArchive(archive)
	justice, _ := archive.Start(context.Background(), conversation.OwnerScope, conversation.KindGeneral, "", "Justice")
	selfChat(t, handler, "About justice")
	archive.Start(context.Background(), conversation.OwnerScope, conversation.KindReading, "item-1", "A reading")
	selfChat(t, handler, "/nova")
	selfChat(t, handler, "About power")

	list := selfChat(t, handler, "/conversas")
	lines := strings.Split(list, "\n")
	if lines[0] != "Suas últimas conversas:" || !strings.HasPrefix(lines[1], "1. About power · 1 mensagens · ") || !strings.HasSuffix(lines[1], " (atual)") ||
		!strings.HasPrefix(lines[2], "2. Justice · 1 mensagens · ") || strings.Contains(list, "A reading") {
		t.Fatalf("unexpected list:\n%s", list)
	}
	if reply := selfChat(t, handler, "/abrir 2"); reply != "Agora continuamos em: Justice" {
		t.Fatal(reply)
	}
	selfChat(t, handler, "Back to justice")
	if turns := turnsOf(t, archive, justice.ID); len(turns) != 2 || turns[1].Content != "Back to justice" {
		t.Fatal("the phone must continue the opened conversation", turns)
	}
}

func TestOpeningNeedsARecentListAndAKnownNumber(t *testing.T) {
	archive := archiveFor(t)
	handler := NewChatDomainHandler(&scriptedModel{answers: []map[string]any{{"reply": "a"}}}).WithArchive(archive)
	if reply := selfChat(t, handler, "/open 1"); reply != "Send /conversations first, then use the number from the list." {
		t.Fatal(reply)
	}
	selfChat(t, handler, "Something")
	selfChat(t, handler, "/conversations")
	for _, bad := range []string{"/open 3", "/open x", "/open"} {
		if reply := selfChat(t, handler, bad); !strings.HasPrefix(reply, "There is no conversation") {
			t.Fatal(bad, reply)
		}
	}
	handler.selfChatState.listedAt = time.Now().Add(-selfChatListLifetime - time.Second)
	if reply := selfChat(t, handler, "/open 1"); !strings.HasPrefix(reply, "Send /conversations first") {
		t.Fatal("an old list's numbers must not be used", reply)
	}
}

func TestRenameNamesTheCurrentConversation(t *testing.T) {
	archive := archiveFor(t)
	handler := NewChatDomainHandler(&scriptedModel{answers: []map[string]any{{"reply": "a"}}}).WithArchive(archive)
	if reply := selfChat(t, handler, "/renomear Algo"); reply != "Ainda não há conversa para renomear. Mande uma mensagem ou use /nova." {
		t.Fatal(reply)
	}
	selfChat(t, handler, "Topic")
	if reply := selfChat(t, handler, "/renomear   Justiça   e poder "); reply != "Conversa renomeada para: Justiça e poder" {
		t.Fatal(reply)
	}
	if items := listed(t, archive); items[0].Title != "Justiça e poder" {
		t.Fatal(items)
	}
	if reply := selfChat(t, handler, "/renomear"); !strings.HasPrefix(reply, "Diga o novo título") {
		t.Fatal(reply)
	}
	if reply := selfChat(t, handler, "/renomear "+strings.Repeat("x", 121)); !strings.HasPrefix(reply, "Esse título é longo demais") {
		t.Fatal(reply)
	}
}

func TestDeletingAsksFirstAndOnlyAConfirmationInTimeDeletes(t *testing.T) {
	archive := archiveFor(t)
	handler := NewChatDomainHandler(&scriptedModel{answers: []map[string]any{{"reply": "a"}, {"reply": "b"}}}).WithArchive(archive)
	selfChat(t, handler, "Keep me")
	selfChat(t, handler, "/nova")
	selfChat(t, handler, "Delete me")
	selfChat(t, handler, "/conversas")

	if reply := selfChat(t, handler, "/apagar 1"); !strings.HasPrefix(reply, "Apagar “Delete me”?") || !strings.Contains(reply, "/sim") {
		t.Fatal(reply)
	}
	if len(listed(t, archive)) != 2 {
		t.Fatal("asking must not delete")
	}
	if reply := selfChat(t, handler, "/sim"); reply != "Conversa apagada: Delete me" {
		t.Fatal(reply)
	}
	items := listed(t, archive)
	if len(items) != 1 || items[0].Title != "Keep me" {
		t.Fatal(items)
	}
	if reply := selfChat(t, handler, "/sim"); reply != "Não há nada esperando confirmação." {
		t.Fatal("a confirmation is used once", reply)
	}

	selfChat(t, handler, "/conversas")
	selfChat(t, handler, "/apagar 1")
	handler.selfChatState.askedAt = time.Now().Add(-selfChatConfirmTime - time.Second)
	if reply := selfChat(t, handler, "/sim"); reply != "Não há nada esperando confirmação." || len(listed(t, archive)) != 1 {
		t.Fatal("a late confirmation must not delete", reply)
	}
}

func TestACommandOnAConversationChangedSinceTheListDoesNothing(t *testing.T) {
	archive := archiveFor(t)
	model := &scriptedModel{answers: []map[string]any{{"reply": "a"}, {"reply": "b"}}}
	handler := NewChatDomainHandler(model).WithArchive(archive)
	selfChat(t, handler, "Topic")
	selfChat(t, handler, "/conversas")
	selfChat(t, handler, "/apagar 1")
	selfChat(t, handler, "Another turn") // the conversation moves on after it was listed
	if reply := selfChat(t, handler, "/sim"); !strings.HasPrefix(reply, "Essa conversa mudou em outro lugar") {
		t.Fatal(reply)
	}
	if items := listed(t, archive); len(items) != 1 || items[0].Turns != 2 {
		t.Fatal("a changed conversation must not be deleted", items)
	}
}

func TestCommandsAreNotTurnsAndOnlyTheSelfChatHasThem(t *testing.T) {
	archive := archiveFor(t)
	model := &scriptedModel{answers: []map[string]any{{"reply": "Plain text to me."}, {"reply": "Also plain."}}}
	handler := NewChatDomainHandler(model).WithArchive(archive)
	selfChat(t, handler, "/nova")
	selfChat(t, handler, "/conversas")
	if items := listed(t, archive); len(items) != 1 || items[0].Turns != 0 {
		t.Fatal("commands must not become turns", items)
	}
	result, err := handler.Handle(context.Background(), whatsappTurn("/apagar 1", "MSG1", false), archiveStep)
	if err != nil || result["reply"] != "Plain text to me." || result["command"] == true {
		t.Fatal("a contact's slash text is an ordinary message", result, err)
	}
	if reply := selfChat(t, handler, "/unknown thing"); reply != "Also plain." {
		t.Fatal("an unknown slash word is an ordinary message", reply)
	}
	if len(listed(t, archive)) != 1 {
		t.Fatal("a contact cannot delete")
	}

}
