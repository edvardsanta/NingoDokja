package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"
)

var (
	ErrJournalConflict = errors.New("application journal conflict")
	ErrJournalDenied   = errors.New("application journal admission denied")
)

// JournalStore must execute the callback once inside one atomic transaction:
// lock the scoped episode, scoped operation key and scoped attempt-owner claim;
// pass detached copies; commit snapshot, attempt claim and operation together.
// On error/cancellation nothing changes. A nil NextSnapshot means no snapshot
// write. Replay and compare-and-swap are deliberately inside the same boundary.
// No production adapter is provided by this batch.
type JournalStore interface {
	Transact(context.Context, JournalKey, func(JournalState) (JournalWrite, error)) (JournalResult, error)
}
type JournalKey struct{ Scope, EpisodeRef, AttemptRef, OperationKey string }
type JournalOperation struct {
	Digest string
	Result Episode
}
type JournalState struct {
	Current      *Episode
	Operation    *JournalOperation
	AttemptOwner string
}
type JournalWrite struct {
	NextSnapshot *Episode
	Operation    *JournalOperation
	Result       JournalResult
}
type JournalResult struct {
	Episode           Episode
	Changed, Replayed bool
}

// Admission is application-supplied trusted policy, never decoded from a payload.
// Access checks current scope/record read-write rights before a transaction.
// Verify functions must be bounded pure decisions over independently verified
// facts from the owning domains. They must not make service calls under the lock.
// Verifying an attempt authorizes recording its declared action, not executing it.
type Admission struct {
	Access            func(context.Context, string, string) error
	VerifyAttempt     func(Attempt) bool
	VerifyObservation func(Attempt, Observation) bool
}
type Journal struct {
	Store     JournalStore
	Admission Admission
	Clock     func() time.Time
}
type BeginRecord struct {
	Key         JournalKey
	Attempt     Attempt
	Constraints []string
	Prediction  *Prediction
}
type ChangeRecord struct {
	Key              JournalKey
	ExpectedRevision uint64
}
type ObserveRecord struct {
	ChangeRecord
	Observation Observation
}

func clone[T any](value T) T {
	b, _ := json.Marshal(value)
	var copy T
	_ = json.Unmarshal(b, &copy)
	return copy
}

func (j Journal) apply(ctx context.Context, key JournalKey, action string, request any, expected uint64, replayAdmission func(Episode) bool, mutate func(*Episode, time.Time) (Episode, bool, error)) (JournalResult, error) {
	if err := bounded(request); err != nil {
		return JournalResult{}, err
	}
	for _, v := range []string{key.Scope, key.EpisodeRef, key.AttemptRef, key.OperationKey} {
		if !ref(v) {
			return JournalResult{}, fmt.Errorf("invalid journal key")
		}
	}
	if j.Store == nil || j.Clock == nil || j.Admission.Access == nil || j.Admission.VerifyAttempt == nil {
		return JournalResult{}, ErrJournalDenied
	}
	if err := ctx.Err(); err != nil {
		return JournalResult{}, err
	}
	if err := j.Admission.Access(ctx, key.Scope, key.EpisodeRef); err != nil {
		return JournalResult{}, ErrJournalDenied
	}
	raw, err := json.Marshal(struct {
		Version int
		Action  string
		Request any
	}{1, action, request})
	if err != nil {
		return JournalResult{}, err
	}
	sum := sha256.Sum256(raw)
	requestDigest := "sha256:" + hex.EncodeToString(sum[:])
	return j.Store.Transact(ctx, key, func(state JournalState) (JournalWrite, error) {
		if err := ctx.Err(); err != nil {
			return JournalWrite{}, err
		}
		if state.Operation != nil {
			if state.Operation.Digest != requestDigest {
				return JournalWrite{}, ErrJournalConflict
			}
			old := state.Operation.Result
			if old.Scope != key.Scope || old.Ref != key.EpisodeRef || old.Attempt.Ref != key.AttemptRef {
				return JournalWrite{}, ErrJournalConflict
			}
			if err := ValidateEpisode(old); err != nil {
				return JournalWrite{}, err
			}
			if !j.Admission.VerifyAttempt(old.Attempt) || (replayAdmission != nil && !replayAdmission(old)) {
				return JournalWrite{}, ErrJournalDenied
			}
			return JournalWrite{Result: JournalResult{Episode: clone(old), Replayed: true}}, nil
		}
		var current *Episode
		if state.Current != nil {
			copy := clone(*state.Current)
			current = &copy
			if err := ValidateEpisode(copy); err != nil {
				return JournalWrite{}, err
			}
			if copy.Scope != key.Scope || copy.Ref != key.EpisodeRef || copy.Attempt.Ref != key.AttemptRef || copy.Revision != expected {
				return JournalWrite{}, ErrJournalConflict
			}
			if copy.Revision == math.MaxUint64 {
				return JournalWrite{}, ErrJournalConflict
			}
		} else if expected != 0 {
			return JournalWrite{}, ErrJournalConflict
		}
		if (current != nil && state.AttemptOwner != key.EpisodeRef) || (state.AttemptOwner != "" && state.AttemptOwner != key.EpisodeRef) {
			return JournalWrite{}, ErrJournalConflict
		}
		now := j.Clock()
		if now.IsZero() || (current != nil && now.Before(current.AcceptedAt)) {
			return JournalWrite{}, fmt.Errorf("invalid journal clock order")
		}
		next, changed, err := mutate(current, now)
		if err != nil {
			return JournalWrite{}, err
		}
		if next.Scope != key.Scope || next.Ref != key.EpisodeRef || next.Attempt.Ref != key.AttemptRef {
			return JournalWrite{}, ErrJournalConflict
		}
		if err := ValidateEpisode(next); err != nil {
			return JournalWrite{}, err
		}
		if !j.Admission.VerifyAttempt(next.Attempt) {
			return JournalWrite{}, ErrJournalDenied
		}
		op := JournalOperation{Digest: requestDigest, Result: clone(next)}
		write := JournalWrite{Operation: &op, Result: JournalResult{Episode: clone(next), Changed: changed}}
		if changed {
			snapshot := clone(next)
			write.NextSnapshot = &snapshot
		}
		return write, nil
	})
}

