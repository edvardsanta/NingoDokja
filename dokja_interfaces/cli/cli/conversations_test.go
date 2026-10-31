package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

type scriptedArchive struct {
	asked   []string
	payload []map[string]any
	answers map[string]string
}

func (s *scriptedArchive) ask(eventType string, payload map[string]any) (map[string]any, error) {
	s.asked = append(s.asked, eventType)
	s.payload = append(s.payload, payload)
	raw, ok := s.answers[eventType]
	if !ok {
		return nil, fmt.Errorf("unexpected %s", eventType)
	}
	answer := map[string]any{}
	return answer, json.Unmarshal([]byte(raw), &answer)
}

func conversationID(c string) string { return strings.Repeat(c, archiveIDLength) }

func TestAFlatAnswerKeepsTheTurnsBesideTheConversation(t *testing.T) {
	answer, err := conversationAnswer(map[string]any{"domain": "conversation", "result": map[string]any{
		"conversation": map[string]any{"id": "a"}, "turns": []any{map[string]any{"seq": 1}},
	}})
	if err != nil || answer["turns"] == nil {
		t.Fatal("the turns must survive a one-domain reply", answer, err)
	}
	nested, err := conversationAnswer(map[string]any{"result": map[string]any{
		"conversation": map[string]any{"conversation": map[string]any{"id": "a"}, "turns": []any{}},
	}})
	if err != nil || nested["turns"] == nil {
		t.Fatal("a verbose reply nests the answer under the domain", nested, err)
	}
}

func TestTheCLIReadsThePinnedTurnProjection(t *testing.T) {
	raw, err := os.ReadFile("../../../dokja_orch/internal/handlers/testdata/turn_view_v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var turns []archivedTurn
	if err := json.Unmarshal(raw, &turns); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	printTurns(&out, conversationReading{Conversation: archivedConversation{ID: conversationID("a"), Title: "Pinned"}, Turns: turns})
	for _, want := range []string{
		"Pinned (aaaaaaaa)", "you: What is a reader?", "ningo: One who judges.", "(answered by ollama / example/model:latest)",
		"(still being answered)", "(failed: the model returned no reply)", "(interrupted before an answer)",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q in:\n%s", want, out.String())
		}
	}
	if strings.Count(out.String(), "ningo:") != 1 {
		t.Fatal("only a completed turn has a reply", out.String())
	}
	for _, turn := range turns {
		if turn.View != 1 {
			t.Fatal("this reader knows version 1", turn)
		}
	}
}

func TestAShortIDFindsItsConversationAndOnlyThatOne(t *testing.T) {
	archive := &scriptedArchive{answers: map[string]string{"conversation.list": fmt.Sprintf(
		`{"conversations":[{"id":"%s","kind":"general"},{"id":"ab%s","kind":"general"},{"id":"ac%s","kind":"general"}]}`,
		conversationID("0"), strings.Repeat("0", 30), strings.Repeat("0", 30))}}
	if id, err := matchConversation(archive.ask, "AC00"); err != nil || !strings.HasPrefix(id, "ac00") {
		t.Fatal(id, err)
	}
	if _, err := matchConversation(archive.ask, "a"); err == nil || !strings.Contains(err.Error(), "2 conversations") {
		t.Fatal("an ambiguous id must not choose", err)
	}
	if _, err := matchConversation(archive.ask, "ff"); err == nil {
		t.Fatal("an unknown id must not choose")
	}
	asked := len(archive.asked)
	if id, err := matchConversation(archive.ask, conversationID("f")); err != nil || id != conversationID("f") || len(archive.asked) != asked {
		t.Fatal("a whole id needs no list", id, err)
	}
}

func TestContinueTakesTheLatestGeneralConversationNotAReading(t *testing.T) {
	archive := &scriptedArchive{answers: map[string]string{"conversation.list": fmt.Sprintf(
		`{"conversations":[{"id":"%s","kind":"reading"},{"id":"%s","kind":"general"},{"id":"%s","kind":"general"}]}`,
		conversationID("a"), conversationID("b"), conversationID("c"))}}
	id, err := chatTarget{proceed: true}.resolve(archive.ask, "Hello")
	if err != nil || id != conversationID("b") {
		t.Fatal(id, err)
	}
}

