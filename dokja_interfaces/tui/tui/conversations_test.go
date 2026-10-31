package tui

// These tests run in Portuguese like the rest of this suite (TestMain in i18n_test.go), so they assert the
// Portuguese catalog text.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	firstConversation  = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	secondConversation = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func conversationFixture(id, kind, title string, revision float64) map[string]any {
	return map[string]any{
		"id": id, "kind": kind, "title": title, "revision": revision, "turns": 2.0,
		"updated_at": "2026-10-30T12:00:00Z",
	}
}

func turnFixture(id, state, content, reply, reason string) map[string]any {
	return map[string]any{
		"view": 1.0, "id": id, "state": state, "content": content, "reply": reply, "reason": reason,
		"provider": "ollama", "model": "example/model",
	}
}

func conversationsTUI(t *testing.T) (*Model, *fakeClient) {
	t.Helper()
	fake := newFake()
	fake.responses["conversation.list"] = map[string]any{"conversations": []any{
		conversationFixture(firstConversation, "general", "Justiça", 3),
		conversationFixture(secondConversation, "reading", "", 1),
	}, "next": ""}
	fake.responses["conversation.read"] = map[string]any{
		"conversation": conversationFixture(firstConversation, "general", "Justiça", 3),
		"turns":        []any{turnFixture("t1", "completed", "O que é justiça?", "Pergunte a Kant.", "")},
	}
	fake.responses["conversation.start"] = map[string]any{"conversation": conversationFixture(firstConversation, "general", "Novo", 1)}
	fake.responses["conversation.rename"] = map[string]any{"conversation": conversationFixture(firstConversation, "general", "Outro", 4)}
	fake.responses["conversation.delete"] = map[string]any{"deleted": true}
	fake.responses["message.created"] = map[string]any{"chat": map[string]any{"reply": "ok"}}
	m := NewModel(fake, time.Millisecond, time.Second)
	pump(m, m.Init())
	return m, fake
}

func TestTheTabListsTheSharedConversations(t *testing.T) {
	m, fake := conversationsTUI(t)
	press(m, "5")
	if fake.count("conversation.list") != 1 {
		t.Fatal("opening the tab reads the archive")
	}
	view := m.View()
	for _, want := range []string{"aaaaaaaa", "Justiça", "(sem título)", "2 turnos", "2026-10-30", "leitura"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q:\n%s", want, view)
		}
	}
}

func TestOpeningShowsTheTurnsAndWhoAnswered(t *testing.T) {
	m, fake := conversationsTUI(t)
	press(m, "5", "enter")
	if got := fake.last("conversation.read").payload; got["conversation_id"] != firstConversation {
		t.Fatal(got)
	}
	view := m.View()
	for _, want := range []string{"Você: O que é justiça?", "Ningo: Pergunte a Kant.", "Respondido por ollama / example/model"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q:\n%s", want, view)
		}
	}
}

func TestEveryTurnStateIsShownAsItIs(t *testing.T) {
	raw, err := os.ReadFile("../../../dokja_orch/internal/handlers/testdata/turn_view_v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var turns []any
	if err := json.Unmarshal(raw, &turns); err != nil {
		t.Fatal(err)
	}
	m, fake := conversationsTUI(t)
	fake.responses["conversation.read"] = map[string]any{"conversation": conversationFixture(firstConversation, "general", "Pin", 1), "turns": turns}
	press(m, "5", "enter")
	view := m.View()
	for _, want := range []string{
		"Ningo: One who judges.", "Respondido por ollama / example/model:latest", "Ainda sendo respondido.",
		"Falhou: the model returned no reply", "Interrompido antes da resposta.",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q:\n%s", want, view)
		}
	}
	if strings.Count(view, "Ningo: ") != 1 {
		t.Fatal("only a completed turn has a reply", view)
	}
}

func TestWhatIsTypedIsTextAndGoesToTheOpenConversation(t *testing.T) {
	m, fake := conversationsTUI(t)
	press(m, "5", "enter", "enter")
	typeText(m, "q1 2")
	press(m, "enter")
	if m.tab != tabConversations {
		t.Fatal("digits typed in a message must not switch tabs")
	}
	got := fake.last("message.created").payload
	if got["content"] != "q1 2" || got["conversation_id"] != firstConversation || !strings.HasPrefix(got["send_id"].(string), "cli-") {
		t.Fatal(got)
	}
	if fake.count("conversation.read") != 2 {
		t.Fatal("the transcript is read again after the turn")
	}
}

func TestAFirstMessageStartsATitledConversation(t *testing.T) {
	m, fake := conversationsTUI(t)
	press(m, "5", "n")
	typeText(m, "Um novo tema")
	press(m, "enter")
	if got := fake.last("conversation.start").payload; got["title"] != "Um novo tema" {
		t.Fatal(got)
	}
	if got := fake.last("message.created").payload; got["conversation_id"] != firstConversation {
		t.Fatal(got)
	}
}

