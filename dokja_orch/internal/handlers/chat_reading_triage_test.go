package handlers

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// triagingGenerator adds the structured task call the production client offers, with a scripted
// verdict, to any chat generator.
type triagingGenerator struct {
	triageModel
	verdict bool
	err     error
	asked   []string
}

type triageModel interface {
	ChatGenerator
	ChatConversationGenerator
}

func (g *triagingGenerator) GenerateTaskJSON(_ context.Context, _ string, _ map[string]any, messages []map[string]string, accept func(map[string]any) error) (map[string]any, error) {
	g.asked = append(g.asked, messages[len(messages)-1]["content"])
	if g.err != nil {
		return nil, g.err
	}
	reply := map[string]any{"uses_reading": g.verdict}
	if err := accept(reply); err != nil {
		return nil, err
	}
	return reply, nil
}

func sentEvidence(generator *fakeChatGenerator) bool {
	return strings.Contains(evidenceSent(generator), "UNTRUSTED_WEB_EVIDENCE")
}

func readThenSay(t *testing.T, generator ChatGenerator, say string) {
	t.Helper()
	service := &webReadingService{}
	h := NewChatDomainHandler(generator).WithWebReading(service, nil)
	if _, err := h.Handle(context.Background(), chatEvent("https://example.com/law"), chatStep); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Handle(context.Background(), chatEvent(say), chatStep); err != nil {
		t.Fatal(err)
	}
}

// Regression: a page read earlier was attached to every later message, so keyboard mashing and
// banter in the channel were answered with citations of that page.
func TestKeptReadingStaysOutOfUnrelatedMessages(t *testing.T) {
	for _, noise := range []string{"AAAAAAAAAAAAAAAA", "gay", "jo", "burger", "PARA DE FALAR DESSE ASSUNTO!!!"} {
		generator := &fakeChatGenerator{result: map[string]any{"reply": "ok"}}
		readThenSay(t, generator, noise)
		if sentEvidence(generator) {
			t.Errorf("%q received the earlier reading as evidence", noise)
		}
	}
}

func TestKeptReadingFollowsAMessageThatNamesIt(t *testing.T) {
	generator := &fakeChatGenerator{result: map[string]any{"reply": "ok"}}
	readThenSay(t, generator, "What does it say about the financial impact estimate?")
	if !sentEvidence(generator) {
		t.Fatal("a follow-up about the page lost the reading")
	}
}

func TestTriageDecidesWhenNoWordsOverlap(t *testing.T) {
	cases := []struct {
		name    string
		verdict bool
		err     error
		want    bool
	}{
		{"related", true, nil, true},
		{"unrelated", false, nil, false},
		{"triage failure keeps evidence out", true, fmt.Errorf("provider down"), false},
	}
	for _, c := range cases {
		inner := &fakeChatGenerator{result: map[string]any{"reply": "ok"}}
		generator := &triagingGenerator{triageModel: inner, verdict: c.verdict, err: c.err}
		readThenSay(t, generator, "e sobre isso, quem decide?")
		if got := sentEvidence(inner); got != c.want {
			t.Errorf("%s: evidence sent = %v, want %v", c.name, got, c.want)
		}
		if len(generator.asked) != 1 {
			t.Errorf("%s: triage asked %d times, want 1", c.name, len(generator.asked))
		}
	}
}

func TestTriageIsSkippedWhenWordsOverlap(t *testing.T) {
	inner := &fakeChatGenerator{result: map[string]any{"reply": "ok"}}
	generator := &triagingGenerator{triageModel: inner}
	readThenSay(t, generator, "resume o link")
	if len(generator.asked) != 0 || !sentEvidence(inner) {
		t.Fatalf("asked=%d evidence=%v", len(generator.asked), sentEvidence(inner))
	}
}
