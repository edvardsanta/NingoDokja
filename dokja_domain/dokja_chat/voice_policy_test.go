package chat

import (
	"strings"
	"testing"
	"time"
)

func TestVoicePolicyListensPassivelyAndKeepsAnAddressedExchangeOpen(t *testing.T) {
	p := NewVoicePolicy()
	now := time.Unix(1000, 0)
	p.now = func() time.Time { return now }
	decide := func(room, user, text string) bool {
		return p.Decide(room, []VoiceTurn{{UserID: user, Text: text}}).Respond
	}
	if decide("room", "a", "What do you think about this?") {
		t.Fatal("ambient conversation triggered inference")
	}
	if !decide("room", "a", "Ningo, what do you think?") {
		t.Fatal("direct address was ignored")
	}
	now = now.Add(5 * time.Second)
	if !decide("room", "a", "That explanation missed something") {
		t.Fatal("speaker had to repeat the name")
	}
	if decide("other-room", "a", "And why?") {
		t.Fatal("engagement leaked between rooms")
	}
	if !decide("room", "b", "But how does that work?") {
		t.Fatal("another speaker could not join the exchange")
	}
	if decide("room", "c", "Someone sent me a photo") {
		t.Fatal("unrelated room speech triggered a reply")
	}
	now = now.Add(VoiceFollowupWindow + time.Second)
	if decide("room", "a", "another thought") {
		t.Fatal("engagement never expired")
	}
	if !decide("room", "a", "Dokja, explain in detail") {
		t.Fatal("second supported name was ignored")
	}
}

func TestBackchannelsDoNotTakeTheFloorOrRefreshEngagement(t *testing.T) {
	p := NewVoicePolicy()
	now := time.Unix(100, 0)
	p.now = func() time.Time { return now }
	p.Decide("room", []VoiceTurn{{UserID: "a", Text: "Ningo, explain this"}})
	now = now.Add(25 * time.Second)
	for _, text := range []string{"uhum", "ok", "aham", "sim", "Ningo, obrigado!", "thanks"} {
		if decision := p.Decide("room", []VoiceTurn{{UserID: "a", Text: text}}); decision.Respond {
			t.Fatal("backchannel requested an interruption", text)
		}
	}
	now = now.Add(6 * time.Second)
	if p.Decide("room", []VoiceTurn{{UserID: "a", Text: "another thought"}}).Respond {
		t.Fatal("acknowledgment extended the active exchange")
	}
}

func TestVoicePolicyRequestsDetailOnlyWhenExplicit(t *testing.T) {
	p := NewVoicePolicy()
	for _, tc := range []struct {
		text   string
		detail bool
	}{{"Ningo, explica isso", false}, {"Ningo, explica melhor, passo a passo", true}, {"Dokja, go deeper", true}} {
		result := p.Decide("room", []VoiceTurn{{UserID: "a", Text: tc.text}})
		if !result.Respond || result.Detailed != tc.detail {
			t.Fatal(tc, result)
		}
	}
	if reply := ConciseSpokenReply("The value is 3.14. Another sentence.", false); reply != "The value is 3.14." {
		t.Fatal(reply)
	}
	if reply := ConciseSpokenReply("First. Second.", true); reply != "First. Second." {
		t.Fatal(reply)
	}
	if reply := ConciseSpokenReply(strings.Repeat("word ", 100), false); len(strings.Fields(reply)) != 45 {
		t.Fatal("long spoken default was not bounded")
	}
}

func TestPassiveVoiceHistoryIsBoundedWithoutModelSummaries(t *testing.T) {
	messages := make([]ContextMessage, 300)
	for i := range messages {
		messages[i] = ContextMessage{Role: "user", Content: "a previous turn"}
	}
	if kept := BoundedVoiceHistory(messages); len(kept) != 127 {
		t.Fatal(len(kept))
	}
	if kept := BoundedVoiceHistory([]ContextMessage{{Role: "user", Content: strings.Repeat("x", 129*1024)}}); len(kept) != 0 {
		t.Fatal("oversized old turn survived")
	}
}

func TestFollowupWindowIncludesSpokenExplanation(t *testing.T) {
	p := NewVoicePolicy()
	now := time.Unix(1000, 0)
	p.now = func() time.Time { return now }
	decision := p.Decide("room", []VoiceTurn{{UserID: "a", Text: "Ningo"}})
	if !decision.Respond {
		t.Fatal("calling his name alone did not open the exchange")
	}
	p.CompleteReply("room", decision, strings.Repeat("word ", 150))
	now = now.Add(70 * time.Second)
	if !p.Decide("room", []VoiceTurn{{UserID: "a", Text: "That explanation missed a detail"}}).Respond {
		t.Fatal("window expired while Ningo was speaking")
	}
}

func TestStartingVoiceOpensAnExchangeWithoutRepeatingTheName(t *testing.T) {
	p := NewVoicePolicy()
	now := time.Unix(1000, 0)
	p.now = func() time.Time { return now }
	if !p.OpenListener("room", "listener-one", "starter") {
		t.Fatal("new listener was not opened")
	}
	if !p.Decide("room", []VoiceTurn{{UserID: "starter", Text: "teste"}}).Respond {
		t.Fatal("explicit start did not count as addressing the assistant")
	}
	now = now.Add(5 * time.Minute)
	if p.OpenListener("room", "listener-one", "starter") {
		t.Fatal("turn metadata reopened the listener")
	}
	if !p.Decide("room", []VoiceTurn{{UserID: "starter", Text: "teste"}}).Respond {
		t.Fatal("explicit listener starter lost engagement after the follow-up window")
	}
	if p.Decide("room", []VoiceTurn{{UserID: "starter", Text: "uhum"}}).Respond {
		t.Fatal("persistent engagement answered a backchannel")
	}
	if p.Decide("room", []VoiceTurn{{UserID: "bystander", Text: "ambient discussion"}}).Respond {
		t.Fatal("persistent engagement leaked to a bystander")
	}
	p.OpenListener("room", "listener-two", "other")
	if p.Decide("room", []VoiceTurn{{UserID: "starter", Text: "teste"}}).Respond {
		t.Fatal("another speaker inherited the starter's window")
	}
	if !p.Decide("room", []VoiceTurn{{UserID: "other", Text: "teste"}}).Respond {
		t.Fatal("restarted listener did not open its starter's exchange")
	}
}
