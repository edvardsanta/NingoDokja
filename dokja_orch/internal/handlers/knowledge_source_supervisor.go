package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	knowledge "read_books/dokja_domain/dokja_knowledge"
	application "read_books/dokja_domain/dokja_memory/application"
	"read_books/internal/core"
)

var (
	ErrSourceControlDenied   = errors.New("source supervision access denied")
	ErrSourceControlConflict = errors.New("source supervision revision or operation conflict")
	ErrSourceControlState    = errors.New("source supervision control is not eligible in the current state")
)

// These identities must come from the host's current authentication/policy check,
// independently of event.User, event.Context, feedback text and historical claims.
type SourceSupervisorActor struct{ Ref, DecisionRef string }
type SourceSupervisorPermission struct {
	Scope, ApplicationRef, Action string
	ExpectedRevision              uint64
}
type SourceSupervisorLimits struct {
	Reads, Controls, Bytes int
	Lifetime               time.Duration
}

// Authorization callbacks must be bounded, honor context and resolve current
// authenticated rights; they must not derive identity from candidate event data.
type SourceSupervisorOptions struct {
	Scope           string
	Store           application.JournalStore
	Authorize       func(context.Context, SourceSupervisorPermission) (SourceSupervisorActor, error)
	AuthorizeSource func(context.Context, SourceReadPermission) (string, error)
	Clock           func() time.Time
	Limits          SourceSupervisorLimits
}
type SourceControlAck struct {
	ApplicationRef, OperationRef, State string
	ControlRevision, PlanRevision       uint64
}
type SourceControlRecord struct {
	SourceControlAck
	ActorRef, DecisionRef, Action, Reason, PlanDigest string
}
type SourceSupervisorSnapshot struct {
	StartActorRef, StartDecisionRef         string
	ApplicationRef, Scope, State, Reason    string
	ControlRevision, PlanRevision, Sequence uint64
	ReadAttempts, UsedBytes, Pending        int
	Uncertain                               bool
	Deadline                                time.Time
	Request                                 knowledge.ApplicationRequest
	Controls                                []SourceControlRecord
	Settlements                             []SourceReadSettlement
	Prepared                                map[string]any
}
type sourceControlReplay struct {
	Digest string
	Ack    SourceControlAck
}

// SourceSupervisor owns ONE volatile, read-only application, not a global task
// registry. Its authenticated native host API adds no network route or startup
// registration. The host owns its lifetime and supplies separate current control
// and source permissions. Restart recovery is deliberately not inferred here.
type SourceSupervisor struct {
	mu         sync.Mutex
	handler    *KnowledgeDomainHandler
	options    SourceSupervisorOptions
	lifetime   context.Context
	snapshot   SourceSupervisorSnapshot
	operations map[string]sourceControlReplay
	changed    chan struct{}
	planCancel context.CancelFunc
}

func NewSourceSupervisor(lifetime context.Context, handler *KnowledgeDomainHandler, options SourceSupervisorOptions) (*SourceSupervisor, error) {
	if lifetime == nil || handler == nil || handler.domain == nil || !sourceReadIdentity.MatchString(options.Scope) || options.Store == nil || options.Authorize == nil || options.AuthorizeSource == nil || options.Clock == nil {
		return nil, fmt.Errorf("incomplete source supervisor host")
	}
	limits := &options.Limits
	if limits.Reads == 0 {
		limits.Reads = 32
	}
	if limits.Controls == 0 {
		limits.Controls = 32
	}
	if limits.Bytes == 0 {
		limits.Bytes = 512 * 1024
	}
	if limits.Lifetime == 0 {
		limits.Lifetime = 60 * time.Second
	}
	if limits.Reads < 1 || limits.Reads > 32 || limits.Controls < 1 || limits.Controls > 32 || limits.Bytes < 1 || limits.Bytes > 512*1024 || limits.Lifetime <= 0 || limits.Lifetime > 60*time.Second {
		return nil, fmt.Errorf("source supervision limits exceed the bounded host policy")
	}
	return &SourceSupervisor{handler: handler, options: options, lifetime: lifetime, changed: make(chan struct{}), operations: map[string]sourceControlReplay{}, snapshot: SourceSupervisorSnapshot{ApplicationRef: uuid.NewString(), Scope: options.Scope, State: "created"}}, nil
}

