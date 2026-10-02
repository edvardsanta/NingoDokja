package memory

import (
	"math"
	"sort"
)

// Outcomes an experience can be resolved with. accepted is the only success. expired means
// no verdict ever arrived, so it is neither a success nor a failure and is left out of
// every count that predicts or scores.
const (
	OutcomeAccepted = "accepted"
	OutcomeReplaced = "replaced"
	OutcomeIgnored  = "ignored"
	OutcomeExpired  = "expired"
)

var Outcomes = []string{OutcomeAccepted, OutcomeReplaced, OutcomeIgnored, OutcomeExpired}

func validOutcome(outcome string) bool {
	for _, known := range Outcomes {
		if outcome == known {
			return true
		}
	}
	return false
}

// verdict turns an outcome into the 0/1 target a prediction is scored against. ok is false
// for expired and for anything unknown: there is nothing to learn from them.
func verdict(outcome string) (value float64, ok bool) {
	switch outcome {
	case OutcomeAccepted:
		return 1, true
	case OutcomeReplaced, OutcomeIgnored:
		return 0, true
	}
	return 0, false
}

// Config holds the prediction thresholds. They are provisional: not calibrated against real
// data yet, and deliberately not borrowed from the knowledge base (its 0.45 was measured on
// three long documents, and an experience's context is a short message).
type Config struct {
	// MinSimilarity is the cosine score a neighbour needs to count as "similar".
	MinSimilarity float64
	// MinSupport is how many similar neighbours with a verdict a prediction needs before
	// it may differ from the baseline.
	MinSupport int
	// MinResolved is how many experiences of the action must have a verdict at all.
	MinResolved int
	// PriorWeight is how many neighbours' worth of belief the baseline counts for: with
	// little evidence the prediction stays close to it.
	PriorWeight float64
	// MinScored is how many scored predictions a scorecard needs before it means anything.
	MinScored int
}

func DefaultConfig() Config {
	return Config{MinSimilarity: 0.6, MinSupport: 3, MinResolved: 10, PriorWeight: 2, MinScored: 30}
}

// Neighbor is one earlier experience that resembles the context being predicted.
type Neighbor struct {
	Ref        string
	Action     string
	Detail     string
	Outcome    string
	Similarity float64
	// Snippet is the start of the neighbour's context, present only when it was asked for.
	Snippet string
}

type Prediction struct {
	// P is the estimated chance the action is accepted in this context.
	P float64
	// Baseline is what a predictor that ignored the context would say: how often the
	// action has been accepted so far, smoothed so it is defined with no history.
	Baseline float64
	// Support counts the similar neighbours with a verdict that fed P.
	Support int
	// Resolved counts every experience of the action that has a verdict.
	Resolved int
	// Insufficient means there was too little evidence, so P is the baseline.
	Insufficient bool
	Reason       string
	Evidence     []Neighbor
}

// maxEvidence caps the neighbours quoted back with a prediction.
const maxEvidence = 5

// Predict estimates how likely `action` is to be accepted in a context, from the earlier
// experiences closest to it. outcomes counts every stored experience of the action by
// outcome and gives the baseline.
//
// The estimate is the similarity-weighted share of acceptances among the similar
// neighbours, pulled toward the baseline by PriorWeight so a handful of neighbours cannot
// swing it far. With too little evidence it returns the baseline and says so.
func Predict(action string, neighbors []Neighbor, outcomes map[string]int, config Config) Prediction {
	accepted := outcomes[OutcomeAccepted]
	resolved := accepted + outcomes[OutcomeReplaced] + outcomes[OutcomeIgnored]
	baseline := (float64(accepted) + 1) / (float64(resolved) + 2)

	prediction := Prediction{P: baseline, Baseline: baseline, Resolved: resolved}

	var weighted, weights float64
	similar := make([]Neighbor, 0, len(neighbors))
	for _, neighbor := range neighbors {
		value, decisive := verdict(neighbor.Outcome)
		if neighbor.Action != action || !decisive || neighbor.Similarity < config.MinSimilarity {
			continue
		}
		weighted += neighbor.Similarity * value
		weights += neighbor.Similarity
		similar = append(similar, neighbor)
	}
	prediction.Support = len(similar)
	sort.SliceStable(similar, func(i, j int) bool { return similar[i].Similarity > similar[j].Similarity })
	prediction.Evidence = similar[:min(len(similar), maxEvidence)]

	switch {
	case resolved < config.MinResolved:
		prediction.Insufficient = true
		prediction.Reason = "too few resolved experiences of this action"
	case prediction.Support < config.MinSupport:
		prediction.Insufficient = true
		prediction.Reason = "too few similar experiences"
	default:
		prediction.P = (weighted + config.PriorWeight*baseline) / (weights + config.PriorWeight)
	}
	return prediction
}

