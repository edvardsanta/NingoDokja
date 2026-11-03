// Package application validates proposed knowledge-use records and decides whether
// observations can inform later work. It performs no I/O and grants no authority.
package application

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	AttemptContract     = "knowledge.application.attempt.v1"
	ObservationContract = "knowledge.application.observation.v1"
	PredictionContract  = "knowledge.application.prediction.v1"
	EpisodeContract     = "knowledge.application.episode.v1"
	MaxRecordBytes      = 64 * 1024
	MaxSources          = 8
	MaxChecks           = 16
	MaxEvidence         = 16
	MaxCandidates       = 32
)

type Binding struct{ Ref, Version string }
type Evidence struct{ Ref, Hash string }
type Stamp struct {
	Contract, Ref, Scope string
	AcceptedAt           time.Time
}
type Attempt struct {
	Stamp
	ApplicationRef, WorkspaceTaskRef, WorkspaceAttemptRef, ProposalRef string
	StateRevision                                                      uint64
	Method, Criterion                                                  Binding
	InputDigest                                                        string
	Sources, Prerequisites                                             []Evidence
	AuthorizationRef, DispatchMode, Phase                              string
	DispatchedAt                                                       *time.Time
}
type Observation struct {
	Stamp
	ApplicationRef, AttemptRef, InputDigest string
	Criterion                               Binding
	Kind, Observer, Outcome                 string
	Evidence                                []Evidence
	OccurredAt                              *time.Time
	Supersedes                              string
}
type Prediction struct {
	Stamp
	ApplicationRef, AttemptRef string
	StateRevision              uint64
	Criterion                  Binding
	Target, Status             string
	Predictor                  Binding
	Horizon                    time.Time
	Evidence                   []Evidence
	Probability, Baseline      *float64
}
type Episode struct {
	Stamp
	Revision     uint64
	Attempt      Attempt
	Observations []Observation
	Constraints  []string
	Prediction   *Prediction
}

var digest = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