func TestContinueStartsATitledConversationWhenThereIsNone(t *testing.T) {
	archive := &scriptedArchive{answers: map[string]string{
		"conversation.list":  `{"conversations":[]}`,
		"conversation.start": fmt.Sprintf(`{"conversation":{"id":"%s"}}`, conversationID("d")),
	}}
	id, err := chatTarget{proceed: true}.resolve(archive.ask, "  Where   do we start? ")
	if err != nil || id != conversationID("d") {
		t.Fatal(id, err)
	}
	if last := archive.payload[len(archive.payload)-1]; last["title"] != "Where do we start?" {
		t.Fatal(last)
	}
}

func TestATitleIsCutToTheStartOfTheMessage(t *testing.T) {
	title := ConversationTitle(strings.Repeat("é", 100))
	if len([]rune(title)) != titleFromRunes || !strings.HasSuffix(title, "…") {
		t.Fatal(title)
	}
}

func TestAPlainChatStaysOutOfTheArchive(t *testing.T) {
	app := NewApp()
	app.newRequester = func(string) (*Requester, error) {
		t.Fatal("a plain chat asks the archive nothing")
		return nil, nil
	}
	payload, context_, err := app.chatFields(context.Background(), " hi ", chatSessionOptions{sessionKey: "k", forceNewSession: true})
	if err != nil || payload["content"] != "hi" || payload["conversation_id"] != nil || payload["send_id"] != nil {
		t.Fatal(payload, err)
	}
	if context_["session_key"] != "k" || context_["force_new_session"] != true {
		t.Fatal(context_)
	}
}

