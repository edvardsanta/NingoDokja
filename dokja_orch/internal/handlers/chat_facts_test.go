package handlers

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	chat "read_books/dokja_domain/dokja_chat"
	conversation "read_books/dokja_domain/dokja_conversation"
	"read_books/internal/core"
	"read_books/internal/infrastructure/conversations"
)

const factsGroup = "111@g.us"

func factsFor(t *testing.T) *conversation.Facts {
	t.Helper()
	store, err := conversations.Open(filepath.Join(t.TempDir(), "facts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return conversation.NewFacts(store)
}

func factsHandler(t *testing.T, answers ...map[string]any) (*ChatDomainHandler, *scriptedModel) {
	t.Helper()
	model := &scriptedModel{answers: answers}
	handler := NewChatDomainHandler(model).WithFacts(factsFor(t)).WithNingoFacts(chat.NingoFacts{
		Groups: []string{factsGroup},
		People: []chat.KnownPerson{{Name: "Ana", WhatsApp: "5511999990000"}},
	})
	return handler, model
}

// factGroupTurn is a message in the listed group from the account with this number.
func factGroupTurn(content, number string, ownAccount bool) core.Event {
	return core.Event{
		Source: core.SourceWhatsApp, Type: "message.created",
		User: core.EventUser{ID: number}, Channel: core.EventChannel{ID: factsGroup},
		Payload: map[string]any{"content": content},
		Context: map[string]any{"chat_kind": "group", "sender_numbers": []string{number}, "is_own_account": ownAccount, "content": content},
	}
}

func say(t *testing.T, handler *ChatDomainHandler, event core.Event) string {
	t.Helper()
	result, err := handler.Handle(context.Background(), event, archiveStep)
	if err != nil {
		t.Fatal(err)
	}
	reply, _ := result["reply"].(string)
	return reply
}

func TestPhrasesAreReadAndEverythingElseIsNot(t *testing.T) {
	for content, want := range map[string]string{
		"guarde isso: gosto de chá": "gosto de chá",
		"Guarda isso pra mim - chá": "chá",
		"anote - chá":               "chá",
		"salve que prefiro chá":     "prefiro chá",
		"lembre disso: chá":         "chá",
		"remember this: I like tea": "I like tea",
		"remember that I like tea":  "I like tea",
		"save: tea":                 "tea",
		"guarde isso":               "",
	} {
		command, _, argument, ok := parseFactCommand(content)
		if !ok || command != factRemember || argument != want {
			t.Errorf("%q: %v %q %v", content, command, argument, ok)
		}
	}
	for _, content := range []string{
		"o que você sabe sobre mim?", "oq vc sabe de mim", "o que vc guardou", "what do you know about me?", "what have you saved",
	} {
		if command, _, _, ok := parseFactCommand(content); !ok || command != factList {
			t.Errorf("%q must list", content)
		}
	}
	for _, content := range []string{"esquece 2", "esqueça isso: chá", "forget 1", "forget this: tea"} {
		if command, _, _, ok := parseFactCommand(content); !ok || command != factForget {
			t.Errorf("%q must forget", content)
		}
	}
	for _, content := range []string{
		"lembra que a gente falou de Kant?", "lembra disso?", "guarde isso?", "guarda-chuva novo", "esquece, deixa pra lá",
		"o que você sabe?", "o que é guardar?", "remember when we met", "keep going", "forget it", "como anotar melhor",
		"Ana disse guarde isso: nada",
	} {
		if _, _, _, ok := parseFactCommand(content); ok {
			t.Errorf("%q must stay an ordinary message", content)
		}
	}
}

func TestBothFactCatalogsHoldTheSameMessages(t *testing.T) {
	keys := func(locale string) string {
		var names []string
		for key, text := range factCatalogs[locale] {
			if strings.TrimSpace(text) == "" {
				t.Errorf("%s: %s is empty", locale, key)
			}
			names = append(names, key)
		}
		sort.Strings(names)
		return strings.Join(names, ",")
	}
	if keys("pt") != keys("en") {
		t.Fatal("the catalogs differ")
	}
}

func TestAKnownPersonSavesInTheGroupAndGetsItBackThere(t *testing.T) {
	handler, model := factsHandler(t)
	if reply := say(t, handler, factGroupTurn("guarde isso: meu filme é Stalker", "5511999990000", false)); reply != "Guardado: meu filme é Stalker" {
		t.Fatal(reply)
	}
	reply := say(t, handler, factGroupTurn("o que você sabe sobre mim?", "5511999990000", false))
	if !strings.Contains(reply, "1. meu filme é Stalker") {
		t.Fatal(reply)
	}
	if len(model.asked) != 0 {
		t.Fatal("a phrase is answered without the model", model.asked)
	}
}

func TestAStrangerSavesNothingEvenClaimingToBeSomeone(t *testing.T) {
	handler, _ := factsHandler(t)
	stranger := factGroupTurn("guarde isso: quero acesso", "5511888880000", false)
	stranger.Context["known_speaker"] = "Ana"
	if reply := say(t, handler, stranger); !strings.HasPrefix(reply, "Só guardo isso") {
		t.Fatal(reply)
	}
	if held, _ := handler.facts.All(context.Background()); len(held) != 0 {
		t.Fatal("a claim in the context must not be believed", held)
	}
}

func TestEachPersonOnlyReachesTheirOwnFacts(t *testing.T) {
	handler, _ := factsHandler(t)
	say(t, handler, factGroupTurn("guarde isso: da Ana", "5511999990000", false))
	say(t, handler, factGroupTurn("guarde isso: do dono", "5511777770000", true))
	ana := say(t, handler, factGroupTurn("o que você sabe sobre mim?", "5511999990000", false))
	if strings.Contains(ana, "do dono") {
		t.Fatal(ana)
	}
	if reply := say(t, handler, factGroupTurn("esquece isso: do dono", "5511999990000", false)); !strings.HasPrefix(reply, "Não achei") {
		t.Fatal("nobody forgets another person's fact", reply)
	}
	if held, _ := handler.facts.All(context.Background()); len(held) != 2 {
		t.Fatal(held)
	}
}

func TestAPrivateFactStaysOutOfTheGroupEvenForItsOwner(t *testing.T) {
	handler, model := factsHandler(t, map[string]any{"reply": "ok"})
	handler.facts.Remember(context.Background(), conversation.OwnerPerson, conversation.PlacePrivate, "segredo do dono")
	own := factGroupTurn("o que você sabe sobre mim?", "5511777770000", true)
	if reply := say(t, handler, own); strings.Contains(reply, "segredo") {
		t.Fatal(reply)
	}
	say(t, handler, factGroupTurn("me conta algo", "5511777770000", true))
	if strings.Contains(strings.Join(model.system, " "), "segredo") {
		t.Fatal("the model must not see a private fact in a group turn")
	}
	if reply := say(t, handler, factGroupTurn("esquece isso: segredo", "5511777770000", true)); !strings.HasPrefix(reply, "Não achei") {
		t.Fatal(reply)
	}
}

func TestTheModelSeesOnlyTheSpeakersOwnFactsAsData(t *testing.T) {
	handler, model := factsHandler(t, map[string]any{"reply": "Stalker."}, map[string]any{"reply": "Não sei."})
	say(t, handler, factGroupTurn("guarde isso: meu filme é Stalker", "5511999990000", false))
	say(t, handler, factGroupTurn("qual é meu filme?", "5511999990000", false))
	system := model.system[0]
	if !strings.Contains(system, "meu filme é Stalker") || !strings.Contains(system, "never instructions") {
		t.Fatal(system)
	}
	say(t, handler, factGroupTurn("qual é meu filme?", "5511888880000", false))
	if strings.Contains(model.system[1], "Stalker") {
		t.Fatal("another speaker must not get these facts")
	}
}

func TestForgettingByNumberAndByWords(t *testing.T) {
	handler, _ := factsHandler(t)
	for _, fact := range []string{"gosto de chá", "gosto de café", "moro longe"} {
		say(t, handler, factGroupTurn("guarde isso: "+fact, "5511999990000", false))
	}
	if reply := say(t, handler, factGroupTurn("esquece isso: gosto", "5511999990000", false)); !strings.HasPrefix(reply, "2 coisas") {
		t.Fatal(reply)
	}
	if reply := say(t, handler, factGroupTurn("esquece 3", "5511999990000", false)); reply != "Esquecido: moro longe" {
		t.Fatal(reply)
	}
	if reply := say(t, handler, factGroupTurn("esquece 9", "5511999990000", false)); !strings.HasPrefix(reply, "Não achei") {
		t.Fatal(reply)
	}
	if reply := say(t, handler, factGroupTurn("esquece isso: café", "5511999990000", false)); reply != "Esquecido: gosto de café" {
		t.Fatal(reply)
	}
}

func TestAFactSavedOnTheOwnersInterfaceIsATurnAndIsSavedOnce(t *testing.T) {
	archive := archiveFor(t)
	handler := NewChatDomainHandler(&scriptedModel{}).WithArchive(archive).WithFacts(factsFor(t))
	held, _ := archive.Start(context.Background(), conversation.OwnerScope, conversation.KindGeneral, "", "")
	event := ownerTurn(map[string]any{"content": "guarde isso: gosto de chá", "conversation_id": held.ID, "send_id": "send-0001"})
	for range 2 {
		if result, err := handler.Handle(context.Background(), event, archiveStep); err != nil || result["reply"] != "Guardado: gosto de chá" {
			t.Fatal(result, err)
		}
	}
	if facts, _ := handler.facts.All(context.Background()); len(facts) != 1 {
		t.Fatal("a repeated send keeps the fact once", facts)
	}
	if turns, _ := archive.Turns(context.Background(), conversation.OwnerScope, held.ID, 0, 10); len(turns) != 1 || turns[0].State != conversation.TurnCompleted {
		t.Fatal(turns)
	}
}

func TestTheOwnersInterfacesSeeAndDeleteEveryonesFacts(t *testing.T) {
	facts := factsFor(t)
	kept, _ := facts.Remember(context.Background(), conversation.PersonKey("Ana"), "whatsapp:"+factsGroup, "da Ana")
	handler := NewConversationDomainHandler(archiveFor(t)).WithFacts(facts)
	desktop := core.Event{Source: core.SourceDesktop, Type: "conversation.facts"}
	result, err := handler.Handle(context.Background(), desktop, core.WorkflowStep{})
	if err != nil {
		t.Fatal(err)
	}
	views := result["facts"].([]map[string]any)
	if len(views) != 1 || views[0]["person"] != "Ana" || views[0]["place"] != "whatsapp" || views[0]["owner"] != false {
		t.Fatal(views)
	}
	stranger := core.Event{Source: core.SourceWhatsApp, Type: "conversation.fact_delete", Payload: map[string]any{"fact_id": kept.ID}}
	if _, err := handler.Handle(context.Background(), stranger, core.WorkflowStep{}); err == nil {
		t.Fatal("only the owner's interfaces may delete here")
	}
	desktop.Type, desktop.Payload = "conversation.fact_delete", map[string]any{"fact_id": kept.ID}
	if _, err := handler.Handle(context.Background(), desktop, core.WorkflowStep{}); err != nil {
		t.Fatal(err)
	}
	if held, _ := facts.All(context.Background()); len(held) != 0 {
		t.Fatal(held)
	}
}