func TestCancellingANewConversationStartsNothing(t *testing.T) {
	m, fake := conversationsTUI(t)
	press(m, "5", "n", "esc")
	if fake.count("conversation.start") != 0 || m.conversations.open != nil {
		t.Fatal("nothing was written")
	}
}

func TestAFailedTurnIsShownWithTheFailure(t *testing.T) {
	m, fake := conversationsTUI(t)
	press(m, "5", "enter")
	fake.errs["message.created"] = errors.New("o modelo não respondeu")
	fake.responses["conversation.read"] = map[string]any{
		"conversation": conversationFixture(firstConversation, "general", "Justiça", 4),
		"turns":        []any{turnFixture("t2", "failed", "Oi", "", "the model returned no reply")},
	}
	press(m, "enter")
	typeText(m, "Oi")
	press(m, "enter")
	if view := m.View(); !strings.Contains(view, "Falhou: the model returned no reply") || !strings.Contains(view, "o modelo não respondeu") {
		t.Fatal(view)
	}
}

func TestRetryNamesTheLastUnansweredTurnAndNoNewMessage(t *testing.T) {
	m, fake := conversationsTUI(t)
	fake.responses["conversation.read"] = map[string]any{
		"conversation": conversationFixture(firstConversation, "general", "Justiça", 4),
		"turns": []any{
			turnFixture("t1", "failed", "Antes", "", "x"), turnFixture("t2", "completed", "Depois", "Sim", ""),
			turnFixture("t3", "interrupted", "Agora", "", ""),
		},
	}
	press(m, "5", "enter", "R")
	got := fake.last("message.created").payload
	if got["retry_turn_id"] != "t3" || got["conversation_id"] != firstConversation || got["send_id"] != nil || got["content"] != nil {
		t.Fatal(got)
	}
}

func pendingFixture(id, sendID, content string) map[string]any {
	turn := turnFixture(id, "pending", content, "", "")
	turn["send_id"] = sendID
	return turn
}

func TestStoppingReachesTheSendThisScreenWaitsOn(t *testing.T) {
	m, fake := conversationsTUI(t)
	fake.responses["conversation.stop"] = map[string]any{"stopping": true}
	press(m, "5", "enter")
	// As while a reply is awaited: the turn is out, and nothing else may start.
	m.busy, m.conversations.sending = true, &runningTurn{firstConversation, "send-0001"}
	reads := fake.count("conversation.read")
	press(m, "s")
	got := fake.last("conversation.stop").payload
	if got["conversation_id"] != firstConversation || got["send_id"] != "send-0001" {
		t.Fatal(got)
	}
	if !m.busy || fake.count("conversation.read") != reads {
		t.Fatal("the awaited turn brings its own outcome", m.busy, fake.count("conversation.read"))
	}
	if !strings.Contains(m.notice, "parando a resposta") {
		t.Fatal(m.notice)
	}
}

func TestStoppingASendFromElsewhereReadsHowItEnded(t *testing.T) {
	m, fake := conversationsTUI(t)
	fake.responses["conversation.stop"] = map[string]any{"stopping": true}
	fake.responses["conversation.read"] = map[string]any{
		"conversation": conversationFixture(firstConversation, "general", "Justiça", 4),
		"turns":        []any{pendingFixture("t1", "send-elsewhere", "Agora")},
	}
	press(m, "5", "enter")
	reads := fake.count("conversation.read")
	press(m, "s")
	if got := fake.last("conversation.stop").payload; got["send_id"] != "send-elsewhere" {
		t.Fatal(got)
	}
	if fake.count("conversation.read") != reads+1 {
		t.Fatal("a send nothing here waits on must be read again")
	}
}

func TestAWaitedSendIsForgottenOnceItsTurnIsShown(t *testing.T) {
	m, fake := conversationsTUI(t)
	press(m, "5", "enter", "enter")
	m.conversationInput.SetValue("Agora")
	press(m, "enter")
	if fake.count("message.created") != 1 || m.conversations.sending != nil {
		t.Fatal("the settled send must not stay stoppable", m.conversations.sending)
	}
	press(m, "s")
	if fake.count("conversation.stop") != 0 || !strings.Contains(m.notice, "Nenhuma resposta a caminho") {
		t.Fatal(m.notice)
	}
}

