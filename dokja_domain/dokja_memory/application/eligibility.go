package application

import (
	"encoding/json"
	"fmt"
	"time"
)

// Query is trusted application context, not a payload. Callbacks must verify
// current read access and receipt/report provenance independently of record text.
// Missing callbacks or evidence versions fail closed. There is no public writer.
type Query struct {
	MethodApplicable     bool
	VerifyAttempt        func(Attempt) bool
	Scope, Purpose       string
	Method, Criterion    Binding
	AsOf                 time.Time
	EvidenceVersions     map[string]string
	SatisfiedConstraints map[string]bool
	CanRead              func(scope, reference string) bool
	AdmitObservation     func(Observation) bool
	AllowOperatorReports bool
}
type Decision struct {
	EpisodeRef, Outcome string
	Eligible            bool
	Reasons             []string
}
type Selection struct {
	Status    string
	Decisions []Decision
}
type ForecastScore struct {
	Brier         float64
	BaselineBrier *float64
}

// Evaluate derives the observed result rather than accepting an episode's claimed
// success/eligibility. It never promotes a proposal or rewrites the input journal.
func Evaluate(e Episode, q Query) (Decision, error) {
	d := Decision{EpisodeRef: e.Ref, Outcome: "unknown", Reasons: []string{}}
	deny := func(reason string) (Decision, error) { d.Reasons = append(d.Reasons, reason); return d, nil }
	if err := ValidateEpisode(e); err != nil {
		return d, err
	}
	if q.Scope != e.Scope || q.CanRead == nil || (!q.CanRead(e.Scope, e.Ref) || !q.CanRead(e.Scope, e.Attempt.Ref)) {
		return deny("scope_denied")
	}
	if q.AsOf.IsZero() || e.AcceptedAt.After(q.AsOf) {
		return deny("episode_not_yet_available")
	}
	if q.Method != e.Attempt.Method || q.Criterion != e.Attempt.Criterion {
		return deny("method_or_criterion_mismatch")
	}
	if q.VerifyAttempt == nil || !q.VerifyAttempt(e.Attempt) {
		return deny("unverified_attempt")
	}
	if !q.MethodApplicable {
		return deny("applicability_not_established")
	}
	switch q.Purpose {
	case "success_support", "caution", "prediction", "diagnostic":
	default:
		return deny("unsupported_use")
	}
	check := func(items []Evidence) bool {
		for _, ev := range items {
			if !q.CanRead(e.Scope, ev.Ref) || q.EvidenceVersions[ev.Ref] != ev.Hash {
				return false
			}
		}
		return true
	}
	if !check(e.Attempt.Sources) || !check(e.Attempt.Prerequisites) {
		return deny("evidence_inaccessible_or_changed")
	}
	for _, condition := range e.Constraints {
		if !q.SatisfiedConstraints[condition] {
			return deny("context_not_applicable")
		}
	}
	if e.Attempt.DispatchedAt == nil {
		return deny("not_dispatched")
	}
	if q.Purpose == "prediction" && (e.Prediction == nil || e.Prediction.Status != "estimated") {
		return deny("no_prior_estimate")
	}
	superseded := map[string]bool{}
	for _, o := range e.Observations {
		if !q.CanRead(e.Scope, o.Ref) {
			return deny("scope_denied")
		}
		if !check(o.Evidence) {
			return deny("evidence_inaccessible_or_changed")
		}
		if q.AdmitObservation == nil || !q.AdmitObservation(o) {
			return deny("unverified_observer")
		}
		if o.Supersedes != "" {
			superseded[o.Supersedes] = true
		}
	}
	seen := map[string]bool{}
	observedCount := 0
	for _, o := range e.Observations {
		if superseded[o.Ref] || seen[o.Ref] {
			continue
		}
		seen[o.Ref] = true
		if o.Kind == "simulation" {
			if q.Purpose != "diagnostic" {
				return deny("simulation_not_observed_outcome")
			}
			continue
		}
		if o.Kind == "operator_report" && (!q.AllowOperatorReports || q.Purpose == "prediction") {
			return deny("operator_report_not_permitted_for_use")
		}
		if q.Purpose == "prediction" {
			if e.Prediction == nil || o.OccurredAt == nil || o.OccurredAt.Before(*e.Attempt.DispatchedAt) || o.OccurredAt.After(e.Prediction.Horizon) {
				return deny("observation_outside_prediction_target")
			}
		}
		observedCount++
		if observedCount == 1 {
			d.Outcome = o.Outcome
		} else if d.Outcome != o.Outcome {
			d.Outcome = "conflicted"
		}
	}
	switch q.Purpose {
	case "success_support":
		d.Eligible = d.Outcome == "succeeded"
	case "caution":
		d.Eligible = d.Outcome == "failed" || d.Outcome == "partial"
	case "prediction":
		if !q.CanRead(e.Scope, e.Prediction.Ref) {
			return deny("scope_denied")
		}
		if !check(e.Prediction.Evidence) {
			return deny("evidence_inaccessible_or_changed")
		}
		d.Eligible = d.Outcome == "succeeded" || d.Outcome == "failed"
	case "diagnostic":
		d.Eligible = true
	}
	if !d.Eligible {
		d.Reasons = append(d.Reasons, "outcome_not_eligible_for_use")
	}
	return d, nil
}

// Select bounds policy work and keeps exclusion reasons. No eligible candidate is
// an explicit cold start, not permission to invent a remembered method.
func Select(episodes []Episode, q Query) (Selection, error) {
	result := Selection{Status: "insufficient_evidence", Decisions: []Decision{}}
	if len(episodes) > MaxCandidates {
		return result, fmt.Errorf("at most %d episode candidates", MaxCandidates)
	}
	seen := map[[2]string]string{}
	for _, e := range episodes {
		d, err := Evaluate(e, q)
		if err != nil {
			return result, err
		}
		raw, _ := json.Marshal(e)
		key := [2]string{e.Scope, e.Ref}
		if prior, ok := seen[key]; ok {
			if prior != string(raw) {
				return result, fmt.Errorf("episode reference conflict")
			}
			continue
		}
		seen[key] = string(raw)
		result.Decisions = append(result.Decisions, d)
		if d.Eligible {
			result.Status = "eligible_evidence"
		}
	}
	return result, nil
}

// ScorePrediction is deliberately limited to an admitted executor observation of
// the same terminal criterion within its prior horizon. Other targets need policy.
func ScorePrediction(e Episode, q Query) (*ForecastScore, Decision, error) {
	q.Purpose = "prediction"
	d, err := Evaluate(e, q)
	if err != nil || !d.Eligible {
		return nil, d, err
	}
	observed := 0.0
	if d.Outcome == "succeeded" {
		observed = 1
	}
	delta := *e.Prediction.Probability - observed
	score := ForecastScore{Brier: delta * delta}
	if e.Prediction.Baseline != nil {
		v := *e.Prediction.Baseline - observed
		b := v * v
		score.BaselineBrier = &b
	}
	return &score, d, nil
}