func ref(s string) bool {
	if strings.TrimSpace(s) != s || s == "" || len(s) > 256 || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func binding(b Binding) bool { return ref(b.Ref) && ref(b.Version) }
func bounded(v any) error {
	b, err := json.Marshal(v)
	if err != nil || len(b) > MaxRecordBytes {
		return fmt.Errorf("invalid or oversized application record")
	}
	return nil
}
func stamp(s Stamp, contract string) error {
	if s.Contract != contract || !ref(s.Ref) || !ref(s.Scope) || s.AcceptedAt.IsZero() {
		return fmt.Errorf("invalid record identity or acceptance time")
	}
	return nil
}
func evidence(items []Evidence, limit int, required bool) error {
	if len(items) > limit || (required && len(items) == 0) {
		return fmt.Errorf("invalid evidence count")
	}
	seen := map[string]bool{}
	for _, e := range items {
		if !ref(e.Ref) || !digest.MatchString(e.Hash) || seen[e.Ref] {
			return fmt.Errorf("invalid or duplicate evidence")
		}
		seen[e.Ref] = true
	}
	return nil
}

// ValidateAttempt checks structure, not whether the action-owning domain granted
// authorization or whether a recorded dispatch actually occurred.
func ValidateAttempt(a Attempt) error {
	if err := bounded(a); err != nil {
		return err
	}
	if err := stamp(a.Stamp, AttemptContract); err != nil {
		return err
	}
	for _, r := range []string{a.ApplicationRef, a.WorkspaceTaskRef, a.WorkspaceAttemptRef, a.ProposalRef, a.AuthorizationRef} {
		if !ref(r) {
			return fmt.Errorf("incomplete attempt binding")
		}
	}
	if a.StateRevision == 0 || !binding(a.Method) || !binding(a.Criterion) || !digest.MatchString(a.InputDigest) {
		return fmt.Errorf("invalid attempt version or input")
	}
	if err := evidence(a.Sources, MaxSources, true); err != nil {
		return err
	}
	if err := evidence(a.Prerequisites, MaxChecks, false); err != nil {
		return err
	}
	if a.DispatchMode != "registered_capability" && a.DispatchMode != "operator_report" {
		return fmt.Errorf("unsupported dispatch mode")
	}
	switch a.Phase {
	case "prepared":
		if a.DispatchedAt != nil {
			return fmt.Errorf("prepared attempt cannot claim dispatch")
		}
	case "dispatched", "settled":
		if a.DispatchedAt == nil {
			return fmt.Errorf("missing dispatch time")
		}
	case "interrupted": // interruption before dispatch does not manufacture an action
	default:
		return fmt.Errorf("unknown attempt phase")
	}
	if a.DispatchedAt != nil && (a.DispatchedAt.IsZero() || a.DispatchedAt.Before(a.AcceptedAt)) {
		return fmt.Errorf("invalid dispatch order")
	}
	return nil
}

func ValidateObservation(a Attempt, o Observation) error {
	if err := ValidateAttempt(a); err != nil {
		return err
	}
	if err := bounded(o); err != nil {
		return err
	}
	if err := stamp(o.Stamp, ObservationContract); err != nil {
		return err
	}
	if o.Scope != a.Scope || o.ApplicationRef != a.ApplicationRef || o.AttemptRef != a.Ref || o.InputDigest != a.InputDigest || o.Criterion != a.Criterion {
		return fmt.Errorf("observation does not bind to this attempt")
	}
	if a.DispatchedAt == nil || o.AcceptedAt.Before(*a.DispatchedAt) {
		return fmt.Errorf("observation requires prior dispatch")
	}
	if !ref(o.Observer) || (o.Supersedes != "" && (!ref(o.Supersedes) || o.Supersedes == o.Ref)) {
		return fmt.Errorf("invalid observer or correction")
	}
	if o.Kind != "executor_receipt" && o.Kind != "operator_report" && o.Kind != "simulation" {
		return fmt.Errorf("unknown observation kind")
	}
	switch o.Outcome {
	case "unknown", "succeeded", "failed", "partial":
	default:
		return fmt.Errorf("unknown observed result")
	}
	if o.OccurredAt != nil && o.OccurredAt.IsZero() {
		return fmt.Errorf("invalid occurrence time")
	}
	return evidence(o.Evidence, MaxEvidence, true)
}

func ValidatePrediction(a Attempt, p Prediction) error {
	if err := ValidateAttempt(a); err != nil {
		return err
	}
	if err := bounded(p); err != nil {
		return err
	}
	if err := stamp(p.Stamp, PredictionContract); err != nil {
		return err
	}
	if p.Scope != a.Scope || p.ApplicationRef != a.ApplicationRef || p.AttemptRef != a.Ref || p.StateRevision != a.StateRevision || p.Criterion != a.Criterion {
		return fmt.Errorf("prediction does not bind to this attempt")
	}
	if !binding(p.Predictor) || p.Target != "criterion_satisfied" || !p.Horizon.After(p.AcceptedAt) || p.AcceptedAt.Before(a.AcceptedAt) {
		return fmt.Errorf("invalid prediction target or horizon")
	}
	if a.DispatchedAt != nil && (!p.AcceptedAt.Before(*a.DispatchedAt) || !p.Horizon.After(*a.DispatchedAt)) {
		return fmt.Errorf("forecast must precede dispatch and cover it")
	}
	finite := func(v *float64) bool {
		return v == nil || (!math.IsNaN(*v) && !math.IsInf(*v, 0) && *v >= 0 && *v <= 1)
	}
	if !finite(p.Probability) || !finite(p.Baseline) {
		return fmt.Errorf("invalid forecast probability")
	}
	if (p.Status == "estimated" && p.Probability == nil) || (p.Status == "insufficient_evidence" && p.Probability != nil) || (p.Status != "estimated" && p.Status != "insufficient_evidence") {
		return fmt.Errorf("invalid forecast status")
	}
	return evidence(p.Evidence, MaxEvidence, false)
}

func ValidateEpisode(e Episode) error {
	if err := bounded(e); err != nil {
		return err
	}
	if err := stamp(e.Stamp, EpisodeContract); err != nil {
		return err
	}
	if err := ValidateAttempt(e.Attempt); err != nil {
		return err
	}
	if e.Revision == 0 || e.Scope != e.Attempt.Scope || e.AcceptedAt.Before(e.Attempt.AcceptedAt) || len(e.Observations) > MaxEvidence || len(e.Constraints) > MaxChecks {
		return fmt.Errorf("invalid episode scope, revision or bounds")
	}
	seenConstraints := map[string]bool{}
	for _, c := range e.Constraints {
		if !ref(c) || seenConstraints[c] {
			return fmt.Errorf("invalid or duplicate context constraint")
		}
		seenConstraints[c] = true
	}
	if e.Attempt.Phase == "settled" && len(e.Observations) == 0 {
		return fmt.Errorf("settled attempt needs an observation")
	}
	observations := map[string]Observation{}
	for _, o := range e.Observations {
		if err := ValidateObservation(e.Attempt, o); err != nil {
			return err
		}
		if o.AcceptedAt.After(e.AcceptedAt) {
			return fmt.Errorf("episode predates observation")
		}
		if prior, ok := observations[o.Ref]; ok {
			left, _ := json.Marshal(prior)
			right, _ := json.Marshal(o)
			if string(left) != string(right) {
				return fmt.Errorf("observation reference conflict")
			}
		}
		observations[o.Ref] = o
	}
	for _, o := range observations {
		if o.Supersedes == "" {
			continue
		}
		old, ok := observations[o.Supersedes]
		if !ok || o.Kind != old.Kind || o.Observer != old.Observer || !o.AcceptedAt.After(old.AcceptedAt) {
			return fmt.Errorf("correction needs an earlier original observation")
		}
	}
	if e.Prediction != nil {
		if e.Prediction.AcceptedAt.After(e.AcceptedAt) {
			return fmt.Errorf("episode predates prediction")
		}
		return ValidatePrediction(e.Attempt, *e.Prediction)
	}
	return nil
}