func TestAStopThatFailsSaysSoWithoutTheArchivesWords(t *testing.T) {
	m, fake := conversationsTUI(t)
	fake.responses["conversation.read"] = map[string]any{
		"conversation": conversationFixture(firstConversation, "general", "Justiça", 4),
		"turns":        []any{pendingFixture("t1", "send-elsewhere", "Agora")},
	}
	fake.errs["conversation.stop"] = errors.New("conversation changed; read it again")
	press(m, "5", "enter", "s")
	if !strings.Contains(m.notice, "Não consegui parar a resposta") || m.conversations.stopping {
		t.Fatal(m.notice, m.conversations.stopping)
	}
}

func TestRetryHasNothingToDoAfterAnAnswer(t *testing.T) {
	m, fake := conversationsTUI(t)
	press(m, "5", "enter", "R")
	if fake.count("message.created") != 0 || !strings.Contains(m.View(), "não tem nada para reenviar") {
		t.Fatal(m.View())
	}
}

func TestRenameNamesTheRevisionItShowed(t *testing.T) {
	m, fake := conversationsTUI(t)
	press(m, "5", "e")
	m.conversationInput.SetValue("Outro")
	press(m, "enter")
	got := fake.last("conversation.rename").payload
	if got["conversation_id"] != firstConversation || got["revision"] != 3 || got["title"] != "Outro" {
		t.Fatal(got)
	}
}

func TestDeleteAsksFirstAndOnlyYesDeletes(t *testing.T) {
	m, fake := conversationsTUI(t)
	press(m, "5", "d", "n")
	if fake.count("conversation.delete") != 0 {
		t.Fatal("no means no")
	}
	press(m, "d", "y")
	got := fake.last("conversation.delete").payload
	if got["conversation_id"] != firstConversation || got["revision"] != 3 {
		t.Fatal(got)
	}
	if fake.count("conversation.list") != 2 {
		t.Fatal("the list is read again")
	}
}

func TestAReplyCannotDriveTheTerminal(t *testing.T) {
	m, fake := conversationsTUI(t)
	fake.responses["conversation.read"] = map[string]any{
		"conversation": conversationFixture(firstConversation, "general", "T\x1b]0;x\x07", 1),
		"turns":        []any{turnFixture("t1", "completed", "a\x1b[2J", "b\x1b]52;c;AAAA\x07c", "")},
	}
	press(m, "5", "enter")
	for _, turn := range m.conversations.open.turns {
		for _, line := range turnLines(turn) {
			if strings.Contains(line, "]52") && strings.ContainsRune(line, 0x1b) {
				t.Fatalf("a control sequence reached the screen: %q", line)
			}
		}
	}
	if strings.ContainsRune(conversationName(m.conversations.open.row), 0x1b) {
		t.Fatal("a title must not carry control characters")
	}
}

func TestTheFlatAnswerOfTheConversationDomainKeepsItsField(t *testing.T) {
	inner := map[string]any{"conversation": map[string]any{"id": firstConversation}}
	res, err := unwrapResult(map[string]any{"domain": "conversation", "result": inner})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := res["conversation"].(map[string]any); !ok {
		t.Fatal("conversation.start would lose its conversation", res)
	}
	other, err := unwrapResult(map[string]any{"result": map[string]any{"memory": map[string]any{"ok": true}}})
	if err != nil || other["ok"] != true {
		t.Fatal("every other reply unwraps as before", other, err)
	}
}

func TestRenamingTheOnlyConversationWorksWithAnEmptyList(t *testing.T) {
	m, fake := conversationsTUI(t)
	fake.responses["conversation.list"] = map[string]any{"conversations": []any{}, "next": ""}
	press(m, "5", "n")
	typeText(m, "Primeira")
	press(m, "enter", "e")
	m.conversationInput.SetValue("Outro")
	press(m, "enter")
	if got := fake.last("conversation.rename").payload; got["title"] != "Outro" {
		t.Fatal(got)
	}
}

func TestTextWrittenWhileBusyStaysInTheField(t *testing.T) {
	m, fake := conversationsTUI(t)
	press(m, "5", "enter", "enter")
	typeText(m, "Não me perca")
	m.busy, m.busyLabel = true, "outra coisa"
	press(m, "enter")
	if fake.count("message.created") != 0 || m.conversationInput.Value() != "Não me perca" || !m.conversations.writing() {
		t.Fatal("the text must wait in the field", m.conversationInput.Value())
	}
}

func TestF5OpensTheTab(t *testing.T) {
	m, fake := conversationsTUI(t)
	m.Update(tea.KeyMsg{Type: tea.KeyF5})
	if m.tab != tabConversations {
		t.Fatal(fake.calls)
	}
}

// numberedTurns are turns first to last of a conversation, each with its place in it.
func numberedTurns(first, last int) []any {
	turns := []any{}
	for seq := first; seq <= last; seq++ {
		turn := turnFixture(fmt.Sprintf("t%d", seq), "completed", fmt.Sprintf("pergunta %d", seq), "resposta", "")
		turn["seq"] = float64(seq)
		delete(turn, "provider")
		delete(turn, "model")
		turns = append(turns, turn)
	}
	return turns
}

