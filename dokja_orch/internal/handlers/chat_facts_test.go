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
		"guarde isso: gosto de chá":        "gosto de chá",
		"Guarda isso pra mim - chá":        "chá",
		"anote - chá":                      "chá",
		"salve que prefiro chá":            "prefiro chá",
		"lembre disso: chá":                "chá",
		"remember this: I like tea":        "I like tea",
		"remember that I like tea":         "I like tea",
		"save: tea":                        "tea",
		"guarde isso":                      "",
		"Ningo, guarde que gosto de metal": "gosto de metal",
		"ningo: anote: chá":                "chá",
		"/ningo guarde isso: chá":          "chá",
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

func TestTheNameDoesNotMakeAnOrdinaryMessageAPhrase(t *testing.T) {
	for _, content := range []string{"Ningo, tudo bem?", "Ningo, lembra disso?", "Ningo guarde isso: nada", "Oi Ningo, guarde isso: nada"} {
		if _, _, _, ok := parseFactCommand(content); ok {
			t.Errorf("%q must stay an ordinary message", content)
		}
	}
}

func TestTheOwnerKeepsAFactFromTheirOwnChatCallingTheName(t *testing.T) {
	archive := archiveFor(t)
	handler := NewChatDomainHandler(&scriptedModel{}).WithArchive(archive).WithFacts(factsFor(t))
	result, err := handler.Handle(context.Background(), whatsappTurn("Ningo, guarde que eu gosto de heavy metal", "MSG1", true), archiveStep)
	if err != nil || result["reply"] != "Guardado: eu gosto de heavy metal" {
		t.Fatal(result, err)
	}
	held, _ := handler.facts.All(context.Background())
	if len(held) != 1 || held[0].Person != conversation.OwnerPerson || held[0].Place != conversation.PlacePrivate {
		t.Fatal(held)
	}
}