func sourceClone[T any](value T) T {
	raw, _ := json.Marshal(value)
	var copy T
	_ = json.Unmarshal(raw, &copy)
	return copy
}
func sourceSize(value any) int { raw, _ := json.Marshal(value); return len(raw) }
func (s *SourceSupervisor) authenticate(ctx context.Context, action string, revision uint64) (SourceSupervisorActor, error) {
	if err := ctx.Err(); err != nil {
		return SourceSupervisorActor{}, err
	}
	actor, err := s.options.Authorize(ctx, SourceSupervisorPermission{Scope: s.options.Scope, ApplicationRef: s.snapshot.ApplicationRef, Action: action, ExpectedRevision: revision})
	if ctx.Err() != nil {
		return SourceSupervisorActor{}, ctx.Err()
	}
	if err != nil || !sourceReadIdentity.MatchString(actor.Ref) || !sourceReadIdentity.MatchString(actor.DecisionRef) {
		return SourceSupervisorActor{}, ErrSourceControlDenied
	}
	return actor, nil
}
func (s *SourceSupervisor) signalLocked() {
	s.snapshot.Sequence++
	close(s.changed)
	s.changed = make(chan struct{})
}
func sourceTerminal(state string) bool {
	switch state {
	case "prepared", "failed", "cancelled", "expired", "exhausted":
		return true
	}
	return false
}
func (s *SourceSupervisor) expireLocked() {
	if s.snapshot.State != "created" && !sourceTerminal(s.snapshot.State) && (s.lifetime.Err() != nil || !s.options.Clock().Before(s.snapshot.Deadline)) {
		s.snapshot.State, s.snapshot.Reason = "expired", "application_lifetime_ended"
		s.snapshot.Prepared = nil
		if s.planCancel != nil {
			s.planCancel()
		}
		s.signalLocked()
	}
}

// Keep only bounded correlation metadata. Candidate context maps and display
// names are neither control authority nor needed by source preparation.
func sourceSupervisionEvent(event core.Event) (core.Event, error) {
	event = core.Event{EventID: event.EventID, Timestamp: event.Timestamp, Source: event.Source, Type: event.Type,
		User: core.EventUser{ID: event.User.ID}, Channel: event.Channel}
	for _, value := range []string{event.EventID, string(event.Source), event.User.ID, event.Channel.ID} {
		if len(value) > 240 {
			return core.Event{}, fmt.Errorf("supervision event correlation exceeds budget")
		}
	}
	if err := event.Normalize(); err != nil {
		return core.Event{}, err
	}
	return event, nil
}

