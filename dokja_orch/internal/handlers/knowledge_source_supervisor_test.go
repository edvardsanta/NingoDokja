package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	knowledge "read_books/dokja_domain/dokja_knowledge"
	"read_books/internal/core"
)

type steeringPrincipalKey struct{}

func steeringOwner() context.Context {
	return context.WithValue(context.Background(), steeringPrincipalKey{}, "operator")
}
func steeringAuthorization(ctx context.Context, p SourceSupervisorPermission) (SourceSupervisorActor, error) {
	if ctx.Value(steeringPrincipalKey{}) != "operator" || p.Scope != "scope:fixture" {
		return SourceSupervisorActor{}, ErrSourceControlDenied
	}
	return SourceSupervisorActor{Ref: "actor:operator", DecisionRef: "decision:current"}, nil
}

type steeringSources struct {
	mu          sync.Mutex
	calls       []string
	block       <-chan struct{}
	started     chan struct{}
	blocked     bool
	unconfirmed bool
	texts       map[string]string
}

func (s *steeringSources) Dispatch(_ context.Context, action string, payload map[string]any) (map[string]any, error) {
	id, _ := payload["source_id"].(string)
	s.mu.Lock()
	s.calls = append(s.calls, id)
	text := "text:" + id
	if value, ok := s.texts[id]; ok {
		text = value
	}
	block := s.block != nil && !s.blocked && id == "guide"
	if block {
		s.blocked = true
	}
	s.mu.Unlock()
	if block {
		close(s.started)
		<-s.block
	}
	if action != "knowledge.list" {
		return nil, fmt.Errorf("unexpected action")
	}
	if s.unconfirmed {
		return nil, fmt.Errorf("native read unconfirmed")
	}
	return map[string]any{"documents": []any{map[string]any{"source_id": id, "text": text}}}, nil
}
func (s *steeringSources) callList() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}
func steeringEvent() core.Event {
	event := applicationEvent()
	for _, item := range event.Payload["sources"].([]any) {
		selection := item.(map[string]any)
		selection["text_version"] = sourceReadDigestText("text:" + selection["source_id"].(string))
	}
	return event
}
func steeringReplacement() knowledge.ApplicationRequest {
	return knowledge.ApplicationRequest{Contract: knowledge.ApplicationContract, Goal: "Use version one of the API, not version two", Constraints: []string{"The previous response does not establish success"}, Sources: []knowledge.ApplicationSelection{{SourceID: "v1", TextVersion: sourceReadDigestText("text:v1")}, {SourceID: "api", TextVersion: sourceReadDigestText("text:api")}}}
}
func steeringSupervisor(t *testing.T, source *steeringSources, store *sourceJournalStore, change func(*SourceSupervisorOptions)) *SourceSupervisor {
	t.Helper()
	options := SourceSupervisorOptions{Scope: "scope:fixture", Store: store, Clock: time.Now, Authorize: steeringAuthorization, AuthorizeSource: func(ctx context.Context, p SourceReadPermission) (string, error) {
		if ctx.Value(steeringPrincipalKey{}) != "operator" || ctx.Err() != nil || p.Scope != "scope:fixture" || p.Source.SourceID == "private" {
			return "", ErrSourceControlDenied
		}
		return "decision:source", nil
	}}
	if change != nil {
		change(&options)
	}
	lifetime, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	supervisor, err := NewSourceSupervisor(lifetime, NewKnowledgeDomainHandler(source), options)
	if err != nil {
		t.Fatal(err)
	}
	return supervisor
}
func steeringWait(t *testing.T, s *SourceSupervisor, condition func(SourceSupervisorSnapshot) bool) SourceSupervisorSnapshot {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		snapshot, err := s.Status(steeringOwner())
		if err != nil {
			t.Fatal(err)
		}
		if condition(snapshot) {
			return snapshot
		}
		s.mu.Lock()
		changed := s.changed
		s.mu.Unlock()
		// Notifications can race the preceding status read, so bound a missed edge.
		select {
		case <-changed:
		case <-time.After(time.Millisecond):
		case <-deadline:
			t.Fatalf("supervision did not reach expected state: %+v", snapshot)
		}
	}
}
func steeringStartBlocked(t *testing.T, source *steeringSources, s *SourceSupervisor) {
	t.Helper()
	if _, err := s.Start(steeringOwner(), steeringEvent()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-source.started:
	case <-time.After(time.Second):
		t.Fatal("native source did not start")
	}
}
func TestSourceSupervisorProgressesAutomatically(t *testing.T) {
	source := &steeringSources{}
	s := steeringSupervisor(t, source, newSourceJournalStore(), nil)
	if _, err := s.Start(steeringOwner(), steeringEvent()); err != nil {
		t.Fatal(err)
	}
	snapshot := steeringWait(t, s, func(s SourceSupervisorSnapshot) bool { return s.State == "prepared" })
	if !reflect.DeepEqual(source.callList(), []string{"guide", "api"}) || len(snapshot.Settlements) != 2 || snapshot.ReadAttempts != 2 || snapshot.Pending != 0 || snapshot.ControlRevision != 1 || snapshot.PlanRevision != 1 || snapshot.Prepared["applicability"] != "unassessed" {
		t.Fatalf("unexpected automatic execution: %+v", snapshot)
	}
}
func TestSourceSupervisorRejectsUntrustedControlClaims(t *testing.T) {
	source := &steeringSources{}
	s := steeringSupervisor(t, source, newSourceJournalStore(), nil)
	event := steeringEvent()
	event.Context = map[string]any{"is_owner": true, "actor": "operator"}
	event.User.ID = "operator"
	if _, err := s.Start(context.Background(), event); !errors.Is(err, ErrSourceControlDenied) {
		t.Fatalf("trusted event owner claim: %v", err)
	}
	if _, err := s.Status(context.Background()); !errors.Is(err, ErrSourceControlDenied) {
		t.Fatal("unauthenticated status was disclosed")
	}
	if _, err := s.Control(context.Background(), knowledge.SourceControl{Action: "cancel", OperationRef: "control:one", ExpectedRevision: 1}); !errors.Is(err, ErrSourceControlDenied) {
		t.Fatal("unauthenticated control accepted")
	}
	if len(source.callList()) != 0 {
		t.Fatal("unauthenticated start performed a read")
	}
}
func TestSourceSupervisorRedirectPreservesLateOriginalReceipt(t *testing.T) {
	release := make(chan struct{})
	source := &steeringSources{block: release, started: make(chan struct{})}
	s := steeringSupervisor(t, source, newSourceJournalStore(), nil)
	steeringStartBlocked(t, source, s)
	replacement := steeringReplacement()
	ack, err := s.Control(steeringOwner(), knowledge.SourceControl{Action: "redirect", Reason: "The old plan uses version two", Application: &replacement, OperationRef: "control:redirect", ExpectedRevision: 1})
	if err != nil || ack.ControlRevision != 2 || ack.PlanRevision != 2 || ack.State != "reassessing" {
		t.Fatalf("unexpected redirect acknowledgment: %+v %v", ack, err)
	}
	snapshot, _ := s.Status(steeringOwner())
	if snapshot.Prepared != nil || !reflect.DeepEqual(source.callList(), []string{"guide"}) {
		t.Fatal("redirect falsely completed or dispatched while old read was pending")
	}
	close(release)
	snapshot = steeringWait(t, s, func(s SourceSupervisorSnapshot) bool { return s.State == "prepared" })
	if !reflect.DeepEqual(source.callList(), []string{"guide", "v1", "api"}) || snapshot.Prepared["goal"] != replacement.Goal || len(snapshot.Settlements) != 3 {
		t.Fatalf("obsolete plan advanced: %+v calls=%v", snapshot, source.callList())
	}
	old := snapshot.Settlements[0]
	fresh := snapshot.Settlements[1]
	if old.StateRevision != 1 || old.PlanRevision != 1 || !old.Confirmed || fresh.StateRevision != 2 || fresh.PlanRevision != 2 || old.WorkspaceAttemptRef == fresh.WorkspaceAttemptRef || old.AttemptRef == fresh.AttemptRef {
		t.Fatalf("late receipt was relabelled/reused: %+v", snapshot.Settlements)
	}
	if snapshot.Controls[0].ActorRef != "actor:operator" || snapshot.Controls[0].Reason == "" {
		t.Fatal("lost authenticated feedback attribution")
	}
}
func TestSourceSupervisorPauseResumeAndScopedReplay(t *testing.T) {
	release := make(chan struct{})
	source := &steeringSources{block: release, started: make(chan struct{})}
	var revoked atomic.Bool
	s := steeringSupervisor(t, source, newSourceJournalStore(), func(o *SourceSupervisorOptions) {
		o.Authorize = func(ctx context.Context, p SourceSupervisorPermission) (SourceSupervisorActor, error) {
			if revoked.Load() {
				return SourceSupervisorActor{}, ErrSourceControlDenied
			}
			return steeringAuthorization(ctx, p)
		}
	})
	steeringStartBlocked(t, source, s)
	command := knowledge.SourceControl{Action: "pause", OperationRef: "control:pause", ExpectedRevision: 1}
	ack, err := s.Control(steeringOwner(), command)
	if err != nil {
		t.Fatal(err)
	}
	if replay, err := s.Control(steeringOwner(), command); err != nil || replay != ack {
		t.Fatalf("control did not replay original acknowledgment: %+v %v", replay, err)
	}
	changed := command
	changed.Reason = "changed payload"
	if _, err := s.Control(steeringOwner(), changed); !errors.Is(err, ErrSourceControlConflict) {
		t.Fatal("changed replay overwrote original")
	}
	if _, err := s.Control(steeringOwner(), knowledge.SourceControl{Action: "resume", OperationRef: "control:early", ExpectedRevision: 2}); !errors.Is(err, ErrSourceControlState) {
		t.Fatal("resumed before old receipt settled")
	}
	close(release)
	steeringWait(t, s, func(s SourceSupervisorSnapshot) bool { return s.Pending == 0 && len(s.Settlements) == 1 })
	revoked.Store(true)
	if _, err := s.Control(steeringOwner(), command); !errors.Is(err, ErrSourceControlDenied) {
		t.Fatal("replay bypassed current authorization")
	}
	revoked.Store(false)
	resume, err := s.Control(steeringOwner(), knowledge.SourceControl{Action: "resume", OperationRef: "control:resume", ExpectedRevision: 2})
	if err != nil || resume.ControlRevision != 3 || resume.PlanRevision != 1 {
		t.Fatalf("pause/resume changed the plan revision: %+v %v", resume, err)
	}
	snapshot := steeringWait(t, s, func(s SourceSupervisorSnapshot) bool { return s.State == "prepared" })
	if snapshot.ReadAttempts != 3 || len(snapshot.Controls) != 2 || snapshot.Settlements[1].StateRevision != 3 || snapshot.Settlements[1].PlanRevision != 1 {
		t.Fatalf("resume reset state or lost revisions: %+v", snapshot)
	}
}
func TestSourceSupervisorCompetingControlsUseCAS(t *testing.T) {
	release := make(chan struct{})
	source := &steeringSources{block: release, started: make(chan struct{})}
	s := steeringSupervisor(t, source, newSourceJournalStore(), nil)
	steeringStartBlocked(t, source, s)
	var wait sync.WaitGroup
	results := make(chan error, 2)
	for _, action := range []string{"pause", "cancel"} {
		wait.Add(1)
		go func(action string) {
			defer wait.Done()
			_, err := s.Control(steeringOwner(), knowledge.SourceControl{Action: action, OperationRef: "control:" + action, ExpectedRevision: 1})
			results <- err
		}(action)
	}
	wait.Wait()
	close(results)
	accepted, conflicts := 0, 0
	for err := range results {
		if err == nil {
			accepted++
		} else if errors.Is(err, ErrSourceControlConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	close(release)
	snapshot := steeringWait(t, s, func(s SourceSupervisorSnapshot) bool { return s.Pending == 0 })
	if accepted != 1 || conflicts != 1 || snapshot.ControlRevision != 2 || len(snapshot.Controls) != 1 || len(source.callList()) != 1 {
		t.Fatalf("concurrent controls overwrote each other: %+v", snapshot)
	}
}
func TestSourceSupervisorUncertainSettlementBlocksResume(t *testing.T) {
	for _, name := range []string{"read", "journal"} {
		t.Run(name, func(t *testing.T) {
			release := make(chan struct{})
			source := &steeringSources{block: release, started: make(chan struct{}), unconfirmed: name == "read"}
			store := newSourceJournalStore()
			if name == "journal" {
				store.failAt = 3
			}
			s := steeringSupervisor(t, source, store, nil)
			steeringStartBlocked(t, source, s)
			if _, err := s.Control(steeringOwner(), knowledge.SourceControl{Action: "pause", OperationRef: "control:pause", ExpectedRevision: 1}); err != nil {
				t.Fatal(err)
			}
			close(release)
			snapshot := steeringWait(t, s, func(s SourceSupervisorSnapshot) bool { return s.Pending == 0 && s.Uncertain })
			if len(snapshot.Settlements) != 1 || snapshot.Settlements[0].Confirmed != (name == "read") {
				t.Fatalf("wrong uncertainty: %+v", snapshot)
			}
			if _, err := s.Control(steeringOwner(), knowledge.SourceControl{Action: "resume", OperationRef: "control:resume", ExpectedRevision: 2}); !errors.Is(err, ErrSourceControlState) {
				t.Fatal("unknown effect was automatically retried")
			}
			if _, err := s.Control(steeringOwner(), knowledge.SourceControl{Action: "cancel", OperationRef: "control:cancel", ExpectedRevision: 2}); err != nil {
				t.Fatal(err)
			}
			if len(source.callList()) != 1 {
				t.Fatal("uncertain native action was repeated")
			}
		})
	}
}
func TestSourceSupervisorCorrectionCannotResetReadBudget(t *testing.T) {
	release := make(chan struct{})
	source := &steeringSources{block: release, started: make(chan struct{})}
	s := steeringSupervisor(t, source, newSourceJournalStore(), func(o *SourceSupervisorOptions) { o.Limits.Reads = 2 })
	steeringStartBlocked(t, source, s)
	replacement := steeringReplacement()
	if _, err := s.Control(steeringOwner(), knowledge.SourceControl{Action: "redirect", OperationRef: "control:redirect", ExpectedRevision: 1, Reason: "Use version one", Application: &replacement}); err != nil {
		t.Fatal(err)
	}
	close(release)
	snapshot := steeringWait(t, s, func(s SourceSupervisorSnapshot) bool { return s.State == "exhausted" })
	if snapshot.ReadAttempts != 2 || snapshot.Prepared != nil || !reflect.DeepEqual(source.callList(), []string{"guide", "v1"}) {
		t.Fatalf("correction reset cumulative reads: %+v", snapshot)
	}
}
func TestSourceSupervisorCorrectionCannotBroadenReadAuthority(t *testing.T) {
	release := make(chan struct{})
	source := &steeringSources{block: release, started: make(chan struct{})}
	s := steeringSupervisor(t, source, newSourceJournalStore(), nil)
	steeringStartBlocked(t, source, s)
	replacement := steeringReplacement()
	replacement.Sources[0].SourceID = "private"
	if _, err := s.Control(steeringOwner(), knowledge.SourceControl{Action: "redirect", OperationRef: "control:redirect", ExpectedRevision: 1, Reason: "Try another source", Application: &replacement}); err != nil {
		t.Fatal(err)
	}
	close(release)
	snapshot := steeringWait(t, s, func(s SourceSupervisorSnapshot) bool { return s.State == "failed" })
	if len(source.callList()) != 1 || snapshot.Prepared != nil {
		t.Fatal("control authority granted private source access")
	}
}
func TestSourceSupervisorPauseDoesNotResetDeadline(t *testing.T) {
	release := make(chan struct{})
	source := &steeringSources{block: release, started: make(chan struct{})}
	s := steeringSupervisor(t, source, newSourceJournalStore(), func(o *SourceSupervisorOptions) { o.Limits.Lifetime = 100 * time.Millisecond })
	steeringStartBlocked(t, source, s)
	before, _ := s.Status(steeringOwner())
	if _, err := s.Control(steeringOwner(), knowledge.SourceControl{Action: "pause", OperationRef: "control:pause", ExpectedRevision: 1}); err != nil {
		t.Fatal(err)
	}
	snapshot := steeringWait(t, s, func(s SourceSupervisorSnapshot) bool { return s.State == "expired" })
	close(release)
	snapshot = steeringWait(t, s, func(s SourceSupervisorSnapshot) bool { return s.Pending == 0 })
	if !snapshot.Deadline.Equal(before.Deadline) || snapshot.Prepared != nil || len(snapshot.Settlements) != 1 || len(source.callList()) != 1 {
		t.Fatalf("pause reset lifetime or lost late receipt: %+v", snapshot)
	}
}

func TestSourceSupervisorControlBeforeClaimPreventsOldRead(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	source := &steeringSources{}
	s := steeringSupervisor(t, source, newSourceJournalStore(), func(o *SourceSupervisorOptions) {
		o.Authorize = func(ctx context.Context, p SourceSupervisorPermission) (SourceSupervisorActor, error) {
			if p.Action == "reassess" {
				once.Do(func() { close(entered); <-release })
			}
			return steeringAuthorization(ctx, p)
		}
	})
	if _, err := s.Start(steeringOwner(), steeringEvent()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("no reassessment checkpoint")
	}
	replacement := steeringReplacement()
	if _, err := s.Control(steeringOwner(), knowledge.SourceControl{Action: "redirect", OperationRef: "control:before-read", ExpectedRevision: 1, Reason: "Replace the obsolete version", Application: &replacement}); err != nil {
		t.Fatal(err)
	}
	close(release)
	snapshot := steeringWait(t, s, func(s SourceSupervisorSnapshot) bool { return s.State == "prepared" })
	if !reflect.DeepEqual(source.callList(), []string{"v1", "api"}) || snapshot.ReadAttempts != 2 {
		t.Fatalf("old plan claimed a read after correction: %+v", snapshot)
	}
}
func TestSourceSupervisorControlCountAndBytesAreBounded(t *testing.T) {
	for _, name := range []string{"controls", "bytes"} {
		t.Run(name, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			source := &steeringSources{}
			s := steeringSupervisor(t, source, newSourceJournalStore(), func(o *SourceSupervisorOptions) {
				if name == "controls" {
					o.Limits.Controls = 2
				} else {
					o.Limits.Bytes = sourceSize(mustSteeringRequest(t)) + 10
				}
				o.Authorize = func(ctx context.Context, p SourceSupervisorPermission) (SourceSupervisorActor, error) {
					if p.Action == "reassess" {
						once.Do(func() { close(entered); <-release })
					}
					return steeringAuthorization(ctx, p)
				}
			})
			if _, err := s.Start(steeringOwner(), steeringEvent()); err != nil {
				t.Fatal(err)
			}
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("no checkpoint")
			}
			if name == "controls" {
				if _, err := s.Control(steeringOwner(), knowledge.SourceControl{Action: "pause", OperationRef: "control:pause", ExpectedRevision: 1}); err != nil {
					t.Fatal(err)
				}
				ack, err := s.Control(steeringOwner(), knowledge.SourceControl{Action: "resume", OperationRef: "control:resume", ExpectedRevision: 2})
				if err != nil || ack.State != "exhausted" {
					t.Fatalf("control budget did not stop progression: %+v %v", ack, err)
				}
			} else {
				if _, err := s.Control(steeringOwner(), knowledge.SourceControl{Action: "pause", OperationRef: "control:large", ExpectedRevision: 1}); err == nil {
					t.Fatal("oversized cumulative control accepted")
				}
				if _, err := s.Control(steeringOwner(), knowledge.SourceControl{Action: "cancel", OperationRef: "control:cancel", ExpectedRevision: 1}); err != nil {
					t.Fatal("byte budget prevented cancellation")
				}
			}
			close(release)
			snapshot, _ := s.Status(steeringOwner())
			if !sourceTerminal(snapshot.State) || len(source.callList()) != 0 {
				t.Fatalf("bounded task continued: %+v", snapshot)
			}
		})
	}
}
func mustSteeringRequest(t *testing.T) knowledge.ApplicationRequest {
	t.Helper()
	r, err := knowledge.ParseApplicationRequest(steeringEvent().Payload)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func TestSourceSupervisorSnapshotAndTerminalStateAreIsolated(t *testing.T) {
	source := &steeringSources{}
	s := steeringSupervisor(t, source, newSourceJournalStore(), nil)
	if _, err := s.Start(steeringOwner(), steeringEvent()); err != nil {
		t.Fatal(err)
	}
	snapshot := steeringWait(t, s, func(s SourceSupervisorSnapshot) bool { return s.State == "prepared" })
	snapshot.Prepared["goal"] = "invented"
	snapshot.Request.Sources[0].SourceID = "foreign"
	snapshot.Settlements[0].Outcome = "invented"
	current, _ := s.Status(steeringOwner())
	if current.Prepared["goal"] == "invented" || current.Request.Sources[0].SourceID == "foreign" || current.Settlements[0].Outcome == "invented" || current.StartActorRef != "actor:operator" {
		t.Fatal("caller mutated trusted supervisor state or lost attribution")
	}
	if _, err := s.Control(steeringOwner(), knowledge.SourceControl{Action: "cancel", OperationRef: "control:late", ExpectedRevision: 1}); !errors.Is(err, ErrSourceControlState) {
		t.Fatal("terminal task accepted new control")
	}
	if _, err := s.Start(steeringOwner(), steeringEvent()); !errors.Is(err, ErrSourceControlState) {
		t.Fatal("same logical application was restarted and reset its budgets")
	}
}

func TestSourceSupervisorChangedPinExcludesOldText(t *testing.T) {
	release := make(chan struct{})
	source := &steeringSources{block: release, started: make(chan struct{}), texts: map[string]string{}}
	s := steeringSupervisor(t, source, newSourceJournalStore(), nil)
	steeringStartBlocked(t, source, s)
	request := mustSteeringRequest(t)
	request.Sources[0].TextVersion = sourceReadDigestText("Updated guide for the corrected version")
	source.mu.Lock()
	source.texts["guide"] = "Updated guide for the corrected version"
	source.mu.Unlock()
	if _, err := s.Control(steeringOwner(), knowledge.SourceControl{Action: "redirect", OperationRef: "control:pin", ExpectedRevision: 1, Reason: "Use the updated pinned guide", Application: &request}); err != nil {
		t.Fatal(err)
	}
	close(release)
	snapshot := steeringWait(t, s, func(s SourceSupervisorSnapshot) bool { return s.State == "prepared" })
	sources := snapshot.Prepared["sources"].([]any)
	guide := sources[0].(map[string]any)
	if guide["text"] != "Updated guide for the corrected version" || guide["text_version"] != request.Sources[0].TextVersion || snapshot.Settlements[0].PlanRevision != 1 || snapshot.Settlements[1].PlanRevision != 2 {
		t.Fatalf("obsolete pin/text entered revised context: %+v", snapshot)
	}
}
func TestSourceSupervisorChargesNativeBytesBeforeNextRead(t *testing.T) {
	release := make(chan struct{})
	source := &steeringSources{block: release, started: make(chan struct{})}
	s := steeringSupervisor(t, source, newSourceJournalStore(), func(o *SourceSupervisorOptions) { o.Limits.Bytes = sourceSize(mustSteeringRequest(t)) + 10 })
	steeringStartBlocked(t, source, s)
	close(release)
	snapshot := steeringWait(t, s, func(s SourceSupervisorSnapshot) bool { return s.State == "exhausted" && s.Pending == 0 })
	if snapshot.UsedBytes <= s.options.Limits.Bytes || snapshot.Prepared != nil || snapshot.Settlements[0].SourceBytes <= 0 || len(source.callList()) != 1 {
		t.Fatalf("native bytes were not charged before next dispatch: %+v", snapshot)
	}
}

func TestSourceSupervisorBoundsEventCorrelationAndDropsCandidateContext(t *testing.T) {
	event := steeringEvent()
	event.Context = map[string]any{"claimed_authority": make(chan int)}
	event.User.Name = "unused display name"
	projected, err := sourceSupervisionEvent(event)
	if err != nil || projected.EventID != event.EventID || projected.User.ID != event.User.ID || projected.Source != event.Source || projected.User.Name != "" || len(projected.Context) != 0 || len(projected.Payload) != 0 {
		t.Fatalf("invalid native event projection: %+v %v", projected, err)
	}
	event.Channel.ID = string(make([]byte, 241))
	if _, err := sourceSupervisionEvent(event); err == nil {
		t.Fatal("unbounded event correlation metadata accepted")
	}
}

func TestSourceSupervisorCandidateChecksShareActiveBudget(t *testing.T) {
	release := make(chan struct{})
	source := &steeringSources{block: release, started: make(chan struct{})}
	s := steeringSupervisor(t, source, newSourceJournalStore(), func(o *SourceSupervisorOptions) { o.Limits.Reads = 3 })
	steeringStartBlocked(t, source, s)
	if _, err := s.Control(steeringOwner(), knowledge.SourceControl{Action: "pause", OperationRef: "control:pause", ExpectedRevision: 1}); err != nil {
		t.Fatal(err)
	}
	close(release)
	steeringWait(t, s, func(s SourceSupervisorSnapshot) bool { return s.Pending == 0 })
	request := mustSteeringRequest(t)
	for i := range request.Sources {
		request.Sources[i].TextVersion = ""
	}
	raw, _ := json.Marshal(request)
	var payload map[string]any
	_ = json.Unmarshal(raw, &payload)
	preview, err := s.Preview(steeringOwner(), 2, payload)
	if err != nil || preview["status"] != "prepared" {
		t.Fatalf("candidate check failed: %v %v", preview, err)
	}
	state, _ := s.Status(steeringOwner())
	if state.ReadAttempts != 3 || state.ControlRevision != 2 || state.PlanRevision != 1 || len(state.Settlements) != 1 || state.State != "paused" {
		t.Fatalf("candidate check reset budget, accepted a plan or manufactured receipts: %+v", state)
	}
	if _, err := s.Control(steeringOwner(), knowledge.SourceControl{Action: "resume", OperationRef: "control:resume", ExpectedRevision: 2}); err != nil {
		t.Fatal(err)
	}
	state = steeringWait(t, s, func(s SourceSupervisorSnapshot) bool { return s.State == "exhausted" })
	if len(source.callList()) != 3 || state.Prepared != nil {
		t.Fatal("candidate reads bypassed the active read ceiling")
	}
}
func TestSourceSupervisorCandidateCheckRequiresPauseAndAuthority(t *testing.T) {
	release := make(chan struct{})
	source := &steeringSources{block: release, started: make(chan struct{})}
	s := steeringSupervisor(t, source, newSourceJournalStore(), nil)
	steeringStartBlocked(t, source, s)
	if _, err := s.Preview(steeringOwner(), 1, steeringEvent().Payload); err == nil {
		t.Fatal("checked a candidate while dispatch was active")
	}
	if _, err := s.Preview(context.Background(), 1, steeringEvent().Payload); !errors.Is(err, ErrSourceControlDenied) {
		t.Fatal("untrusted preview admitted")
	}
	close(release)
}