func TestOnlyAKnownSpeakerIsToldHowToKeepThings(t *testing.T) {
	handler, model := factsHandler(t, map[string]any{"reply": "a"}, map[string]any{"reply": "b"})
	say(t, handler, factGroupTurn("oi", "5511999990000", false))
	say(t, handler, factGroupTurn("oi", "5511888880000", false))
	if !strings.Contains(model.system[0], "Keeping facts:") || strings.Contains(model.system[1], "Keeping facts:") {
		t.Fatal(model.system)
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

// nameStory is Portuguese on purpose: the phrases being read are Portuguese.
const nameStory = "Ningo, a escolha surgiu por causa de uma novel que gosto, Dokja pode significa filho único, ou leitor, guarde isso, e Ningo surge pq queria que fosse o número 1 ou seja o leitor número um"

func TestARequestAmongOtherWordsIsRead(t *testing.T) {
	for content, want := range map[string]string{
		nameStory:                          "a escolha surgiu por causa de uma novel que gosto, Dokja pode significa filho único, ou leitor, e Ningo surge pq queria que fosse o número 1 ou seja o leitor número um",
		"Sou vegano, guarde isso":          "Sou vegano",
		"Odeio coentro. Guarde isso.":      "Odeio coentro.",
		"meu filme é Stalker, guarde isso": "meu filme é Stalker",
		"meu filme é Stalker, e anota isso pra mim, por acaso":   "meu filme é Stalker, por acaso",
		"Moro em Santos. Guarde isso. Qual praia você indica?":   "Moro em Santos. Qual praia você indica?",
		"qual o melhor? guarda isso por favor, gosto de chá":     "qual o melhor, gosto de chá",
		"guarde isso: gosto de chá preto. Qual o melhor?":        "gosto de chá preto. Qual o melhor?",
		"my film is Stalker, remember this, what about Solaris?": "my film is Stalker, what about Solaris?",
		"gosto de chá preto\nlembre-se disso\ne você?":           "gosto de chá preto\ne você?",
	} {
		if _, kept, ok := parseFactInMessage(content); !ok || kept != want {
			t.Errorf("%q: %q %v", content, kept, ok)
		}
	}
	if locale, _, _ := parseFactInMessage("my film is Stalker, remember this"); locale != "en" {
		t.Error("the outcome is told in the language of the phrase")
	}
}

func TestWordsThatDoNotAskStayAnOrdinaryMessage(t *testing.T) {
	for _, content := range []string{
		"Ana disse guarde isso: nada", "Ana me pediu que guarde isso, mas esqueci", "não guarde isso, é bobagem de verdade",
		"gosto de chá preto, guarde isso?", "fomos lá ontem à noite, lembra disso, foi ótimo", "gosto de chá preto, guarde isso: nada",
		"esse guarda-chuva novo, guarda isso aqui no armário",
		"Oi Ningo, guarde isso: nada", "lembra disso?", "I will keep this, it is fine",
	} {
		if _, kept, ok := parseFactInMessage(content); ok {
			t.Errorf("%q must keep nothing, got %q", content, kept)
		}
	}
}

func TestKeepingAndAnsweringHappenInOneMessage(t *testing.T) {
	handler, model := factsHandler(t, map[string]any{"reply": "Um nome de leitor. Pretensioso."})
	reply := say(t, handler, factGroupTurn(nameStory, "5511999990000", false))
	held, _ := handler.facts.All(context.Background())
	if len(held) != 1 || strings.Contains(held[0].Content, "guarde isso") || strings.Contains(held[0].Content, "Ningo,") ||
		held[0].Person != conversation.PersonKey("Ana") {
		t.Fatal(held)
	}
	if reply != "Um nome de leitor. Pretensioso.\n\nGuardado: "+held[0].Content {
		t.Fatal(reply)
	}
	if !strings.Contains(model.system[0], "reports the outcome itself") {
		t.Fatal("the model must leave the outcome to the application", model.system[0])
	}
}

func TestARequestWithNothingToKeepIsAnsweredAsOne(t *testing.T) {
	for _, content := range []string{"boa, guarde isso", "guarde isso, tá?"} {
		handler, model := factsHandler(t, map[string]any{"reply": "Hm."})
		if reply := say(t, handler, factGroupTurn(content, "5511999990000", false)); !strings.HasPrefix(reply, "Hm.\n\nDiga o que guardar") {
			t.Fatal(content, reply)
		}
		if held, _ := handler.facts.All(context.Background()); len(held) != 0 {
			t.Fatal(content, held)
		}
		if !strings.Contains(model.system[0], "reports the outcome itself") {
			t.Fatal("the model must not claim it kept this", content)
		}
	}
}

func TestAnOrdinaryTurnIsNotToldAboutAnOutcome(t *testing.T) {
	handler, model := factsHandler(t, map[string]any{"reply": "ok"})
	event := factGroupTurn("oi", "5511999990000", false)
	event.Context["fact_in_message"] = true
	if reply := say(t, handler, event); reply != "ok" {
		t.Fatal(reply)
	}
	if strings.Contains(model.system[0], "reports the outcome itself") {
		t.Fatal("an interface does not grant this")
	}
}

func TestAStrangerIsAnsweredAndToldNothingWasKept(t *testing.T) {
	handler, _ := factsHandler(t, map[string]any{"reply": "Quem é você?"})
	reply := say(t, handler, factGroupTurn("meu filme é Stalker, guarde isso, e o seu?", "5511888880000", false))
	if !strings.HasPrefix(reply, "Quem é você?\n\nSó guardo isso") {
		t.Fatal(reply)
	}
	if held, _ := handler.facts.All(context.Background()); len(held) != 0 {
		t.Fatal(held)
	}
}

func TestAMessageTooLongToKeepIsStillAnswered(t *testing.T) {
	handler, _ := factsHandler(t, map[string]any{"reply": "Li."})
	long := strings.Repeat("palavra ", conversation.MaxFactRunes/8+1) + ", guarde isso"
	if reply := say(t, handler, factGroupTurn(long, "5511999990000", false)); !strings.HasPrefix(reply, "Li.\n\nIsso é longo demais") {
		t.Fatal(reply)
	}
	if held, _ := handler.facts.All(context.Background()); len(held) != 0 {
		t.Fatal(held)
	}
}

func TestVoiceKeepsNothingFromAClause(t *testing.T) {
	handler, _ := factsHandler(t)
	event, line := handler.keepInMessage(context.Background(), core.Event{Context: map[string]any{"voice_turns": []chat.VoiceTurn{}}}, "gosto de chá preto, guarde isso")
	if line != "" || event.Context["fact_in_message"] != nil {
		t.Fatal(line, event.Context)
	}
}

func TestARetriedTurnAnswersAndKeepsTheFactOnce(t *testing.T) {
	archive := archiveFor(t)
	model := &scriptedModel{answers: []map[string]any{nil, {"reply": "Bom gosto."}}}
	handler := NewChatDomainHandler(model).WithArchive(archive).WithFacts(factsFor(t))
	held, _ := archive.Start(context.Background(), conversation.OwnerScope, conversation.KindGeneral, "", "")
	event := ownerTurn(map[string]any{"content": "meu filme é Stalker, guarde isso, e o seu?", "conversation_id": held.ID, "send_id": "send-0001"})
	if _, err := handler.Handle(context.Background(), event, archiveStep); err == nil {
		t.Fatal("the model failed")
	}
	if facts, _ := handler.facts.All(context.Background()); len(facts) != 1 {
		t.Fatal("what was asked stays kept when the answer fails", facts)
	}
	turns, _ := archive.Turns(context.Background(), conversation.OwnerScope, held.ID, 0, 10)
	retry := ownerTurn(map[string]any{"conversation_id": held.ID, "retry_turn_id": turns[0].ID})
	result, err := handler.Handle(context.Background(), retry, archiveStep)
	if err != nil || result["reply"] != "Bom gosto.\n\nGuardado: meu filme é Stalker, e o seu?" {
		t.Fatal(result, err)
	}
	if facts, _ := handler.facts.All(context.Background()); len(facts) != 1 {
		t.Fatal("a retry keeps the fact once", facts)
	}
	turns, _ = archive.Turns(context.Background(), conversation.OwnerScope, held.ID, 0, 10)
	if !strings.Contains(turns[len(turns)-1].Reply, "Guardado:") {
		t.Fatal("the transcript keeps what the owner was shown", turns)
	}
}
