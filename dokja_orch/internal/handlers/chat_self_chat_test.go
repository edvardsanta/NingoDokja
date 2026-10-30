package handlers

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	conversation "read_books/dokja_domain/dokja_conversation"
	"read_books/internal/core"
)

// whatsappTurn is a message the WhatsApp interface hands over; selfChat says whether the linked
// account wrote it in its own "message yourself" chat.
func whatsappTurn(content, messageID string, selfChat bool) core.Event {
	return core.Event{
		Source: core.SourceWhatsApp, Type: "message.created",
		User:    core.EventUser{ID: "owner-number"},
		Channel: core.EventChannel{ID: "self-chat"},
		Payload: map[string]any{"content": content},
		Context: map[string]any{"gateway_inbound": true, "is_self_chat": selfChat, "message_id": messageID, "content": content},
	}
}

func listed(t *testing.T, archive *conversation.Archive) []conversation.Conversation {
	t.Helper()
	items, _, err := archive.List(context.Background(), conversation.OwnerScope, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	return items
}

func turnsOf(t *testing.T, archive *conversation.Archive, id string) []conversation.Turn {
	t.Helper()
	turns, err := archive.Turns(context.Background(), conversation.OwnerScope, id, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	return turns
}

func TestTheOwnersSelfChatContinuesTheDesktopsConversation(t *testing.T) {
	archive := archiveFor(t)
	model := &scriptedModel{answers: []map[string]any{{"reply": "On the desktop."}, {"reply": "On the phone."}}}
	handler := NewChatDomainHandler(model).WithArchive(archive)
	held, _ := archive.Start(context.Background(), conversation.OwnerScope, conversation.KindGeneral, "", "Justice")
	if _, err := handler.Handle(context.Background(), ownerTurn(map[string]any{"content": "What is justice?", "conversation_id": held.ID, "send_id": "send-0001"}), archiveStep); err != nil {
		t.Fatal(err)
	}

	result, err := handler.Handle(context.Background(), whatsappTurn("And from here?", "MSG1", true), archiveStep)
	if err != nil || result["reply"] != "On the phone." {
		t.Fatal(result, err)
	}
	turns := turnsOf(t, archive, held.ID)
	if len(turns) != 2 || turns[1].Content != "And from here?" || turns[1].State != conversation.TurnCompleted || !strings.HasPrefix(turns[1].SendID, "wa-") {
		t.Fatal("the phone's turn must continue the desktop's conversation", turns)
	}
	asked := model.asked[1]
	if len(asked) != 3 || asked[0]["content"] != "What is justice?" || asked[1]["content"] != "On the desktop." {
		t.Fatal("the phone's reply must see the desktop's turns", asked)
	}
	if len(listed(t, archive)) != 1 {
		t.Fatal("no other conversation may be started")
	}
}

func TestOtherWhatsAppChatsNeverTouchTheArchive(t *testing.T) {
	archive := archiveFor(t)
	model := &scriptedModel{answers: []map[string]any{{"reply": "Hello, stranger."}, {"reply": "Hello, group."}}}
	handler := NewChatDomainHandler(model).WithArchive(archive)
	contact := whatsappTurn("hi", "MSG1", false)
	unflagged := whatsappTurn("hi again", "MSG2", false)
	delete(unflagged.Context, "is_self_chat")
	unflagged.Context["is_self_chat"] = "true" // only a real boolean counts
	for _, event := range []core.Event{contact, unflagged} {
		if _, err := handler.Handle(context.Background(), event, archiveStep); err != nil {
			t.Fatal(err)
		}
	}
	if items := listed(t, archive); len(items) != 0 {
		t.Fatal("a contact's message must not be archived", items)
	}
}

func TestTheSelfChatStartsATitledConversationWhenNothingCanBeContinued(t *testing.T) {
	archive := archiveFor(t)
	model := &scriptedModel{answers: []map[string]any{{"reply": "Begin, then."}}}
	handler := NewChatDomainHandler(model).WithArchive(archive)
	// A reading's discussion is continued only by naming it, so it is not the phone's.
	reading, _ := archive.Start(context.Background(), conversation.OwnerScope, conversation.KindReading, "item-1", "A reading")
	archive.Send(context.Background(), conversation.OwnerScope, reading.ID, "send-0001", "About the reading", conversation.Provenance{})

	if _, err := handler.Handle(context.Background(), whatsappTurn("  Where   do we start? ", "MSG1", true), archiveStep); err != nil {
		t.Fatal(err)
	}
	items := listed(t, archive)
	if len(items) != 2 || items[0].Kind != conversation.KindGeneral || items[0].Title != "Where do we start?" || items[0].Turns != 1 {
		t.Fatal(items)
	}
}

func TestASelfChatMessageDeliveredAgainIsAnsweredOnce(t *testing.T) {
	archive := archiveFor(t)
	model := &scriptedModel{answers: []map[string]any{{"reply": "Once."}}}
	handler := NewChatDomainHandler(model).WithArchive(archive)
	first, err := handler.Handle(context.Background(), whatsappTurn("Again?", "MSG1", true), archiveStep)
	if err != nil {
		t.Fatal(err)
	}
	again, err := handler.Handle(context.Background(), whatsappTurn("Again?", "MSG1", true), archiveStep)
	if err != nil || again["reply"] != "Once." || again["replayed"] != true || len(model.asked) != 1 {
		t.Fatal("a redelivered message is answered from the archive, never asked again", first, again, err)
	}
}

func TestASelfChatMessageDeliveredWhileStillAnsweredGetsNoSecondReply(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	archive := archiveFor(t)
	model := newGatedModel("Slow")
	handler := NewChatDomainHandler(model).WithArchive(archive)
	first := make(chan handled, 1)
	go func() {
		result, err := handler.Handle(ctx, whatsappTurn("Slow", "MSG1", true), archiveStep)
		first <- handled{result, err}
	}()
	awaitSignal(t, ctx, model.started["Slow"], "the first delivery was not asked")
	again, err := handler.Handle(ctx, whatsappTurn("Slow", "MSG1", true), archiveStep)
	if err != nil || again["reply"] != "" || again["replayed"] != true {
		t.Fatal("the repeat must stay quiet; the first delivery brings the reply", again, err)
	}
	close(model.release["Slow"])
	if got := awaitHandled(t, ctx, first); got.err != nil || got.result["reply"] != "Answer to Slow" {
		t.Fatal(got.result, got.err)
	}
}

func TestTwoFirstSelfChatMessagesShareOneConversation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	archive := archiveFor(t)
	model := newGatedModel("One", "Two")
	close(model.release["One"])
	close(model.release["Two"])
	handler := NewChatDomainHandler(model).WithArchive(archive)
	var wg sync.WaitGroup
	for i, content := range []string{"One", "Two"} {
		wg.Add(1)
		go func(content, id string) {
			defer wg.Done()
			if _, err := handler.Handle(ctx, whatsappTurn(content, id, true), archiveStep); err != nil {
				t.Error(err)
			}
		}(content, []string{"MSG1", "MSG2"}[i])
	}
	wg.Wait()
	items := listed(t, archive)
	if len(items) != 1 || items[0].Turns != 2 {
		t.Fatal("two first messages must land in one conversation", items)
	}
}

func TestAPhoneTurnWaitsInOrderBehindTheDesktopsPendingTurn(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	archive := archiveFor(t)
	model := newGatedModel("Desktop first", "Phone second")
	handler := NewChatDomainHandler(model).WithArchive(archive)
	held, _ := archive.Start(ctx, conversation.OwnerScope, conversation.KindGeneral, "", "")
	desktop := sendAsync(ctx, handler, held.ID, "send-0001", "Desktop first")
	awaitSignal(t, ctx, model.started["Desktop first"], "the desktop's turn was not asked")

	phone := make(chan handled, 1)
	go func() {
		result, err := handler.Handle(ctx, whatsappTurn("Phone second", "MSG1", true), archiveStep)
		phone <- handled{result, err}
	}()
	awaitTurns(t, ctx, archive, held.ID, 2)
	time.Sleep(20 * time.Millisecond)
	if model.asked("Phone second") {
		t.Fatal("the phone's turn was asked before the desktop's reply settled")
	}
	close(model.release["Desktop first"])
	awaitHandled(t, ctx, desktop)
	awaitSignal(t, ctx, model.started["Phone second"], "the phone's turn was not asked after the desktop's")
	close(model.release["Phone second"])
	if got := awaitHandled(t, ctx, phone); got.err != nil {
		t.Fatal(got.err)
	}
	if !strings.Contains(model.history("Phone second"), "assistant: Answer to Desktop first") {
		t.Fatal("the phone's reply must see the desktop's", model.history("Phone second"))
	}
}

func TestTheSelfChatCannotPickAnotherConversation(t *testing.T) {
	archive := archiveFor(t)
	model := &scriptedModel{answers: []map[string]any{{"reply": "Here."}}}
	handler := NewChatDomainHandler(model).WithArchive(archive)
	other, _ := archive.Start(context.Background(), conversation.OwnerScope, conversation.KindReading, "item-1", "Other")
	event := whatsappTurn("Into that one", "MSG1", true)
	event.Payload["conversation_id"] = other.ID
	event.Payload["send_id"] = "send-0001"
	if _, err := handler.Handle(context.Background(), event, archiveStep); err != nil {
		t.Fatal(err)
	}
	if turns := turnsOf(t, archive, other.ID); len(turns) != 0 {
		t.Fatal("the interface's own fields must not choose the conversation", turns)
	}
}