func (s *SourceSupervisor) Start(ctx context.Context, event core.Event) (SourceSupervisorSnapshot, error) {
	actor, err := s.authenticate(ctx, "start", 0)
	if err != nil {
		return SourceSupervisorSnapshot{}, err
	}
	if event.Type != "knowledge.application.prepare" {
		return SourceSupervisorSnapshot{}, fmt.Errorf("source supervisor accepts preparation only")
	}
	request, err := knowledge.ParseApplicationRequest(event.Payload)
	if err != nil {
		return SourceSupervisorSnapshot{}, err
	}
	request, err = knowledge.ValidateSupervisedApplication(request)
	if err != nil {
		return SourceSupervisorSnapshot{}, err
	}
	event, err = sourceSupervisionEvent(event)
	if err != nil {
		return SourceSupervisorSnapshot{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.snapshot.State != "created" {
		return SourceSupervisorSnapshot{}, ErrSourceControlState
	}
	if s.lifetime.Err() != nil || ctx.Err() != nil {
		return SourceSupervisorSnapshot{}, ErrSourceControlState
	}
	if sourceSize(request) > s.options.Limits.Bytes {
		return SourceSupervisorSnapshot{}, fmt.Errorf("initial application exceeds cumulative byte budget")
	}
	now := s.options.Clock()
	if now.IsZero() {
		return SourceSupervisorSnapshot{}, fmt.Errorf("invalid source supervision clock")
	}
	s.snapshot.StartActorRef, s.snapshot.StartDecisionRef = actor.Ref, actor.DecisionRef
	s.snapshot.State = "reassessing"
	s.snapshot.ControlRevision = 1
	s.snapshot.PlanRevision = 1
	s.snapshot.Request = request
	s.snapshot.UsedBytes = sourceSize(request)
	s.snapshot.Deadline = now.Add(s.options.Limits.Lifetime)
	s.signalLocked()
	// Retain the host's authenticated context values, not request cancellation.
	// A fixed aggregate deadline and host lifetime still bound all planning.
	runCtx, cancel := context.WithDeadline(context.WithoutCancel(ctx), s.snapshot.Deadline)
	stop := context.AfterFunc(s.lifetime, cancel)
	go func() { defer cancel(); defer stop(); s.run(runCtx, event) }()
	return sourceClone(s.snapshot), nil
}

func (s *SourceSupervisor) Status(ctx context.Context) (SourceSupervisorSnapshot, error) {
	if _, err := s.authenticate(ctx, "status", 0); err != nil {
		return SourceSupervisorSnapshot{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked()
	return sourceClone(s.snapshot), nil
}

// Control acknowledges acceptance only; it does not claim that revalidation,
// replanning, a native read or cancellation of an external effect has completed.
func (s *SourceSupervisor) Control(ctx context.Context, command knowledge.SourceControl) (SourceControlAck, error) {
	actor, err := s.authenticate(ctx, "control:"+command.Action, command.ExpectedRevision)
	if err != nil {
		return SourceControlAck{}, err
	}
	if !sourceReadIdentity.MatchString(command.OperationRef) || command.ExpectedRevision == 0 {
		return SourceControlAck{}, fmt.Errorf("control requires an operation reference and expected revision")
	}
	if err := knowledge.ValidateSourceControl(command); err != nil {
		return SourceControlAck{}, err
	}
	// Detach the candidate from caller-owned pointers before hashing/storing it.
	command = sourceClone(command)
	digest := sourceReadDigest(struct {
		Actor   string
		Command knowledge.SourceControl
	}{actor.Ref, command})
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked()
	if err := ctx.Err(); err != nil {
		return SourceControlAck{}, err
	}
	if replay, ok := s.operations[command.OperationRef]; ok {
		if replay.Digest != digest {
			return SourceControlAck{}, ErrSourceControlConflict
		}
		return replay.Ack, nil
	}
	current := &s.snapshot
	if command.ExpectedRevision != current.ControlRevision {
		return SourceControlAck{}, ErrSourceControlConflict
	}
	if current.State == "created" || sourceTerminal(current.State) || len(current.Controls) >= s.options.Limits.Controls {
		return SourceControlAck{}, ErrSourceControlState
	}
	switch command.Action {
	case "pause":
		if current.State == "paused" {
			return SourceControlAck{}, ErrSourceControlState
		}
	case "resume":
		if current.State != "paused" || current.Pending != 0 || current.Uncertain {
			return SourceControlAck{}, ErrSourceControlState
		}
	}
	// A cancellation remains available when the byte budget is nearly exhausted.
	cost := sourceSize(command)
	if command.Action != "cancel" && current.UsedBytes+cost > s.options.Limits.Bytes {
		return SourceControlAck{}, fmt.Errorf("control exceeds cumulative byte budget")
	}
	current.ControlRevision++
	switch command.Action {
	case "redirect":
		current.Request = sourceClone(*command.Application)
		current.PlanRevision++
		if current.State != "paused" {
			current.State = "reassessing"
		}
	case "pause":
		current.State = "paused"
	case "resume":
		current.State = "reassessing"
	case "cancel":
		current.State = "cancelled"
	}
	current.Prepared = nil
	// Charge accepted command bytes cumulatively, including attributed feedback.
	current.UsedBytes += cost
	if len(current.Controls)+1 == s.options.Limits.Controls && current.State != "cancelled" {
		current.State, current.Reason = "exhausted", "control_budget_exhausted"
	}
	ack := SourceControlAck{ApplicationRef: current.ApplicationRef, OperationRef: command.OperationRef, State: current.State, ControlRevision: current.ControlRevision, PlanRevision: current.PlanRevision}
	current.Controls = append(current.Controls, SourceControlRecord{SourceControlAck: ack, ActorRef: actor.Ref, DecisionRef: actor.DecisionRef, Action: command.Action, Reason: command.Reason, PlanDigest: sourceReadDigest(current.Request)})
	s.operations[command.OperationRef] = sourceControlReplay{Digest: digest, Ack: ack}
	if s.planCancel != nil {
		s.planCancel()
	}
	s.signalLocked()
	return ack, nil
}

func (s *SourceSupervisor) run(ctx context.Context, event core.Event) {
	for {
		s.mu.Lock()
		s.expireLocked()
		if sourceTerminal(s.snapshot.State) {
			s.mu.Unlock()
			return
		}
		if ctx.Err() != nil {
			s.snapshot.State, s.snapshot.Reason = "expired", "application_lifetime_ended"
			s.signalLocked()
			s.mu.Unlock()
			return
		}
		if s.snapshot.State == "paused" || s.snapshot.Pending != 0 || s.snapshot.Uncertain {
			changed := s.changed
			s.mu.Unlock()
			select {
			case <-changed:
			case <-ctx.Done():
			}
			continue
		}
		revision, plan := s.snapshot.ControlRevision, s.snapshot.PlanRevision
		request := sourceClone(s.snapshot.Request)
		s.mu.Unlock()
		// Host authorization and Knowledge eligibility are rechecked outside the lock.
		_, authErr := s.authenticate(ctx, "reassess", revision)
		request, validationErr := knowledge.ValidateSupervisedApplication(request)
		s.mu.Lock()
		if revision != s.snapshot.ControlRevision || plan != s.snapshot.PlanRevision || s.snapshot.State == "paused" {
			s.mu.Unlock()
			continue
		}
		if authErr != nil || validationErr != nil {
			s.snapshot.State, s.snapshot.Reason = "failed", "reassessment_denied"
			s.signalLocked()
			s.mu.Unlock()
			return
		}
		if sourceTerminal(s.snapshot.State) {
			s.mu.Unlock()
			return
		}
		s.snapshot.State = "running"
		planCtx, cancel := context.WithCancel(ctx)
		s.planCancel = cancel
		s.signalLocked()
		s.mu.Unlock()
		result, err := s.prepare(planCtx, event, request, revision, plan)
		cancel()
		s.mu.Lock()
		s.planCancel = nil
		if revision == s.snapshot.ControlRevision && plan == s.snapshot.PlanRevision && s.snapshot.State == "running" {
			switch {
			case ctx.Err() != nil:
				s.snapshot.State, s.snapshot.Reason = "expired", "application_lifetime_ended"
			case err != nil || result["status"] != "prepared":
				s.snapshot.State, s.snapshot.Reason = "failed", "preparation_not_confirmed"
			case s.snapshot.Uncertain || s.snapshot.Pending != 0:
				s.snapshot.State, s.snapshot.Reason = "failed", "native_settlement_unconfirmed"
			default:
				s.snapshot.State = "prepared"
				s.snapshot.Prepared = sourceClone(result["application"].(map[string]any))
			}
			s.signalLocked()
		}
		s.mu.Unlock()
	}
}

func (s *SourceSupervisor) prepare(ctx context.Context, event core.Event, request knowledge.ApplicationRequest, revision, plan uint64) (map[string]any, error) {
	binding := SourceReadJournal{Scope: s.options.Scope, ApplicationRef: s.snapshot.ApplicationRef, StateRevision: revision, PlanRevision: plan, Store: s.options.Store, Clock: s.options.Clock,
		Authorize: func(ctx context.Context, p SourceReadPermission) (string, error) {
			decision, err := s.options.AuthorizeSource(ctx, p)
			if err != nil {
				return "", err
			}
			if p.Purpose == "execute" {
				s.mu.Lock()
				defer s.mu.Unlock()
				if ctx.Err() != nil || s.snapshot.State != "running" || s.snapshot.ControlRevision != revision || s.snapshot.PlanRevision != plan {
					return "", ErrSourceControlState
				}
			}
			return decision, nil
		},
		Settled: func(settlement SourceReadSettlement) {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.snapshot.Settlements = append(s.snapshot.Settlements, settlement)
			s.snapshot.UsedBytes += settlement.SourceBytes
			if s.snapshot.UsedBytes > s.options.Limits.Bytes && !sourceTerminal(s.snapshot.State) {
				s.snapshot.State, s.snapshot.Reason = "exhausted", "byte_budget_exhausted"
				s.snapshot.Prepared = nil
			}
			if !settlement.Confirmed || settlement.Outcome == "unknown" {
				s.snapshot.Uncertain = true
				s.snapshot.Reason = "native_settlement_unconfirmed"
			}
			s.signalLocked()
		},
	}
	bound, err := WithSourceReadJournal(ctx, binding)
	if err != nil {
		return nil, err
	}
	workflow := s.handler.applicationPreparationWorkflow(request)
	for index := range workflow.Steps {
		step := &workflow.Steps[index]
		original := step.Run
		isRead := index < len(request.Sources)
		step.Run = func(ctx context.Context, input core.StepInput) (core.StepResult, error) {
			s.mu.Lock()
			s.expireLocked()
			if ctx.Err() != nil || s.snapshot.State != "running" || s.snapshot.ControlRevision != revision || s.snapshot.PlanRevision != plan {
				s.mu.Unlock()
				return core.StepResult{}, ErrSourceControlState
			}
			if isRead {
				if s.snapshot.ReadAttempts >= s.options.Limits.Reads {
					s.snapshot.State, s.snapshot.Reason = "exhausted", "read_budget_exhausted"
					s.signalLocked()
					s.mu.Unlock()
					return core.StepResult{}, ErrSourceControlState
				}
				s.snapshot.ReadAttempts++
				s.snapshot.Pending++
				s.signalLocked()
			}
			s.mu.Unlock()
			result, err := original(ctx, input)
			s.mu.Lock()
			defer s.mu.Unlock()
			if isRead {
				s.snapshot.Pending--

			}
			s.signalLocked()
			return result, err
		}
	}
	// Each plan has a fresh real workspace. The logical application/revisions live
	// outside it; receipts from abandoned workspaces retain their own identities.
	raw, _ := json.Marshal(request)
	event.Payload = nil
	_ = json.Unmarshal(raw, &event.Payload)
	return s.handler.runApplicationTask(bound, event, workflow, "prepared", "application", "context")
}