// Begin records preparation only. Operation hashing uses the original command,
// before server time is assigned, so a later replay returns the original record.
func (j Journal) Begin(ctx context.Context, r BeginRecord) (JournalResult, error) {
	if !r.Attempt.AcceptedAt.IsZero() || r.Attempt.Phase != "prepared" || r.Attempt.DispatchedAt != nil {
		return JournalResult{}, fmt.Errorf("begin requires unstamped preparation")
	}
	if r.Prediction != nil && !r.Prediction.AcceptedAt.IsZero() {
		return JournalResult{}, fmt.Errorf("prediction time is application controlled")
	}
	return j.apply(ctx, r.Key, "begin", r, 0, nil, func(current *Episode, now time.Time) (Episode, bool, error) {
		if current != nil {
			return Episode{}, false, ErrJournalConflict
		}
		a := clone(r.Attempt)
		a.AcceptedAt = now
		e := Episode{Stamp: Stamp{Contract: EpisodeContract, Ref: r.Key.EpisodeRef, Scope: r.Key.Scope, AcceptedAt: now}, Revision: 1, Attempt: a, Constraints: append([]string(nil), r.Constraints...)}
		if a.Ref != r.Key.AttemptRef || a.Scope != r.Key.Scope {
			return Episode{}, false, ErrJournalConflict
		}
		if r.Prediction != nil {
			p := clone(*r.Prediction)
			p.AcceptedAt = now
			e.Prediction = &p
		}
		return e, true, nil
	})
}

// MarkDispatched records possible dispatch; it does not send an action. A crash
// after this marker still leaves the external effect unknown and must not replay it.
func (j Journal) MarkDispatched(ctx context.Context, r ChangeRecord) (JournalResult, error) {
	return j.apply(ctx, r.Key, "mark_dispatched", r, r.ExpectedRevision, nil, func(current *Episode, now time.Time) (Episode, bool, error) {
		if current == nil || current.Attempt.Phase != "prepared" {
			return Episode{}, false, ErrJournalConflict
		}
		e := *current
		e.Revision++
		e.AcceptedAt = now
		e.Attempt.Phase = "dispatched"
		e.Attempt.DispatchedAt = &now
		if e.Prediction != nil && !e.Prediction.AcceptedAt.Before(now) {
			return Episode{}, false, fmt.Errorf("dispatch must follow prediction")
		}
		return e, true, nil
	})
}

func (j Journal) MarkInterrupted(ctx context.Context, r ChangeRecord) (JournalResult, error) {
	return j.apply(ctx, r.Key, "mark_interrupted", r, r.ExpectedRevision, nil, func(current *Episode, now time.Time) (Episode, bool, error) {
		if current == nil || current.Attempt.Phase == "settled" {
			return Episode{}, false, ErrJournalConflict
		}
		if current.Attempt.Phase == "interrupted" {
			return *current, false, nil
		}
		e := *current
		e.Revision++
		e.AcceptedAt = now
		e.Attempt.Phase = "interrupted"
		return e, true, nil
	})
}

// Observe admits a producer/report observation, never a provider's claim of
// success. A receipt remains tied to this old attempt even after another starts.
func (j Journal) Observe(ctx context.Context, r ObserveRecord) (JournalResult, error) {
	if !r.Observation.AcceptedAt.IsZero() {
		return JournalResult{}, fmt.Errorf("observation acceptance time is application controlled")
	}
	if r.Observation.Kind != "executor_receipt" && r.Observation.Kind != "operator_report" {
		return JournalResult{}, ErrJournalDenied
	}
	if j.Admission.VerifyObservation == nil {
		return JournalResult{}, ErrJournalDenied
	}
	return j.apply(ctx, r.Key, "observe", r, r.ExpectedRevision, func(old Episode) bool {
		for _, observation := range old.Observations {
			if observation.Ref == r.Observation.Ref {
				return j.Admission.VerifyObservation(old.Attempt, observation)
			}
		}
		return false
	}, func(current *Episode, now time.Time) (Episode, bool, error) {
		if current == nil {
			return Episode{}, false, ErrJournalConflict
		}
		o := clone(r.Observation)
		o.AcceptedAt = now
		if err := ValidateObservation(current.Attempt, o); err != nil {
			return Episode{}, false, err
		}
		if !j.Admission.VerifyObservation(current.Attempt, o) {
			return Episode{}, false, ErrJournalDenied
		}
		for _, prior := range current.Observations {
			if prior.Ref != o.Ref {
				continue
			}
			original := prior
			original.AcceptedAt = time.Time{}
			incoming := o
			incoming.AcceptedAt = time.Time{}
			a, _ := json.Marshal(original)
			b, _ := json.Marshal(incoming)
			if string(a) != string(b) {
				return Episode{}, false, ErrJournalConflict
			}
			return *current, false, nil
		}
		e := *current
		e.Revision++
		e.AcceptedAt = now
		e.Observations = append(e.Observations, o)
		if o.Outcome != "unknown" {
			e.Attempt.Phase = "settled"
		}
		return e, true, nil
	})
}
