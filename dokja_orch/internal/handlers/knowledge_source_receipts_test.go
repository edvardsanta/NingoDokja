package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	application "read_books/dokja_domain/dokja_memory/application"
	"read_books/internal/core"
)

// Mechanical transaction fixture: admission and lifecycle decisions are real
// application.Journal code. The PostgreSQL integration covers the storage port.
type sourceJournalStore struct {
	mu         sync.Mutex
	episodes   map[string]application.Episode
	operations map[string]application.JournalOperation
	failAt     int
	calls      int
	settled    chan application.Episode
}

func newSourceJournalStore() *sourceJournalStore {
	return &sourceJournalStore{episodes: map[string]application.Episode{}, operations: map[string]application.JournalOperation{}, settled: make(chan application.Episode, 8)}
}
func detached[T any](v T) T {
	b, _ := json.Marshal(v)
	var copy T
	_ = json.Unmarshal(b, &copy)
	return copy
}
func (s *sourceJournalStore) Transact(ctx context.Context, key application.JournalKey, fn func(application.JournalState) (application.JournalWrite, error)) (application.JournalResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.calls == s.failAt {
		return application.JournalResult{}, fmt.Errorf("fixture write failed")
	}
	if err := ctx.Err(); err != nil {
		return application.JournalResult{}, err
	}
	state := application.JournalState{}
	if e, ok := s.episodes[key.EpisodeRef]; ok {
		e = detached(e)
		state.Current = &e
		state.AttemptOwner = key.EpisodeRef
	}
	if op, ok := s.operations[key.OperationKey]; ok {
		op = detached(op)
		state.Operation = &op
	}
	write, err := fn(state)
	if err != nil {
		return application.JournalResult{}, err
	}
	if write.NextSnapshot != nil {
		e := detached(*write.NextSnapshot)
		s.episodes[key.EpisodeRef] = e
		if len(e.Observations) > 0 {
			s.settled <- e
		}
	}
	if write.Operation != nil {
		s.operations[key.OperationKey] = detached(*write.Operation)
	}
	return detached(write.Result), nil
}
func sourceJournalContext(t *testing.T, ctx context.Context, store application.JournalStore, authorize func(context.Context, SourceReadPermission) (string, error)) context.Context {
	t.Helper()
	if authorize == nil {
		authorize = func(context.Context, SourceReadPermission) (string, error) { return "decision:fixture", nil }
	}
	bound, err := WithSourceReadJournal(ctx, SourceReadJournal{Scope: "scope:fixture", ApplicationRef: "application:fixture", StateRevision: 7, Store: store, Clock: time.Now, Authorize: authorize})
	if err != nil {
		t.Fatal(err)
	}
	return bound
}
func pinnedSourceEvent(store *applicationStore) core.Event {
	event := applicationEvent()
	for _, selection := range event.Payload["sources"].([]any) {
		selected := selection.(map[string]any)
		selected["text_version"] = sourceReadDigestText(store.documents[selected["source_id"].(string)])
	}
	return event
}
func sourceReadDigestText(text string) string {
	// Production hashes source bytes, whereas sourceReadDigest hashes typed JSON.
	return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(text)))
}
func TestSourceJournalNativeSuccess(t *testing.T) {
	source := &applicationStore{documents: map[string]string{"guide": "bounded guide", "api": "bounded api"}}
	store := newSourceJournalStore()
	var permissions []SourceReadPermission
	var permissionMu sync.Mutex
	ctx := sourceJournalContext(t, context.Background(), store, func(_ context.Context, p SourceReadPermission) (string, error) {
		permissionMu.Lock()
		defer permissionMu.Unlock()
		permissions = append(permissions, p)
		return "decision:fixture", nil
	})
	output, err := prepareThroughOrchestrator(ctx, source, pinnedSourceEvent(source))
	if err != nil {
		t.Fatal(err)
	}
	task := output["task"].(core.TaskResult)
	if output["status"] != "prepared" || len(task.Receipts) != 2 || len(store.episodes) != 2 {
		t.Fatalf("missing native receipts: %v", output)
	}
	for _, episode := range store.episodes {
		if episode.Revision != 3 || episode.Attempt.Phase != "settled" || episode.Attempt.WorkspaceTaskRef != task.TaskID || episode.Attempt.WorkspaceAttemptRef != task.AttemptID || episode.Attempt.StateRevision != 7 {
			t.Fatalf("lost immutable workspace binding: %+v", episode)
		}
		observation := episode.Observations[0]
		if observation.Outcome != "succeeded" || observation.Kind != "executor_receipt" || observation.Observer != sourceReadProducer || observation.Criterion.Ref != "knowledge.application.source.validated" {
			t.Fatalf("incorrect receipt: %+v", observation)
		}
		raw, _ := json.Marshal(episode)
		if strings.Contains(string(raw), "bounded guide") || strings.Contains(string(raw), "bounded api") || strings.Contains(string(raw), "Determine when") || strings.Contains(string(raw), "file:") {
			t.Fatal("journal persisted source/goal/provenance body")
		}
	}
	permissionMu.Lock()
	defer permissionMu.Unlock()
	execute := 0
	for _, p := range permissions {
		if p.Purpose == "execute" {
			execute++
			if p.Source.TextVersion == "" || p.WorkspaceAttemptRef != task.AttemptID {
				t.Fatalf("incomplete authorization: %+v", p)
			}
		}
	}
	if execute != 2 {
		t.Fatalf("expected authorization per native read, got %d", execute)
	}
}
func TestSourceJournalRequiresPinsBeforeAnyIO(t *testing.T) {
	source := &applicationStore{documents: map[string]string{"guide": "guide", "api": "api"}}
	store := newSourceJournalStore()
	event := pinnedSourceEvent(source)
	delete(event.Payload["sources"].([]any)[1].(map[string]any), "text_version")
	_, err := prepareThroughOrchestrator(sourceJournalContext(t, context.Background(), store, nil), source, event)
	if err == nil || len(source.calls) != 0 || store.calls != 0 {
		t.Fatalf("unpinned preparation performed IO: %v", err)
	}
}
func TestSourceJournalFailures(t *testing.T) {
	for _, name := range []string{"denied", "record_denied", "begin", "dispatch", "settlement", "pin_mismatch", "missing", "transport"} {
		t.Run(name, func(t *testing.T) {
			source := &applicationStore{documents: map[string]string{"guide": "guide", "api": "api"}}
			event := pinnedSourceEvent(source)
			store := newSourceJournalStore()
			authorize := func(_ context.Context, p SourceReadPermission) (string, error) {
				if name == "denied" || (name == "record_denied" && p.Purpose == "record") {
					return "", fmt.Errorf("denied")
				}
				return "decision:fixture", nil
			}
			switch name {
			case "begin":
				store.failAt = 1
			case "dispatch":
				store.failAt = 2
			case "settlement":
				store.failAt = 3
			case "pin_mismatch":
				source.documents["guide"] = "revised"
			case "missing":
				delete(source.documents, "guide")
			case "transport":
				source.fail = "guide"
			}
			output, err := prepareThroughOrchestrator(sourceJournalContext(t, context.Background(), store, authorize), source, event)
			if err != nil {
				t.Fatal(err)
			}
			if output["status"] != "not_prepared" || output["application"] != nil {
				t.Fatalf("false successful preparation: %v", output)
			}
			calls := len(source.calls)
			if name == "denied" || name == "record_denied" || name == "begin" || name == "dispatch" {
				if calls != 0 {
					t.Fatal("read before confirmed admission/dispatch")
				}
				return
			}
			if calls != 1 {
				t.Fatalf("unexpected source retries/calls: %d", calls)
			}
			if name == "settlement" {
				return
			}
			var episode application.Episode
			select {
			case episode = <-store.settled:
			case <-time.After(time.Second):
				t.Fatal("missing native observation")
			}
			want := "failed"
			if name == "transport" {
				want = "unknown"
			}
			if episode.Observations[0].Outcome != want {
				t.Fatalf("wrong native result: %+v", episode)
			}
		})
	}
}
func TestSourceJournalIgnoresClaimedBindingsInEvent(t *testing.T) {
	source := &applicationStore{documents: map[string]string{"guide": "guide", "api": "api"}}
	event := applicationEvent()
	event.Context = map[string]any{"is_owner": true, "source_read_journal": map[string]any{"enabled": true, "scope": "claimed"}}
	output, err := prepareThroughOrchestrator(context.Background(), source, event)
	if err != nil || output["status"] != "prepared" || len(output["task"].(core.TaskResult).Receipts) != 0 {
		t.Fatalf("event claims changed trusted hook: %v %v", output, err)
	}
}
func TestSourceJournalLateReceiptSurvivesTaskCancellation(t *testing.T) {
	release := make(chan struct{})
	source := &applicationStore{documents: map[string]string{"guide": "guide", "api": "api"}, block: release, started: make(chan struct{}), finished: make(chan struct{})}
	event := pinnedSourceEvent(source)
	store := newSourceJournalStore()
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := sourceJournalContext(t, parent, store, nil)
	type response struct {
		output map[string]any
		err    error
	}
	done := make(chan response, 1)
	go func() { output, err := prepareThroughOrchestrator(ctx, source, event); done <- response{output, err} }()
	select {
	case <-source.started:
	case <-time.After(3 * time.Second):
		t.Fatal("read did not start")
	}
	cancel()
	var result response
	select {
	case result = <-done:
	case <-time.After(time.Second):
		t.Fatal("cancellation waited for native read")
	}
	close(release)
	if result.err != nil || result.output["status"] != "not_prepared" || result.output["application"] != nil {
		t.Fatalf("published cancelled output: %+v", result)
	}
	select {
	case episode := <-store.settled:
		task := result.output["task"].(core.TaskResult)
		if episode.Attempt.WorkspaceAttemptRef != task.AttemptID || episode.Observations[0].Outcome != "succeeded" || len(task.Receipts) != 0 {
			t.Fatalf("late receipt lost or published into cancelled task: %+v", episode)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("late receipt was discarded")
	}
}

func TestSourceJournalRejectsOtherWorkflows(t *testing.T) {
	store := newSourceJournalStore()
	source := &applicationStore{documents: map[string]string{"guide": "guide", "api": "api"}}
	ctx := sourceJournalContext(t, context.Background(), store, nil)
	event := applicationEvent()
	event.Type = "knowledge.application.propose"
	if _, err := prepareThroughOrchestrator(ctx, source, event); err == nil || len(source.calls) != 0 || store.calls != 0 {
		t.Fatalf("journal binding silently bypassed for another action: %v", err)
	}
}
func TestSourceJournalRechecksRecordRightsAfterNativeRead(t *testing.T) {
	store := newSourceJournalStore()
	source := &applicationStore{documents: map[string]string{"guide": "guide", "api": "api"}}
	ctx := sourceJournalContext(t, context.Background(), store, func(_ context.Context, p SourceReadPermission) (string, error) {
		if p.Purpose == "record" && len(source.calls) > 0 {
			return "", fmt.Errorf("record rights revoked")
		}
		return "decision:fixture", nil
	})
	output, err := prepareThroughOrchestrator(ctx, source, pinnedSourceEvent(source))
	if err != nil || output["status"] != "not_prepared" || len(source.calls) != 1 {
		t.Fatalf("ignored revoked journal access: %v %v", output, err)
	}
	for _, episode := range store.episodes {
		if len(episode.Observations) != 0 || episode.Attempt.Phase != "dispatched" {
			t.Fatal("fabricated receipt after admission denial")
		}
	}
}
func TestSourceJournalBindingRejectsIncompleteHost(t *testing.T) {
	base := SourceReadJournal{Scope: "scope:fixture", ApplicationRef: "application:fixture", StateRevision: 1, Store: newSourceJournalStore(), Clock: time.Now, Authorize: func(context.Context, SourceReadPermission) (string, error) { return "decision:fixture", nil }}
	for _, mutate := range []func(*SourceReadJournal){
		func(b *SourceReadJournal) { b.Scope = "" }, func(b *SourceReadJournal) { b.ApplicationRef = "" }, func(b *SourceReadJournal) { b.StateRevision = 0 }, func(b *SourceReadJournal) { b.Store = nil }, func(b *SourceReadJournal) { b.Clock = nil }, func(b *SourceReadJournal) { b.Authorize = nil },
	} {
		binding := base
		mutate(&binding)
		if _, err := WithSourceReadJournal(context.Background(), binding); err == nil {
			t.Fatal("accepted incomplete native host")
		}
	}
}

// Each step of a read is one operation named by its attempt, and asking for a step again replays it.
func TestSourceJournalStepsAreNamedByTheirAttemptAndReplay(t *testing.T) {
	source := &applicationStore{documents: map[string]string{"guide": "bounded guide", "api": "bounded api"}}
	store := newSourceJournalStore()
	ctx := sourceJournalContext(t, context.Background(), store, func(context.Context, SourceReadPermission) (string, error) { return "decision:fixture", nil })
	if _, err := prepareThroughOrchestrator(ctx, source, pinnedSourceEvent(source)); err != nil {
		t.Fatal(err)
	}
	for _, episode := range store.episodes {
		attempt := episode.Attempt.Ref
		for _, step := range []string{"begin", "dispatch", "observe"} {
			if _, ok := store.operations[attempt+":"+step]; !ok {
				t.Fatalf("missing operation %s:%s in %v", attempt, step, len(store.operations))
			}
		}
		journal := application.Journal{Store: store, Clock: time.Now, Admission: application.Admission{
			Access:        func(context.Context, string, string) error { return nil },
			VerifyAttempt: func(application.Attempt) bool { return true },
		}}
		record := sourceReadRecord{journal: &journal, key: application.JournalKey{Scope: episode.Scope, EpisodeRef: episode.Ref, AttemptRef: attempt}}
		if err := record.dispatched(context.Background()); err != nil {
			t.Fatal("asking for a recorded step again must replay it, not conflict:", err)
		}
		if current := store.episodes[episode.Ref]; current.Revision != 3 || current.Attempt.Phase != "settled" {
			t.Fatalf("a replayed step changed the episode: %+v", current)
		}
	}
	if len(store.operations) != 6 {
		t.Fatal("two reads, three steps each", len(store.operations))
	}
}

// Unsettled lists, as a store with recovery would, the attempts still prepared or dispatched.
func (s *sourceJournalStore) Unsettled(_ context.Context, before time.Time, limit int) ([]application.Episode, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []application.Episode
	for _, e := range s.episodes {
		if (e.Attempt.Phase == "prepared" || e.Attempt.Phase == "dispatched") && e.Attempt.AcceptedAt.Before(before) && len(out) < limit {
			out = append(out, detached(e))
		}
	}
	return out, 0, nil
}

// A read cut off between dispatch and its receipt is interrupted at the next start, never read
// again, and an attempt of another capability in the same journal is left alone.
func TestSourceReadsLeftUnsettledAreInterruptedAtStart(t *testing.T) {
	store := newSourceJournalStore()
	source := &applicationStore{documents: map[string]string{"guide": "bounded guide", "api": "bounded api"}}
	ctx := sourceJournalContext(t, context.Background(), store, func(context.Context, SourceReadPermission) (string, error) { return "decision:fixture", nil })
	if _, err := prepareThroughOrchestrator(ctx, source, pinnedSourceEvent(source)); err != nil {
		t.Fatal(err)
	}
	// Turn the settled reads back into what a crash after dispatch leaves, and add another capability's.
	for ref, e := range store.episodes {
		e.Revision, e.Observations = 2, nil
		e.Attempt.Phase = "dispatched"
		store.episodes[ref] = e
	}
	foreign := application.Episode{}
	for _, e := range store.episodes {
		foreign = detached(e)
		break
	}
	foreign.Ref, foreign.Attempt.Ref = "episode:foreign", "attempt:foreign"
	foreign.Attempt.Method.Ref = "another.capability"
	store.episodes[foreign.Ref] = foreign
	reads := len(source.calls)

	report, err := RecoverSourceReads(context.Background(), store, time.Now().Add(time.Second))
	if err != nil || report.Interrupted != 2 {
		t.Fatal(report, err)
	}
	for ref, e := range store.episodes {
		want := "interrupted"
		if ref == foreign.Ref {
			want = "dispatched"
		}
		if e.Attempt.Phase != want {
			t.Fatalf("%s is %s, want %s", ref, e.Attempt.Phase, want)
		}
	}
	if len(source.calls) != reads {
		t.Fatal("recovery must not read a source again")
	}
}
