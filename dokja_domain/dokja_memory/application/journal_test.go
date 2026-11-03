package application

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

// A mechanical transactional fake, not a durable backend. It implements the
// contract under one lock and copies all values; it holds no admission policy.
type journalFake struct {
	mu                 sync.Mutex
	snapshots          map[[2]string]Episode
	operations         map[[2]string]JournalOperation
	owners             map[[2]string]string
	fail               bool
	cancelBeforeCommit context.CancelFunc
}

func (s *journalFake) Transact(ctx context.Context, k JournalKey, fn func(JournalState) (JournalWrite, error)) (JournalResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := JournalState{AttemptOwner: s.owners[[2]string{k.Scope, k.AttemptRef}]}
	if e, ok := s.snapshots[[2]string{k.Scope, k.EpisodeRef}]; ok {
		copy := clone(e)
		state.Current = &copy
	}
	if op, ok := s.operations[[2]string{k.Scope, k.OperationKey}]; ok {
		copy := clone(op)
		state.Operation = &copy
	}
	write, err := fn(state)
	if err != nil {
		return JournalResult{}, err
	}
	if s.cancelBeforeCommit != nil {
		s.cancelBeforeCommit()
	}
	if err := ctx.Err(); err != nil {
		return JournalResult{}, err
	}
	if s.fail {
		return JournalResult{}, errors.New("fixture commit failed")
	}
	if write.NextSnapshot != nil {
		e := clone(*write.NextSnapshot)
		s.snapshots[[2]string{k.Scope, k.EpisodeRef}] = e
		s.owners[[2]string{k.Scope, k.AttemptRef}] = k.EpisodeRef
	}
	if write.Operation != nil {
		s.operations[[2]string{k.Scope, k.OperationKey}] = clone(*write.Operation)
	}
	return clone(write.Result), nil
}
func (s *journalFake) get(k JournalKey) Episode {
	s.mu.Lock()
	defer s.mu.Unlock()
	return clone(s.snapshots[[2]string{k.Scope, k.EpisodeRef}])
}
func journalFixture() (Journal, *journalFake, BeginRecord) {
	e, _ := fixture()
	a := e.Attempt
	a.AcceptedAt = time.Time{}
	a.Phase = "prepared"
	a.DispatchedAt = nil
	store := &journalFake{snapshots: map[[2]string]Episode{}, operations: map[[2]string]JournalOperation{}, owners: map[[2]string]string{}}
	ticks := 0
	j := Journal{Store: store, Clock: func() time.Time { ticks++; return time.Date(2026, 1, 1, 12, 0, ticks, 0, time.UTC) }, Admission: Admission{
		Access: func(ctx context.Context, scope, ref string) error {
			if scope != "scope:a" {
				return ErrJournalDenied
			}
			return nil
		},
		VerifyAttempt:     func(a Attempt) bool { return a.AuthorizationRef == "authorization:1" },
		VerifyObservation: func(a Attempt, o Observation) bool { return o.Observer == "producer:one" },
	}}
	r := BeginRecord{Key: JournalKey{Scope: a.Scope, EpisodeRef: "episode:1", AttemptRef: a.Ref, OperationKey: "operation:begin"}, Attempt: a, Constraints: e.Constraints}
	return j, store, r
}
func beginAndDispatch(t *testing.T, j Journal, r BeginRecord) ChangeRecord {
	t.Helper()
	if _, err := j.Begin(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	c := ChangeRecord{Key: r.Key, ExpectedRevision: 1}
	c.Key.OperationKey = "operation:dispatch"
	if _, err := j.MarkDispatched(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	c.ExpectedRevision = 2
	return c
}
func receipt(c ChangeRecord) ObserveRecord {
	e, _ := fixture()
	o := e.Observations[0]
	o.AcceptedAt = time.Time{}
	o.OccurredAt = nil
	c.Key.OperationKey = "operation:observe"
	return ObserveRecord{ChangeRecord: c, Observation: o}
}
func TestJournalBeginReplayUsesOriginalAcceptedTime(t *testing.T) {
	j, s, r := journalFixture()
	first, err := j.Begin(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := j.Begin(context.Background(), r)
	if err != nil || !replay.Replayed || replay.Changed || !reflect.DeepEqual(first.Episode, replay.Episode) {
		t.Fatal("replay changed original result", replay, err)
	}
	if first.Episode.Attempt.Phase != "prepared" || first.Episode.Attempt.DispatchedAt != nil || len(first.Episode.Observations) != 0 {
		t.Fatal("begin manufactured execution")
	}
	changed := r
	changed.Constraints = []string{"region:west"}
	if _, err = j.Begin(context.Background(), changed); !errors.Is(err, ErrJournalConflict) {
		t.Fatal("operation key admitted changed content", err)
	}
	first.Episode.Attempt.Sources[0].Hash = "mutated"
	if s.get(r.Key).Attempt.Sources[0].Hash == "mutated" {
		t.Fatal("returned value aliased stored state")
	}
}
func TestJournalAtomicFailureAndCancellationLeaveNoOperation(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		j, s, r := journalFixture()
		ctx, stop := context.WithCancel(context.Background())
		if cancel {
			s.cancelBeforeCommit = stop
		} else {
			s.fail = true
		}
		if _, err := j.Begin(ctx, r); err == nil {
			t.Fatal("failed transaction succeeded")
		}
		stop()
		if len(s.snapshots) != 0 || len(s.operations) != 0 || len(s.owners) != 0 {
			t.Fatal("partial write escaped failed transaction")
		}
		s.cancelBeforeCommit = nil
		s.fail = false
		if result, err := j.Begin(context.Background(), r); err != nil || result.Replayed {
			t.Fatal("failed attempt reserved operation key", err)
		}
	}
}
func TestJournalScopedAuthorityAndAttemptClaims(t *testing.T) {
	j, _, r := journalFixture()
	denied := r
	denied.Key.Scope = "scope:b"
	if _, err := j.Begin(context.Background(), denied); !errors.Is(err, ErrJournalDenied) {
		t.Fatal(err)
	}
	denied = r
	denied.Attempt.AuthorizationRef = "claimed:authority"
	if _, err := j.Begin(context.Background(), denied); !errors.Is(err, ErrJournalDenied) {
		t.Fatal(err)
	}
	if _, err := j.Begin(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	duplicate := r
	duplicate.Key.EpisodeRef = "episode:other"
	duplicate.Key.OperationKey = "operation:other"
	if _, err := j.Begin(context.Background(), duplicate); !errors.Is(err, ErrJournalConflict) {
		t.Fatal("attempt claimed by two episodes", err)
	}
	j.Admission.Access = nil
	if _, err := j.Begin(context.Background(), r); !errors.Is(err, ErrJournalDenied) {
		t.Fatal("replay bypassed current access", err)
	}
}
func TestJournalReceiptAdmissionAndIdempotentAppend(t *testing.T) {
	j, s, r := journalFixture()
	c := beginAndDispatch(t, j, r)
	obs := receipt(c)
	bad := obs
	bad.Observation.Kind = "simulation"
	if _, err := j.Observe(context.Background(), bad); !errors.Is(err, ErrJournalDenied) {
		t.Fatal("simulation settled attempt", err)
	}
	bad = obs
	bad.Observation.Observer = "provider:says-done"
	if _, err := j.Observe(context.Background(), bad); !errors.Is(err, ErrJournalDenied) {
		t.Fatal("unverified producer admitted", err)
	}
	bad = obs
	bad.Observation.AttemptRef = "attempt:other"
	if _, err := j.Observe(context.Background(), bad); err == nil {
		t.Fatal("wrong attempt admitted")
	}
	first, err := j.Observe(context.Background(), obs)
	if err != nil || !first.Changed || first.Episode.Revision != 3 {
		t.Fatal(first, err)
	}
	again, err := j.Observe(context.Background(), obs)
	if err != nil || !again.Replayed || again.Episode.Revision != 3 {
		t.Fatal("stale replay failed", again, err)
	}
	duplicate := obs
	duplicate.Key.OperationKey = "operation:duplicate"
	duplicate.ExpectedRevision = 3
	unchanged, err := j.Observe(context.Background(), duplicate)
	if err != nil || unchanged.Changed || unchanged.Episode.Revision != 3 {
		t.Fatal("duplicate receipt added evidence", unchanged, err)
	}
	duplicate.Key.OperationKey = "operation:conflict"
	duplicate.Observation.Outcome = "failed"
	if _, err := j.Observe(context.Background(), duplicate); !errors.Is(err, ErrJournalConflict) {
		t.Fatal("same receipt ref changed content", err)
	}
	if len(s.get(r.Key).Observations) != 1 {
		t.Fatal("duplicate counted twice")
	}
}
func TestJournalLateReceiptChangesOnlyItsOriginalAttempt(t *testing.T) {
	j, s, r := journalFixture()
	c := beginAndDispatch(t, j, r)
	interrupt := c
	interrupt.Key.OperationKey = "operation:interrupt"
	if _, err := j.MarkInterrupted(context.Background(), interrupt); err != nil {
		t.Fatal(err)
	}
	newer := r
	newer.Key.EpisodeRef = "episode:2"
	newer.Key.AttemptRef = "attempt:2"
	newer.Key.OperationKey = "operation:begin-2"
	newer.Attempt.Ref = "attempt:2"
	if _, err := j.Begin(context.Background(), newer); err != nil {
		t.Fatal(err)
	}
	old := receipt(c)
	old.ExpectedRevision = 3
	if _, err := j.Observe(context.Background(), old); err != nil {
		t.Fatal(err)
	}
	if s.get(newer.Key).Attempt.Phase != "prepared" || len(s.get(newer.Key).Observations) != 0 {
		t.Fatal("late receipt settled newer attempt")
	}
	if s.get(r.Key).Attempt.Phase != "settled" {
		t.Fatal("original attempt did not receive late evidence")
	}
}
func TestJournalCompareAndSwapAndConcurrentReplay(t *testing.T) {
	for _, sameOperation := range []bool{false, true} {
		j, s, r := journalFixture()
		c := beginAndDispatch(t, j, r)
		first := receipt(c)
		second := first
		if !sameOperation {
			second.Key.OperationKey = "operation:competing"
			second.Observation.Ref = "observation:2"
			second.Observation.Outcome = "failed"
		}
		var wg sync.WaitGroup
		results := make(chan JournalResult, 2)
		errs := make(chan error, 2)
		for _, command := range []ObserveRecord{first, second} {
			wg.Add(1)
			go func(command ObserveRecord) {
				defer wg.Done()
				v, err := j.Observe(context.Background(), command)
				results <- v
				errs <- err
			}(command)
		}
		wg.Wait()
		close(results)
		close(errs)
		changed, replayed, conflicts := 0, 0, 0
		for v := range results {
			if v.Changed {
				changed++
			}
			if v.Replayed {
				replayed++
			}
		}
		for err := range errs {
			if errors.Is(err, ErrJournalConflict) {
				conflicts++
			} else if err != nil {
				t.Fatal(err)
			}
		}
		if changed != 1 || len(s.get(r.Key).Observations) != 1 {
			t.Fatal("CAS did not select one write")
		}
		if sameOperation && (replayed != 1 || conflicts != 0) {
			t.Fatal("duplicate concurrent command was not replayed")
		}
		if !sameOperation && conflicts != 1 {
			t.Fatal("stale competing command did not conflict")
		}
	}
}
func TestJournalPredictionsAndUnknownResults(t *testing.T) {
	j, s, r := journalFixture()
	example, _ := fixture()
	p := forecast(example)
	p.AcceptedAt = time.Time{}
	r.Prediction = &p
	c := beginAndDispatch(t, j, r)
	obs := receipt(c)
	obs.Observation.Outcome = "unknown"
	v, err := j.Observe(context.Background(), obs)
	if err != nil || v.Episode.Attempt.Phase != "dispatched" {
		t.Fatal("unknown observation became terminal success", err)
	}
	_, q := fixture()
	q.AsOf = v.Episode.AcceptedAt
	if score, decision, err := ScorePrediction(v.Episode, q); err != nil || score != nil || decision.Eligible {
		t.Fatal("unknown result received a score", score, decision, err)
	}
	if !s.get(r.Key).Prediction.AcceptedAt.Before(*s.get(r.Key).Attempt.DispatchedAt) {
		t.Fatal("forecast was not recorded before dispatch")
	}
}
func TestJournalRejectsClientTimeAndObservationBeforeDispatch(t *testing.T) {
	j, _, r := journalFixture()
	r.Attempt.AcceptedAt = time.Now()
	if _, err := j.Begin(context.Background(), r); err == nil {
		t.Fatal("caller assigned acceptance time")
	}
	r.Attempt.AcceptedAt = time.Time{}
	if _, err := j.Begin(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	c := ChangeRecord{Key: r.Key, ExpectedRevision: 1}
	o := receipt(c)
	if _, err := j.Observe(context.Background(), o); err == nil {
		t.Fatal("proposal-only attempt received a receipt")
	}
}

func TestJournalObservationFailureRollsBackSnapshotAndOperation(t *testing.T) {
	j, s, r := journalFixture()
	c := beginAndDispatch(t, j, r)
	o := receipt(c)
	s.fail = true
	if _, err := j.Observe(context.Background(), o); err == nil {
		t.Fatal("failed commit returned success")
	}
	if e := s.get(r.Key); e.Revision != 2 || len(e.Observations) != 0 || e.Attempt.Phase != "dispatched" {
		t.Fatal("failed observation changed state", e)
	}
	s.fail = false
	result, err := j.Observe(context.Background(), o)
	if err != nil || result.Replayed || result.Episode.Revision != 3 {
		t.Fatal("failed operation poisoned replay", result, err)
	}
	j.Admission.VerifyObservation = func(Attempt, Observation) bool { return false }
	if _, err := j.Observe(context.Background(), o); !errors.Is(err, ErrJournalDenied) {
		t.Fatal("replay bypassed current observation admission", err)
	}
}
func TestJournalOperationKeysAreScoped(t *testing.T) {
	j, s, r := journalFixture()
	j.Admission.Access = func(context.Context, string, string) error { return nil }
	if _, err := j.Begin(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	second := r
	second.Key.Scope = "scope:b"
	second.Attempt.Scope = "scope:b"
	result, err := j.Begin(context.Background(), second)
	if err != nil || result.Replayed || result.Episode.Scope != "scope:b" {
		t.Fatal("scoped key collided", result, err)
	}
	if s.get(r.Key).Scope != "scope:a" {
		t.Fatal("other scope overwritten")
	}
}
func TestJournalPreservesConflictingAndCorrectedEvidence(t *testing.T) {
	j, _, r := journalFixture()
	c := beginAndDispatch(t, j, r)
	first := receipt(c)
	if _, err := j.Observe(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	other := first
	other.Key.OperationKey = "operation:other"
	other.ExpectedRevision = 3
	other.Observation.Ref = "observation:2"
	other.Observation.Outcome = "failed"
	result, err := j.Observe(context.Background(), other)
	if err != nil {
		t.Fatal(err)
	}
	_, q := fixture()
	q.AsOf = result.Episode.AcceptedAt
	if d := decision(t, result.Episode, q); d.Outcome != "conflicted" || d.Eligible {
		t.Fatal("conflicting evidence disappeared", d)
	}
	corrected := other
	corrected.Key.OperationKey = "operation:correct"
	corrected.ExpectedRevision = 4
	corrected.Observation.Ref = "observation:3"
	corrected.Observation.Supersedes = "observation:1"
	result, err = j.Observe(context.Background(), corrected)
	if err != nil {
		t.Fatal(err)
	}
	q.AsOf = result.Episode.AcceptedAt
	q.Purpose = "caution"
	if d := decision(t, result.Episode, q); d.Outcome != "failed" || !d.Eligible || len(result.Episode.Observations) != 3 {
		t.Fatal("correction overwrote history", d)
	}
}

func TestJournalUnknownObservationResolvesThroughExplicitCorrection(t *testing.T) {
	j, _, r := journalFixture()
	c := beginAndDispatch(t, j, r)
	unknown := receipt(c)
	unknown.Observation.Outcome = "unknown"
	if _, err := j.Observe(context.Background(), unknown); err != nil {
		t.Fatal(err)
	}
	resolved := unknown
	resolved.ExpectedRevision = 3
	resolved.Key.OperationKey = "operation:resolved"
	resolved.Observation.Ref = "observation:resolved"
	resolved.Observation.Supersedes = unknown.Observation.Ref
	resolved.Observation.Outcome = "succeeded"
	result, err := j.Observe(context.Background(), resolved)
	if err != nil {
		t.Fatal(err)
	}
	_, q := fixture()
	q.AsOf = result.Episode.AcceptedAt
	if d := decision(t, result.Episode, q); !d.Eligible || d.Outcome != "succeeded" || len(result.Episode.Observations) != 2 {
		t.Fatal("unknown observation was not resolved with preserved history", d)
	}
	replay, err := j.Observe(context.Background(), unknown)
	if err != nil || !replay.Replayed || replay.Episode.Revision != 3 || replay.Episode.Attempt.Phase != "dispatched" {
		t.Fatal("replay was relabelled as the latest state", replay, err)
	}
}
