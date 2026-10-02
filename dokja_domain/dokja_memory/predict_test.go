package memory

import (
	"math"
	"testing"
)

func near(t *testing.T, label string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-6 {
		t.Fatalf("%s = %v, want %v", label, got, want)
	}
}

func neighbor(action, outcome string, similarity float64) Neighbor {
	return Neighbor{Ref: "ref-" + outcome, Action: action, Outcome: outcome, Similarity: similarity}
}

func TestPredictPullsSimilarNeighboursTowardTheBaseline(t *testing.T) {
	neighbors := []Neighbor{
		neighbor("tag.suggest", OutcomeAccepted, 0.9),
		neighbor("tag.suggest", OutcomeAccepted, 0.8),
		neighbor("tag.suggest", OutcomeReplaced, 0.7),
		neighbor("tag.suggest", OutcomeAccepted, 0.5), // below the similarity threshold
		neighbor("other", OutcomeAccepted, 0.95),      // another action
		neighbor("tag.suggest", OutcomeExpired, 0.9),  // no verdict
	}
	outcomes := map[string]int{OutcomeAccepted: 8, OutcomeReplaced: 4, OutcomeExpired: 3}

	got := Predict("tag.suggest", neighbors, outcomes, DefaultConfig())

	// baseline = (8+1)/(12+2); estimate = (0.9+0.8 + 2*baseline) / (0.9+0.8+0.7 + 2)
	near(t, "baseline", got.Baseline, 9.0/14.0)
	near(t, "p", got.P, (1.7+2*9.0/14.0)/4.4)
	if got.Support != 3 || got.Resolved != 12 || got.Insufficient {
		t.Fatalf("support=%d resolved=%d insufficient=%v (%s)", got.Support, got.Resolved, got.Insufficient, got.Reason)
	}
	if len(got.Evidence) != 3 || got.Evidence[0].Similarity != 0.9 {
		t.Fatalf("evidence should be the three similar neighbours, closest first: %+v", got.Evidence)
	}
}

func TestPredictFallsBackToTheBaselineWithTooLittleEvidence(t *testing.T) {
	similar := []Neighbor{
		neighbor("tag.suggest", OutcomeAccepted, 0.9),
		neighbor("tag.suggest", OutcomeAccepted, 0.8),
	}

	few := Predict("tag.suggest", similar, map[string]int{OutcomeAccepted: 9, OutcomeReplaced: 3}, DefaultConfig())
	if !few.Insufficient || few.P != few.Baseline || few.Reason != "too few similar experiences" {
		t.Fatalf("two neighbours are not enough support: %+v", few)
	}

	young := Predict("tag.suggest", append(similar, neighbor("tag.suggest", OutcomeReplaced, 0.7)),
		map[string]int{OutcomeAccepted: 2, OutcomeReplaced: 1}, DefaultConfig())
	if !young.Insufficient || young.P != young.Baseline || young.Reason != "too few resolved experiences of this action" {
		t.Fatalf("three resolved experiences are not enough history: %+v", young)
	}
}

func TestPredictWithNoHistoryIsTheNeutralBaseline(t *testing.T) {
	got := Predict("tag.suggest", nil, nil, DefaultConfig())
	near(t, "baseline", got.Baseline, 0.5)
	near(t, "p", got.P, 0.5)
	if !got.Insufficient {
		t.Fatal("no history must be flagged as insufficient")
	}
}

func TestPredictStaysNearTheBaselineWhenSupportIsThin(t *testing.T) {
	config := DefaultConfig()
	thin := []Neighbor{
		neighbor("a", OutcomeReplaced, 0.9),
		neighbor("a", OutcomeReplaced, 0.9),
		neighbor("a", OutcomeReplaced, 0.9),
	}
	got := Predict("a", thin, map[string]int{OutcomeAccepted: 10, OutcomeReplaced: 2}, config)
	// Three rejections lower the estimate, but the prior keeps it well above zero.
	if got.P >= got.Baseline || got.P < 0.3 {
		t.Fatalf("p=%v baseline=%v: expected a moderate pull down, not a collapse", got.P, got.Baseline)
	}
}

func TestScoreComparesStoredPredictionsWithStoredBaselines(t *testing.T) {
	p := func(v float64) *float64 { return &v }
	rows := []ResolvedExperience{
		{Ref: "1", Outcome: OutcomeAccepted, Predicted: p(0.8), Baseline: p(0.5)},
		{Ref: "2", Outcome: OutcomeReplaced, Predicted: p(0.3), Baseline: p(0.5)},
		{Ref: "3", Outcome: OutcomeAccepted, Predicted: p(0.6), Baseline: p(0.5)},
		{Ref: "4", Outcome: OutcomeExpired, Predicted: p(0.9), Baseline: p(0.5)}, // no verdict
		{Ref: "5", Outcome: OutcomeAccepted},                                     // never predicted
	}

	card := Score(rows, 3)

	if card.Scored != 3 || card.Unscored != 2 {
		t.Fatalf("scored=%d unscored=%d", card.Scored, card.Unscored)
	}
	near(t, "brier prediction", card.BrierPrediction, (0.04+0.09+0.16)/3)
	near(t, "brier baseline", card.BrierBaseline, 0.25)
	near(t, "skill", card.Skill, 1-((0.04+0.09+0.16)/3)/0.25)
	if !card.BeatsBaseline || !card.EnoughData {
		t.Fatalf("beats=%v enough=%v", card.BeatsBaseline, card.EnoughData)
	}
}

func TestScoreSaysWhenThereIsNotEnoughDataOrNoSkill(t *testing.T) {
	p := func(v float64) *float64 { return &v }

	if card := Score(nil, 30); card.Scored != 0 || card.BeatsBaseline || card.EnoughData {
		t.Fatalf("an empty scorecard must claim nothing: %+v", card)
	}

	worse := Score([]ResolvedExperience{
		{Outcome: OutcomeAccepted, Predicted: p(0.1), Baseline: p(0.5)},
	}, 1)
	if worse.BeatsBaseline || worse.Skill >= 0 || !worse.EnoughData {
		t.Fatalf("a prediction worse than the baseline must show negative skill: %+v", worse)
	}

	few := Score([]ResolvedExperience{
		{Outcome: OutcomeAccepted, Predicted: p(0.9), Baseline: p(0.5)},
	}, 30)
	if few.EnoughData {
		t.Fatal("one scored prediction is not enough to judge")
	}
}