// ResolvedExperience is an experience with a verdict, as the scorecard reads it. The predicted and
// baseline chances were stored when the decision was made, never recomputed later, so a
// score can only reflect what was known at the time.
type ResolvedExperience struct {
	Ref       string
	Action    string
	Outcome   string
	Predicted *float64
	Baseline  *float64
}

type Scorecard struct {
	Scored   int
	Unscored int
	// Brier scores are the mean squared gap between a chance and what happened: lower is better.
	BrierPrediction float64
	BrierBaseline   float64
	// Skill is 1 - BrierPrediction/BrierBaseline: above 0 the prediction beat the baseline.
	Skill         float64
	BeatsBaseline bool
	// EnoughData is false until MinScored predictions were scored; before that the numbers are noise.
	EnoughData bool
}

// Score compares the stored predictions with the stored baselines against what happened.
// Experiences with no verdict (expired) or with no stored prediction count as unscored.
func Score(rows []ResolvedExperience, minScored int) Scorecard {
	var card Scorecard
	var predictionError, baselineError float64
	for _, row := range rows {
		value, decisive := verdict(row.Outcome)
		if !decisive || row.Predicted == nil || row.Baseline == nil {
			card.Unscored++
			continue
		}
		card.Scored++
		predictionError += math.Pow(*row.Predicted-value, 2)
		baselineError += math.Pow(*row.Baseline-value, 2)
	}
	if card.Scored == 0 {
		return card
	}
	card.BrierPrediction = predictionError / float64(card.Scored)
	card.BrierBaseline = baselineError / float64(card.Scored)
	if card.BrierBaseline > 0 {
		card.Skill = 1 - card.BrierPrediction/card.BrierBaseline
	}
	card.BeatsBaseline = card.Skill > 0
	card.EnoughData = card.Scored >= minScored
	return card
}

// ---- reading the service's answers ----------------------------------------------------

func neighborsFrom(answer map[string]any) []Neighbor {
	raw, _ := answer["neighbors"].([]any)
	neighbors := make([]Neighbor, 0, len(raw))
	for _, item := range raw {
		entry, _ := item.(map[string]any)
		similarity, _ := number(entry, "similarity")
		neighbors = append(neighbors, Neighbor{
			Ref:        text(entry, "ref"),
			Action:     text(entry, "action"),
			Detail:     text(entry, "detail"),
			Outcome:    text(entry, "outcome"),
			Similarity: similarity,
			Snippet:    text(entry, "context_snippet"),
		})
	}
	return neighbors
}

func outcomeCounts(answer map[string]any) map[string]int {
	counts := map[string]int{}
	raw, _ := answer["outcomes"].(map[string]any)
	for outcome := range raw {
		if value, ok := number(raw, outcome); ok && value > 0 {
			counts[outcome] = int(value)
		}
	}
	return counts
}

func resolvedFrom(answer map[string]any) []ResolvedExperience {
	raw, _ := answer["experiences"].([]any)
	rows := make([]ResolvedExperience, 0, len(raw))
	for _, item := range raw {
		entry, _ := item.(map[string]any)
		row := ResolvedExperience{Ref: text(entry, "ref"), Action: text(entry, "action"), Outcome: text(entry, "outcome")}
		if value, ok := number(entry, "predicted_p"); ok {
			row.Predicted = &value
		}
		if value, ok := number(entry, "baseline_p"); ok {
			row.Baseline = &value
		}
		rows = append(rows, row)
	}
	return rows
}