func TestAnArchivedChatNamesItsConversationAndAFreshSend(t *testing.T) {
	app := NewApp()
	held := conversationID("e")
	first, _, err := app.chatFields(context.Background(), "one", chatSessionOptions{target: chatTarget{reference: held}, conversationID: new(string)})
	if err != nil {
		t.Fatal(err)
	}
	second, chatContext, err := app.chatFields(context.Background(), "two", chatSessionOptions{target: chatTarget{reference: held}, conversationID: new(string)})
	if err != nil {
		t.Fatal(err)
	}
	if first["conversation_id"] != held || first["send_id"] == second["send_id"] {
		t.Fatal(first, second)
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`).MatchString(first["send_id"].(string)) {
		t.Fatal("the archive refuses this send id", first["send_id"])
	}
	if chatContext["session_key"] != nil || chatContext["force_new_session"] != nil {
		t.Fatal("the archive decides the session of an archived send", chatContext)
	}
}

func TestAREPLStaysInTheConversationItStarted(t *testing.T) {
	app := NewApp()
	held := conversationID("e")
	kept := held
	app.newRequester = func(string) (*Requester, error) {
		t.Fatal("a known conversation asks nothing again")
		return nil, nil
	}
	payload, _, err := app.chatFields(context.Background(), "next", chatSessionOptions{target: chatTarget{start: true}, conversationID: &kept})
	if err != nil || payload["conversation_id"] != held {
		t.Fatal(payload, err)
	}
}

func TestChoosingTwoConversationTargetsIsRefused(t *testing.T) {
	app := NewApp()
	app.newRequester = func(string) (*Requester, error) {
		t.Fatal("refused flags must send nothing")
		return nil, nil
	}
	if err := app.Run(context.Background(), []string{"dokja-cli", "chat", "--new", "--continue", "hello"}); err == nil {
		t.Fatal("expected an error")
	}
}

func TestListThroughTheOrchestrator(t *testing.T) {
	endpoint, received := fakeOrchestrator(t, fmt.Sprintf(
		`{"status":"ok","result":{"domain":"conversation","result":{"conversations":[{"id":"%s","kind":"general","title":"Justice","turns":2,"updated_at":"2026-10-30T12:00:00Z"}],"next":""}}}`,
		conversationID("a")))
	app := NewApp()
	if err := app.Run(context.Background(), []string{"dokja-cli", "--request-endpoint", endpoint, "conversations", "list"}); err != nil {
		t.Fatal(err)
	}
	var event Event
	if err := json.Unmarshal([]byte(<-received), &event); err != nil {
		t.Fatal(err)
	}
	if event.Type != "conversation.list" || event.Source != "cli" {
		t.Fatal("the archive only answers the owner's interfaces", event)
	}
}

func TestDeletingAsksAndNoLeavesTheConversation(t *testing.T) {
	held := conversationID("a")
	archive := &scriptedArchive{answers: map[string]string{
		"conversation.read":   fmt.Sprintf(`{"conversation":{"id":"%s","title":"Keep","revision":3,"turns":2},"turns":[]}`, held),
		"conversation.delete": fmt.Sprintf(`{"conversation_id":"%s","deleted":true}`, held),
	}}
	var out bytes.Buffer
	for _, answer := range []string{"n\n", "\n"} {
		if err := deleteConversation(archive.ask, strings.NewReader(answer), &out, held, false); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Contains(strings.Join(archive.asked, " "), "conversation.delete") {
		t.Fatal("only a yes deletes", archive.asked)
	}
	if err := deleteConversation(archive.ask, strings.NewReader("y\n"), &out, held, false); err != nil {
		t.Fatal(err)
	}
	last := archive.payload[len(archive.payload)-1]
	if archive.asked[len(archive.asked)-1] != "conversation.delete" || last["conversation_id"] != held || last["revision"] != int64(3) {
		t.Fatal("the delete must name the revision it showed", archive.asked, last)
	}
}

func TestStopNamesTheLatestTurnStillOnItsWay(t *testing.T) {
	held := conversationID("a")
	archive := &scriptedArchive{answers: map[string]string{
		"conversation.read": fmt.Sprintf(`{"conversation":{"id":"%s"},"turns":[{"send_id":"send-old","state":"completed"},{"send_id":"send-0001","state":"pending"}]}`, held),
		"conversation.stop": fmt.Sprintf(`{"conversation_id":"%s","send_id":"send-0001","stopping":true}`, held),
	}}
	var out bytes.Buffer
	if err := stopLatest(archive.ask, &out, held); err != nil {
		t.Fatal(err)
	}
	last := archive.payload[len(archive.payload)-1]
	if archive.asked[len(archive.asked)-1] != "conversation.stop" || last["conversation_id"] != held || last["send_id"] != "send-0001" {
		t.Fatal(archive.asked, last)
	}
	if !strings.Contains(out.String(), "Read the conversation") {
		t.Fatal("a stop only asks, and must not claim the reply ended", out.String())
	}
}

func TestStopAsksNothingWhenNoReplyIsOnItsWay(t *testing.T) {
	held := conversationID("a")
	archive := &scriptedArchive{answers: map[string]string{
		"conversation.read": fmt.Sprintf(`{"conversation":{"id":"%s"},"turns":[{"send_id":"send-0001","state":"pending"},{"send_id":"send-0002","state":"completed"}]}`, held),
	}}
	var out bytes.Buffer
	if err := stopLatest(archive.ask, &out, held); err == nil || !strings.Contains(err.Error(), "on its way") {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(archive.asked, " "), "conversation.stop") {
		t.Fatal("nothing to stop must ask nothing", archive.asked)
	}
}

func TestAConversationWithNoTitleIsSaidToHaveNone(t *testing.T) {
	var out bytes.Buffer
	printConversations(&out, conversationListing{Conversations: []archivedConversation{{ID: conversationID("a"), Kind: "reading", Turns: 1, UpdatedAt: "2026-10-30T12:00:00Z"}}, Next: "1.a"})
	for _, want := range []string{"aaaaaaaa  (untitled) · 1 turns · 2026-10-30 [reading]", "--cursor 1.a"} {
		if !strings.Contains(out.String(), want) {
			t.Fatal(out.String())
		}
	}
	out.Reset()
	printConversations(&out, conversationListing{})
	if !strings.Contains(out.String(), "No conversations yet.") {
		t.Fatal(out.String())
	}
}

func TestOnlyTheLastUnansweredTurnIsSentAgain(t *testing.T) {
	if turn, ok := retryable([]archivedTurn{{ID: "a", State: "failed"}, {ID: "b", State: "completed"}}); ok {
		t.Fatal("an earlier failure with an answer after it is history", turn)
	}
	if turn, ok := retryable([]archivedTurn{{ID: "a", State: "completed"}, {ID: "b", State: "interrupted"}}); !ok || turn.ID != "b" {
		t.Fatal(turn, ok)
	}
	if turn, ok := retryable([]archivedTurn{{ID: "a", State: "failed"}, {ID: "b", State: "pending"}}); !ok || turn.ID != "a" {
		t.Fatal(turn, ok)
	}
	if _, ok := retryable(nil); ok {
		t.Fatal("no turns, nothing to send")
	}
}

func TestARetryNamesTheTurnAndCarriesNoNewMessage(t *testing.T) {
	held := conversationID("e")
	payload, _, err := NewApp().chatFields(context.Background(), "", chatSessionOptions{
		retryID: "turn", target: chatTarget{reference: held}, conversationID: &held,
	})
	if err != nil || payload["retry_turn_id"] != "turn" || payload["conversation_id"] != held {
		t.Fatal(payload, err)
	}
	if payload["send_id"] != nil || payload["content"] != nil {
		t.Fatal("a retry records no new turn", payload)
	}
}
