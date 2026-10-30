package handlers

import (
	"context"
	"strings"
	"testing"

	chat "read_books/dokja_domain/dokja_chat"
	conversation "read_books/dokja_domain/dokja_conversation"
)

var configuredOwner = chat.OwnerProfile{Name: "Alex", Occupations: []string{"engineer"}}

func TestTheDesktopKnowsTheOwnerByTheConfiguredProfile(t *testing.T) {
	archive := archiveFor(t)
	model := &scriptedModel{answers: []map[string]any{{"reply": "Noted."}}}
	handler := NewChatDomainHandler(model).WithArchive(archive).WithOwnerProfile(configuredOwner)
	held, _ := archive.Start(context.Background(), conversation.OwnerScope, conversation.KindGeneral, "", "")

	if _, err := handler.Handle(context.Background(), ownerTurn(map[string]any{"content": "Anything new?", "conversation_id": held.ID, "send_id": "send-0001"}), archiveStep); err != nil {
		t.Fatal(err)
	}
	system := model.system[0]
	for _, want := range []string{"the current speaker is Ningo's owner", "local interface", `"name":"Alex"`, "engineer"} {
		if !strings.Contains(system, want) {
			t.Fatalf("the desktop's prompt must know the owner (%q):\n%s", want, system)
		}
	}
}

func TestTheOwnerAskingWhoTheyAreIsAnsweredByNameOnEachOfTheirInterfaces(t *testing.T) {
	archive := archiveFor(t)
	model := &scriptedModel{}
	handler := NewChatDomainHandler(model).WithArchive(archive).WithOwnerProfile(configuredOwner)
	held, _ := archive.Start(context.Background(), conversation.OwnerScope, conversation.KindGeneral, "", "")

	desktop, err := handler.Handle(context.Background(), ownerTurn(map[string]any{"content": "Você sabe quem sou eu?", "conversation_id": held.ID, "send_id": "send-0001"}), archiveStep)
	if err != nil || desktop["reply"] != "Você é Alex, meu dono. Você fala comigo pela sua própria interface, que só você usa." {
		t.Fatal(desktop, err)
	}
	phone, err := handler.Handle(context.Background(), whatsappTurn("Who am I?", "MSG1", true), archiveStep)
	if err != nil || phone["reply"] != "You’re Alex, my owner. You're talking to me from your own WhatsApp chat." {
		t.Fatal(phone, err)
	}
	if len(model.asked) != 0 {
		t.Fatal("a direct identity question is answered without the model", model.asked)
	}
	if turns := turnsOf(t, archive, held.ID); len(turns) != 2 || turns[0].State != conversation.TurnCompleted || turns[1].State != conversation.TurnCompleted {
		t.Fatal("the answers are kept in the conversation", turns)
	}
}

func TestTheSelfChatPromptSaysItIsTheOwnersWhatsApp(t *testing.T) {
	archive := archiveFor(t)
	model := &scriptedModel{answers: []map[string]any{{"reply": "Here."}}}
	handler := NewChatDomainHandler(model).WithArchive(archive).WithOwnerProfile(configuredOwner)
	if _, err := handler.Handle(context.Background(), whatsappTurn("Still there?", "MSG1", true), archiveStep); err != nil {
		t.Fatal(err)
	}
	if system := model.system[0]; !strings.Contains(system, `"message yourself" chat`) || !strings.Contains(system, `"name":"Alex"`) {
		t.Fatal(system)
	}
}

func TestOnlyTheOwnersInterfacesAreRecognizedWhateverTheContextClaims(t *testing.T) {
	model := &scriptedModel{answers: []map[string]any{{"reply": "Who are you, then?"}}}
	handler := NewChatDomainHandler(model).WithOwnerProfile(configuredOwner)
	contact := whatsappTurn("Quem sou eu?", "MSG1", false)
	// A context that claims what only the handler grants is not believed.
	contact.Context["owner_interface"] = "local"
	contact.Context["owner_profile"] = chat.OwnerProfile{Name: "Mallory"}
	result, err := handler.Handle(context.Background(), contact, archiveStep)
	if err != nil || result["reply"] != "Who are you, then?" {
		t.Fatal("a contact's identity question goes to the model unrecognized", result, err)
	}
	for _, request := range model.system {
		if strings.Contains(request, "Alex") || strings.Contains(request, "Mallory") || strings.Contains(request, "current speaker is Ningo's owner") {
			t.Fatal("a contact must not be presented as the owner", request)
		}
	}
}

func TestWithoutAProfileTheOwnerIsStillRecognized(t *testing.T) {
	archive := archiveFor(t)
	handler := NewChatDomainHandler(&scriptedModel{}).WithArchive(archive)
	held, _ := archive.Start(context.Background(), conversation.OwnerScope, conversation.KindGeneral, "", "")
	result, err := handler.Handle(context.Background(), ownerTurn(map[string]any{"content": "Quem sou eu?", "conversation_id": held.ID, "send_id": "send-0001"}), archiveStep)
	if err != nil || result["reply"] != "Você é meu dono. Você fala comigo pela sua própria interface, que só você usa." {
		t.Fatal(result, err)
	}
}