func readPage(turns []any) map[string]any {
	return map[string]any{"conversation": conversationFixture(firstConversation, "general", "Longa", 3), "turns": turns}
}

func TestEarlierTurnsAreReadOnRequest(t *testing.T) {
	m, fake := conversationsTUI(t)
	fake.responses["conversation.read"] = readPage(numberedTurns(101, 200))
	m.height = 24
	press(m, "5", "enter")
	for range 500 {
		press(m, "up")
	}
	if !strings.Contains(m.View(), "Turnos anteriores com m.") {
		t.Fatal("the top of the page says there is more", m.View())
	}
	fake.responses["conversation.read"] = readPage(numberedTurns(1, 100))
	press(m, "m")
	got := fake.last("conversation.read").payload
	if got["conversation_id"] != firstConversation || got["before_seq"] != 101 {
		t.Fatal(got)
	}
	if turns := m.conversations.open.turns; len(turns) != 200 || turns[0].Seq != 1 || turns[199].Seq != 200 {
		t.Fatal(len(turns))
	}
	view := m.View()
	if !strings.Contains(view, "pergunta 101") || strings.Contains(view, "pergunta 100") {
		t.Fatal("what was on screen stays where it was", view)
	}
	for range 500 {
		press(m, "up")
	}
	if strings.Contains(m.View(), "Turnos anteriores com m.") {
		t.Fatal("the first turn is on screen, there is nothing earlier")
	}
}

func TestThereIsNothingEarlierInAShortConversation(t *testing.T) {
	m, fake := conversationsTUI(t)
	fake.responses["conversation.read"] = readPage(numberedTurns(1, 3))
	press(m, "5", "enter", "m")
	if fake.count("conversation.read") != 1 {
		t.Fatal("nothing to read")
	}
	if strings.Contains(m.View(), "Turnos anteriores") {
		t.Fatal(m.View())
	}
}

func TestSendingKeepsTheEarlierTurnsAlreadyRead(t *testing.T) {
	m, fake := conversationsTUI(t)
	fake.responses["conversation.read"] = readPage(numberedTurns(101, 200))
	press(m, "5", "enter")
	fake.responses["conversation.read"] = readPage(numberedTurns(1, 100))
	press(m, "m")
	fake.responses["conversation.read"] = readPage(numberedTurns(102, 201))
	press(m, "enter")
	typeText(m, "mais uma")
	press(m, "enter")
	turns := m.conversations.open.turns
	if len(turns) != 201 || turns[0].Seq != 1 || turns[100].Seq != 101 || turns[200].Seq != 201 {
		t.Fatal(len(turns))
	}
	if m.conversations.scroll != 0 {
		t.Fatal("after a send the end is shown")
	}
}

func TestARepeatedTurnInAPageIsShownOnce(t *testing.T) {
	m, fake := conversationsTUI(t)
	fake.responses["conversation.read"] = readPage(numberedTurns(50, 60))
	press(m, "5", "enter")
	fake.responses["conversation.read"] = readPage(numberedTurns(40, 50))
	press(m, "m")
	if turns := m.conversations.open.turns; len(turns) != 21 || turns[10].Seq != 50 || turns[11].Seq != 51 {
		t.Fatal(len(turns))
	}
}

func TestAPageOfAConversationLeftBehindIsDropped(t *testing.T) {
	m, fake := conversationsTUI(t)
	fake.responses["conversation.read"] = readPage(numberedTurns(101, 110))
	press(m, "5", "enter")
	m.Update(conversationOlderMsg{id: secondConversation, turns: []conversationTurn{{ID: "other", Seq: 1}}})
	if len(m.conversations.open.turns) != 10 {
		t.Fatal(m.conversations.open.turns)
	}
	m.conversations.open = nil
	m.Update(conversationOlderMsg{id: firstConversation, turns: []conversationTurn{{ID: "other", Seq: 1}}})
	if m.conversations.open != nil {
		t.Fatal("a late page does not reopen what was left")
	}
}

func TestEarlierTurnsThatCannotBeReadChangeNothing(t *testing.T) {
	m, fake := conversationsTUI(t)
	fake.responses["conversation.read"] = readPage(numberedTurns(101, 110))
	press(m, "5", "enter")
	fake.errs["conversation.read"] = errors.New("o arquivo não respondeu")
	press(m, "m")
	if len(m.conversations.open.turns) != 10 || !strings.Contains(m.View(), "o arquivo não respondeu") {
		t.Fatal(m.View())
	}
}
