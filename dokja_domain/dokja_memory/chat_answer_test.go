package memory

import (
	"strings"
	"testing"
)

func TestChatAnswerRefIsStablePerTurn(t *testing.T) {
	first, second := ChatAnswerRef("turn-1"), ChatAnswerRef("turn-1")
	if first != second || !strings.HasPrefix(first, "chat:") || len(first) != len("chat:")+32 {
		t.Fatalf("unstable or malformed ref: %q %q", first, second)
	}
	if ChatAnswerRef("turn-2") == first {
		t.Fatal("two turns share an experience")
	}
}

func TestPrepareChatAnswerNeedsATurnAndAQuestion(t *testing.T) {
	for _, tc := range []struct{ turn, question string }{{"", "why?"}, {"turn", "   "}} {
		if _, ok := PrepareChatAnswer(tc.turn, tc.question, false); ok {
			t.Fatalf("prepared an answer from %+v", tc)
		}
	}
	answer, ok := PrepareChatAnswer("turn", strings.Repeat("q", maxContextRunes+50), true)
	if !ok || answer.Ref() != ChatAnswerRef("turn") {
		t.Fatal("a valid answer was not prepared")
	}
	record := answer.RecordRequest(nil).Event.Payload
	if len([]rune(record["context"].(string))) != maxContextRunes || record["detail"] != ChatAnswerCited || record["action"] != ChatAnswerAction {
		t.Fatalf("unexpected record: action=%v detail=%v", record["action"], record["detail"])
	}
	if _, present := record["predicted_p"]; present {
		t.Fatal("a missing prediction was invented")
	}
}

func TestChatAnswerRecordsTheFrozenPrediction(t *testing.T) {
	answer, _ := PrepareChatAnswer("turn", "what does the guide say?", false)
	record := answer.RecordRequest(&HashtagPrediction{PredictedP: 0.7, BaselineP: 0.5}).Event.Payload
	if record["predicted_p"] != 0.7 || record["baseline_p"] != 0.5 || record["detail"] != ChatAnswerUncited {
		t.Fatalf("prediction not frozen: %v", record)
	}
	if built, err := buildRecord(record); err != nil || built["action"] != ChatAnswerAction {
		t.Fatalf("memory refuses the record: %v", err)
	}
	if prediction := answer.PredictionRequest().Event.Payload; prediction["action"] != ChatAnswerAction {
		t.Fatal("prediction asked for another action")
	}
}

// Expired is not a verdict: an answer nobody judged stays out of the score instead of counting as
// wrong; useful and wrong count both ways.
func TestUnjudgedAnswersStayUnscored(t *testing.T) {
	p, b := 0.8, 0.5
	card := Score([]ResolvedExperience{
		{Action: ChatAnswerAction, Outcome: OutcomeExpired, Predicted: &p, Baseline: &b},
		{Action: ChatAnswerAction, Outcome: OutcomeAccepted, Predicted: &p, Baseline: &b},
		{Action: ChatAnswerAction, Outcome: OutcomeReplaced, Predicted: &p, Baseline: &b},
	}, 1)
	if card.Scored != 2 || card.Unscored != 1 {
		t.Fatalf("expired answer was scored: %+v", card)
	}
}
