package application

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
)

func fixture() (Episode, Query) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	dispatched := now.Add(time.Minute)
	occurred := dispatched.Add(time.Minute)
	ev := Evidence{Ref: "source:api", Hash: "sha256:" + strings.Repeat("a", 64)}
	receipt := Evidence{Ref: "receipt:one", Hash: "sha256:" + strings.Repeat("b", 64)}
	a := Attempt{Stamp: Stamp{Contract: AttemptContract, Ref: "attempt:1", Scope: "scope:a", AcceptedAt: now}, ApplicationRef: "application:1", WorkspaceTaskRef: "workspace:1", WorkspaceAttemptRef: "workspace-attempt:1", ProposalRef: "proposal:1", StateRevision: 1, Method: Binding{"method:retry", "v1"}, Criterion: Binding{"criterion:terminal", "v1"}, InputDigest: "sha256:" + strings.Repeat("c", 64), Sources: []Evidence{ev}, AuthorizationRef: "authorization:1", DispatchMode: "registered_capability", Phase: "dispatched", DispatchedAt: &dispatched}
	o := Observation{Stamp: Stamp{Contract: ObservationContract, Ref: "observation:1", Scope: a.Scope, AcceptedAt: occurred}, ApplicationRef: a.ApplicationRef, AttemptRef: a.Ref, InputDigest: a.InputDigest, Criterion: a.Criterion, Kind: "executor_receipt", Observer: "producer:one", Outcome: "succeeded", Evidence: []Evidence{receipt}, OccurredAt: &occurred}
	e := Episode{Stamp: Stamp{Contract: EpisodeContract, Ref: "episode:1", Scope: a.Scope, AcceptedAt: occurred.Add(time.Minute)}, Revision: 1, Attempt: a, Observations: []Observation{o}, Constraints: []string{"region:east"}}
	q := Query{Scope: a.Scope, Purpose: "success_support", Method: a.Method, Criterion: a.Criterion, AsOf: e.AcceptedAt, MethodApplicable: true, EvidenceVersions: map[string]string{ev.Ref: ev.Hash, receipt.Ref: receipt.Hash}, SatisfiedConstraints: map[string]bool{"region:east": true}, CanRead: func(scope, ref string) bool { return scope == "scope:a" }, VerifyAttempt: func(a Attempt) bool { return a.AuthorizationRef == "authorization:1" }, AdmitObservation: func(o Observation) bool { return o.Observer == "producer:one" }}
	return e, q
}
func decision(t *testing.T, e Episode, q Query) Decision {
	t.Helper()
	d, err := Evaluate(e, q)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestMeasuredOutcomeAndPurpose(t *testing.T) {
	for _, tc := range []struct {
		outcome, purpose string
		eligible         bool
	}{{"succeeded", "success_support", true}, {"failed", "success_support", false}, {"failed", "caution", true}, {"partial", "caution", true}, {"unknown", "success_support", false}, {"unknown", "diagnostic", true}} {
		t.Run(tc.outcome+"/"+tc.purpose, func(t *testing.T) {
			e, q := fixture()
			e.Observations[0].Outcome = tc.outcome
			q.Purpose = tc.purpose
			d := decision(t, e, q)
			if d.Eligible != tc.eligible || d.Outcome != tc.outcome {
				t.Fatalf("unexpected decision: %#v", d)
			}
		})
	}
}
func TestEligibilityFailsClosed(t *testing.T) {
	tests := []struct {
		name, reason string
		change       func(*Episode, *Query)
	}{
		{"scope", "scope_denied", func(e *Episode, q *Query) { q.Scope = "scope:b" }},
		{"missing ACL", "scope_denied", func(e *Episode, q *Query) { q.CanRead = nil }},
		{"revoked read", "scope_denied", func(e *Episode, q *Query) { q.CanRead = func(string, string) bool { return false } }},
		{"source changed", "evidence_inaccessible_or_changed", func(e *Episode, q *Query) { q.EvidenceVersions["source:api"] = "changed" }},
		{"receipt deleted", "evidence_inaccessible_or_changed", func(e *Episode, q *Query) { delete(q.EvidenceVersions, "receipt:one") }},
		{"method version", "method_or_criterion_mismatch", func(e *Episode, q *Query) { q.Method.Version = "v2" }},
		{"context", "context_not_applicable", func(e *Episode, q *Query) { delete(q.SatisfiedConstraints, "region:east") }},
		{"unknown applicability", "applicability_not_established", func(e *Episode, q *Query) { q.MethodApplicable = false }},
		{"unverified dispatch", "unverified_attempt", func(e *Episode, q *Query) { q.VerifyAttempt = nil }},
		{"denied dispatch", "unverified_attempt", func(e *Episode, q *Query) { q.VerifyAttempt = func(Attempt) bool { return false } }},
		{"unverified report", "unverified_observer", func(e *Episode, q *Query) { q.AdmitObservation = nil }},
		{"forged producer", "unverified_observer", func(e *Episode, q *Query) { e.Observations[0].Observer = "claimed:owner" }},
		{"simulation", "simulation_not_observed_outcome", func(e *Episode, q *Query) { e.Observations[0].Kind = "simulation" }},
		{"operator report", "operator_report_not_permitted_for_use", func(e *Episode, q *Query) { e.Observations[0].Kind = "operator_report" }},
		{"future snapshot", "episode_not_yet_available", func(e *Episode, q *Query) { q.AsOf = e.Attempt.AcceptedAt }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e, q := fixture()
			tc.change(&e, &q)
			d := decision(t, e, q)
			if d.Eligible || !reflect.DeepEqual(d.Reasons, []string{tc.reason}) {
				t.Fatalf("unexpected decision: %#v", d)
			}
		})
	}
}
func TestPreparedProposalCannotBecomeSuccess(t *testing.T) {
	e, q := fixture()
	e.Attempt.Phase = "prepared"
	e.Attempt.DispatchedAt = nil
	e.Observations = nil
	d := decision(t, e, q)
	if d.Eligible || d.Outcome != "unknown" {
		t.Fatal(d)
	}
	e.Observations = []Observation{{}}
	if _, err := Evaluate(e, q); err == nil {
		t.Fatal("manufactured receipt accepted")
	}
}
func TestInterruptedAttemptRemainsUnknownUntilItsOwnReceipt(t *testing.T) {
	e, q := fixture()
	e.Attempt.Phase = "interrupted"
	e.Observations = nil
	if d := decision(t, e, q); d.Outcome != "unknown" || d.Eligible {
		t.Fatal(d)
	}
	original, _ := fixture()
	e.Observations = original.Observations
	if !decision(t, e, q).Eligible {
		t.Fatal("late original receipt could not resolve its own attempt")
	}
	e.Attempt.Ref = "attempt:2"
	if _, err := Evaluate(e, q); err == nil {
		t.Fatal("attempt-1 receipt relabelled as attempt 2")
	}
}
func TestDuplicateConflictAndCorrections(t *testing.T) {
	e, q := fixture()
	o := e.Observations[0]
	e.Observations = append(e.Observations, o)
	if !decision(t, e, q).Eligible {
		t.Fatal("duplicate changed result")
	}
	e.Observations[1].Outcome = "failed"
	if _, err := Evaluate(e, q); err == nil {
		t.Fatal("same observation ID accepted different content")
	}
	e, q = fixture()
	other := e.Observations[0]
	other.Ref = "observation:2"
	other.Outcome = "failed"
	e.Observations = append(e.Observations, other)
	if d := decision(t, e, q); d.Outcome != "conflicted" || d.Eligible {
		t.Fatal(d)
	}
	e.Observations[1].Supersedes = e.Observations[0].Ref
	e.Observations[1].AcceptedAt = e.Observations[0].AcceptedAt.Add(time.Second)
	if d := decision(t, e, q); d.Outcome != "failed" || d.Eligible {
		t.Fatal(d)
	}
	q.Purpose = "caution"
	if !decision(t, e, q).Eligible {
		t.Fatal("corrected failure unavailable for caution")
	}
	e.Observations[1].Observer = "other:reporter"
	if err := ValidateEpisode(e); err == nil {
		t.Fatal("another observer overwrote a measurement")
	}
}
func TestObservationBindingAndBounds(t *testing.T) {
	for _, change := range []func(*Episode){
		func(e *Episode) { e.Observations[0].InputDigest = "sha256:" + strings.Repeat("d", 64) },
		func(e *Episode) { e.Observations[0].Criterion.Version = "other" },
		func(e *Episode) { e.Observations[0].Scope = "scope:b" },
		func(e *Episode) { e.Observations[0].AcceptedAt = e.Attempt.AcceptedAt },
		func(e *Episode) { e.Attempt.Ref = strings.Repeat("x", 257) },
		func(e *Episode) { e.Attempt.Sources = make([]Evidence, MaxSources+1) },
		func(e *Episode) { e.Observations = make([]Observation, MaxEvidence+1) },
		func(e *Episode) { e.Constraints = make([]string, MaxChecks+1) },
		func(e *Episode) { e.Attempt.DispatchMode = "model_says_done" },
		func(e *Episode) { e.Observations[0].Supersedes = "missing:observation" },
	} {
		e, _ := fixture()
		change(&e)
		if ValidateEpisode(e) == nil {
			t.Fatal("invalid record admitted")
		}
	}
	e, _ := fixture()
	e.Attempt.Sources[0].Ref = strings.Repeat("x", MaxRecordBytes)
	if ValidateEpisode(e) == nil {
		t.Fatal("oversized record accepted")
	}
}
func forecast(e Episode) Prediction {
	p, b := 0.8, 0.5
	return Prediction{Stamp: Stamp{Contract: PredictionContract, Ref: "prediction:1", Scope: e.Scope, AcceptedAt: e.Attempt.AcceptedAt.Add(time.Second)}, ApplicationRef: e.Attempt.ApplicationRef, AttemptRef: e.Attempt.Ref, StateRevision: e.Attempt.StateRevision, Criterion: e.Attempt.Criterion, Target: "criterion_satisfied", Status: "estimated", Predictor: Binding{"predictor:one", "v1"}, Horizon: e.AcceptedAt.Add(time.Minute), Probability: &p, Baseline: &b}
}
func TestPriorPredictionScoringAndUnknowns(t *testing.T) {
	e, q := fixture()
	p := forecast(e)
	e.Prediction = &p
	score, d, err := ScorePrediction(e, q)
	if err != nil || !d.Eligible || score == nil || math.Abs(score.Brier-0.04) > 1e-9 || score.BaselineBrier == nil || *score.BaselineBrier != 0.25 {
		t.Fatalf("unexpected forecast score: %#v %#v %v", score, d, err)
	}
	e.Prediction.Baseline = nil
	score, _, _ = ScorePrediction(e, q)
	if score.BaselineBrier != nil {
		t.Fatal("missing baseline became zero")
	}
	e.Observations = nil
	score, d, err = ScorePrediction(e, q)
	if err != nil || score != nil || d.Eligible {
		t.Fatal("missing outcome scored")
	}
	e, q = fixture()
	p = forecast(e)
	p.Status = "insufficient_evidence"
	p.Probability = nil
	e.Prediction = &p
	score, d, err = ScorePrediction(e, q)
	if err != nil || score != nil || d.Eligible {
		t.Fatal("unsupported forecast became zero")
	}
}
func TestPredictionCannotBeRetrospectiveOrAnotherTarget(t *testing.T) {
	for _, change := range []func(*Prediction){
		func(p *Prediction) { p.AcceptedAt = p.AcceptedAt.Add(time.Hour) },
		func(p *Prediction) { p.StateRevision++ },
		func(p *Prediction) { p.Criterion.Version = "other" },
		func(p *Prediction) { v := math.NaN(); p.Probability = &v },
		func(p *Prediction) { p.Target = "similarity" },
		func(p *Prediction) { p.Horizon = p.AcceptedAt },
	} {
		e, _ := fixture()
		p := forecast(e)
		change(&p)
		if ValidatePrediction(e.Attempt, p) == nil {
			t.Fatal("invalid forecast admitted")
		}
	}
	e, q := fixture()
	p := forecast(e)
	e.Prediction = &p
	e.Observations[0].Kind = "operator_report"
	q.AllowOperatorReports = true
	if score, d, err := ScorePrediction(e, q); err != nil || score != nil || d.Eligible {
		t.Fatal("operator feedback became measured scoring")
	}
	e, q = fixture()
	p = forecast(e)
	p.Horizon = e.Attempt.DispatchedAt.Add(time.Second)
	e.Prediction = &p
	if score, d, err := ScorePrediction(e, q); err != nil || score != nil || d.Eligible {
		t.Fatal("observation outside forecast window scored")
	}
}
func TestAttributedReportsAndDiagnosticsStayDistinct(t *testing.T) {
	e, q := fixture()
	e.Observations[0].Kind = "operator_report"
	q.AllowOperatorReports = true
	if !decision(t, e, q).Eligible {
		t.Fatal("explicitly permitted attributed report unavailable")
	}
	e.Observations[0].Kind = "simulation"
	q.Purpose = "diagnostic"
	if d := decision(t, e, q); !d.Eligible || d.Outcome != "unknown" {
		t.Fatal("simulation became observed success")
	}
}
func TestSelectionColdStartBoundsAndPurity(t *testing.T) {
	e, q := fixture()
	selection, err := Select(nil, q)
	if err != nil || selection.Status != "insufficient_evidence" {
		t.Fatal(selection, err)
	}
	if _, err = Select(make([]Episode, MaxCandidates+1), q); err == nil {
		t.Fatal("unbounded candidate evaluation")
	}
	before, _ := json.Marshal(e)
	selection, err = Select([]Episode{e}, q)
	after, _ := json.Marshal(e)
	if err != nil || selection.Status != "eligible_evidence" || string(before) != string(after) {
		t.Fatal("selection changed journal or failed", err)
	}
}

func TestReceiptReadAccessAndSnapshotIdentity(t *testing.T) {
	e, q := fixture()
	q.CanRead = func(scope, ref string) bool { return ref != "observation:1" }
	if d := decision(t, e, q); d.Eligible || d.Reasons[0] != "scope_denied" {
		t.Fatal(d)
	}
	e, q = fixture()
	s, err := Select([]Episode{e, e}, q)
	if err != nil || len(s.Decisions) != 1 {
		t.Fatal("duplicate episode counted twice", s, err)
	}
	other := e
	other.Revision++
	if _, err := Select([]Episode{e, other}, q); err == nil {
		t.Fatal("mixed snapshots accepted for one episode")
	}
	e.Attempt.Ref = "invalid\tref"
	if ValidateEpisode(e) == nil {
		t.Fatal("control characters accepted in identity")
	}
	e, _ = fixture()
	e.Attempt.Ref = string([]byte{255})
	if ValidateEpisode(e) == nil {
		t.Fatal("invalid UTF-8 accepted in identity")
	}
}

func TestOnlyResolvedMatchedMeasurementScores(t *testing.T) {
	for _, result := range []string{"failed", "partial", "unknown"} {
		e, q := fixture()
		p := forecast(e)
		e.Prediction = &p
		e.Observations[0].Outcome = result
		score, d, err := ScorePrediction(e, q)
		if err != nil {
			t.Fatal(err)
		}
		if result == "failed" {
			if !d.Eligible || score == nil || math.Abs(score.Brier-0.64) > 1e-9 {
				t.Fatal("failure target was not scored", score, d)
			}
		} else if d.Eligible || score != nil {
			t.Fatal("unresolved or partial target received a binary label", score, d)
		}
	}
	e, q := fixture()
	p := forecast(e)
	e.Prediction = &p
	other := e.Observations[0]
	other.Ref = "observation:2"
	other.Outcome = "failed"
	e.Observations = append(e.Observations, other)
	if score, d, err := ScorePrediction(e, q); err != nil || score != nil || d.Eligible || d.Outcome != "conflicted" {
		t.Fatal("conflicting evidence scored", score, d, err)
	}
}
