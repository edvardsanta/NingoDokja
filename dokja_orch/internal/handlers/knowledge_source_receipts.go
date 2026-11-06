package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"time"

	"github.com/google/uuid"
	knowledge "read_books/dokja_domain/dokja_knowledge"
	application "read_books/dokja_domain/dokja_memory/application"
	"read_books/internal/core"
)

const sourceReadCapability = "knowledge.application.source.read"
const sourceReadProducer = "knowledge.source-reader.v1"

// SourceReadPermission is supplied by the native host, never event.Context.
// Execute authorizes one pinned read; record authorizes recording its original
// attempt even when planning was cancelled. Hosts must distinguish these rights.
type SourceReadPermission struct {
	Purpose                                                         string
	Scope, ApplicationRef, TaskRef, WorkspaceAttemptRef, EpisodeRef string
	StateRevision                                                   uint64
	PlanRevision                                                    uint64
	Capability                                                      string
	Source                                                          knowledge.ApplicationSelection
}

// SourceReadSettlement is a native notification after an actual read returned.
// Unconfirmed journal storage and unconfirmed reads remain explicit. This is a
// host callback, not an event-supplied observer or a new receipt writer.
type SourceReadSettlement struct {
	SourceID                                                             string
	ApplicationRef, TaskRef, WorkspaceAttemptRef, EpisodeRef, AttemptRef string
	StateRevision, PlanRevision                                          uint64
	Outcome                                                              string
	Confirmed                                                            bool
	SourceBytes                                                          int
}

// SourceReadJournal is an opt-in application dependency. Authorize must check
// current authenticated rights independently of event claims. It returns an opaque
// decision reference, not credentials. No ingress or runtime configuration installs
// this binding yet. Each preparation creates fresh native operation identities.
type SourceReadJournal struct {
	Scope, ApplicationRef string
	StateRevision         uint64
	PlanRevision          uint64
	Settled               func(SourceReadSettlement)
	Store                 application.JournalStore
	Authorize             func(context.Context, SourceReadPermission) (string, error)
	Clock                 func() time.Time
}

type sourceReadJournalKey struct{}

var sourceReadIdentity = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,240}$`)

func WithSourceReadJournal(ctx context.Context, binding SourceReadJournal) (context.Context, error) {
	if !sourceReadIdentity.MatchString(binding.Scope) || !sourceReadIdentity.MatchString(binding.ApplicationRef) || binding.StateRevision == 0 || binding.Store == nil || binding.Authorize == nil || binding.Clock == nil {
		return nil, fmt.Errorf("incomplete native source-read journal binding")
	}
	return context.WithValue(ctx, sourceReadJournalKey{}, binding), nil
}

func sourceReadDigest(value any) string {
	raw, _ := json.Marshal(value) // Only fixed native structs containing JSON primitives.
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (h *KnowledgeDomainHandler) loadApplicationSource(ctx context.Context, input core.StepInput, selected knowledge.ApplicationSelection, index int) (map[string]any, []core.Receipt, error) {
	binding, enabled := ctx.Value(sourceReadJournalKey{}).(SourceReadJournal)
	if !enabled {
		source, err := h.domain.LoadApplicationSource(ctx, selected)
		return source, nil, err
	}
	if selected.TextVersion == "" || input.TaskID == "" || input.AttemptID == "" {
		return nil, nil, fmt.Errorf("native source read needs a pinned workspace attempt")
	}
	key := application.JournalKey{Scope: binding.Scope, EpisodeRef: uuid.NewString(), AttemptRef: uuid.NewString(), OperationKey: uuid.NewString()}
	permission := SourceReadPermission{Purpose: "execute", Scope: binding.Scope, ApplicationRef: binding.ApplicationRef, StateRevision: binding.StateRevision, PlanRevision: binding.PlanRevision,
		TaskRef: input.TaskID, WorkspaceAttemptRef: input.AttemptID, EpisodeRef: key.EpisodeRef, Capability: sourceReadCapability, Source: selected}
	authorization, err := binding.Authorize(ctx, permission)
	if err != nil || !sourceReadIdentity.MatchString(authorization) {
		return nil, nil, application.ErrJournalDenied
	}
	attempt := application.Attempt{
		Stamp:          application.Stamp{Contract: application.AttemptContract, Ref: key.AttemptRef, Scope: binding.Scope},
		ApplicationRef: binding.ApplicationRef, WorkspaceTaskRef: input.TaskID, WorkspaceAttemptRef: input.AttemptID,
		ProposalRef: uuid.NewString(), StateRevision: binding.StateRevision,
		Method:    application.Binding{Ref: sourceReadCapability, Version: "1"},
		Criterion: application.Binding{Ref: "knowledge.application.source.validated", Version: "1"},
		InputDigest: sourceReadDigest(struct {
			Capability   string
			Source       knowledge.ApplicationSelection
			PlanRevision uint64 `json:"PlanRevision,omitempty"`
		}{sourceReadCapability, selected, binding.PlanRevision}),
		Sources:          []application.Evidence{{Ref: selected.SourceID, Hash: selected.TextVersion}},
		AuthorizationRef: authorization, DispatchMode: "registered_capability", Phase: "prepared",
	}
	matchesAttempt := func(actual application.Attempt) bool {
		actual.AcceptedAt = time.Time{}
		actual.DispatchedAt = nil
		actual.Phase = "prepared"
		return reflect.DeepEqual(actual, attempt)
	}
	journal := application.Journal{Store: binding.Store, Clock: binding.Clock, Admission: application.Admission{
		Access: func(ctx context.Context, scope, episode string) error {
			if scope != binding.Scope || episode != key.EpisodeRef {
				return application.ErrJournalDenied
			}
			recordPermission := permission
			recordPermission.Purpose = "record"
			decision, err := binding.Authorize(ctx, recordPermission)
			if err != nil || !sourceReadIdentity.MatchString(decision) {
				return application.ErrJournalDenied
			}
			return nil
		},
		VerifyAttempt: matchesAttempt,
	}}
	record := sourceReadRecord{journal: &journal, key: key}
	if err := record.begin(ctx, attempt); err != nil {
		return nil, nil, fmt.Errorf("source-read journal preparation was not confirmed")
	}
	if err := record.dispatched(ctx); err != nil {
		return nil, nil, fmt.Errorf("source-read journal dispatch was not confirmed")
	}
	if err := ctx.Err(); err != nil {
		// No read was started. The marker only records possible dispatch.
		settleCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		if settleErr := record.interrupted(settleCtx); settleErr != nil {
			return nil, nil, fmt.Errorf("source-read journal interruption was not confirmed")
		}
		return nil, nil, err
	}
	read := h.domain.ObserveApplicationSource(ctx, selected)
	occurred := binding.Clock()
	outcome := "unknown"
	switch read.Observation.Status {
	case "validated":
		outcome = "succeeded"
	case "validation_failed":
		outcome = "failed"
	}
	settlement := SourceReadSettlement{SourceID: selected.SourceID, ApplicationRef: binding.ApplicationRef, TaskRef: input.TaskID,
		WorkspaceAttemptRef: input.AttemptID, EpisodeRef: key.EpisodeRef, AttemptRef: attempt.Ref,
		StateRevision: binding.StateRevision, PlanRevision: binding.PlanRevision, Outcome: outcome, SourceBytes: sourceSize(read.Source)}
	if binding.Settled != nil {
		defer func() { binding.Settled(settlement) }()
	}
	observation := application.Observation{
		Stamp:          application.Stamp{Contract: application.ObservationContract, Ref: uuid.NewString(), Scope: binding.Scope},
		ApplicationRef: binding.ApplicationRef, AttemptRef: attempt.Ref, InputDigest: attempt.InputDigest,
		Criterion: attempt.Criterion, Kind: "executor_receipt", Observer: sourceReadProducer, Outcome: outcome,
		Evidence: []application.Evidence{{Ref: uuid.NewString(), Hash: sourceReadDigest(read.Observation)}}, OccurredAt: &occurred,
	}
	journal.Admission.VerifyObservation = func(actual application.Attempt, got application.Observation) bool {
		got.AcceptedAt = time.Time{}
		return matchesAttempt(actual) && sourceReadDigest(got) == sourceReadDigest(observation)
	}
	// Settlement has its own short lifetime. RunTask discards cancelled/late output,
	// but the actual native receipt still belongs to the original immutable attempt.
	settleCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if err := record.observed(settleCtx, observation); err != nil {
		return nil, nil, fmt.Errorf("source-read receipt settlement was not confirmed")
	}
	settlement.Confirmed = true
	if read.Err != nil {
		return nil, nil, read.Err
	}
	return read.Source, []core.Receipt{{ActionID: attempt.Ref, Step: fmt.Sprintf("source-%d", index), Status: "source_validated", At: occurred}}, nil
}

// sourceReadRecord writes one source read to the journal in its fixed steps: prepared (revision 1),
// possibly dispatched (2), then either a receipt or an interruption (3). Each step names its own
// operation and the revision it expects, so callers cannot get them out of order.
type sourceReadRecord struct {
	journal *application.Journal
	key     application.JournalKey
}

// step is the journal key of one step of this attempt. The operation is named by the attempt and the
// step, so asking for a step again replays what was recorded instead of becoming a new operation
// against a revision that has moved on.
func (r sourceReadRecord) step(name string) application.JournalKey {
	key := r.key
	key.OperationKey = r.key.AttemptRef + ":" + name
	return key
}

func (r sourceReadRecord) begin(ctx context.Context, attempt application.Attempt) error {
	_, err := r.journal.Begin(ctx, application.BeginRecord{Key: r.step("begin"), Attempt: attempt})
	return err
}

func (r sourceReadRecord) dispatched(ctx context.Context) error {
	_, err := r.journal.MarkDispatched(ctx, application.ChangeRecord{Key: r.step("dispatch"), ExpectedRevision: 1})
	return err
}

func (r sourceReadRecord) interrupted(ctx context.Context) error {
	_, err := r.journal.MarkInterrupted(ctx, application.ChangeRecord{Key: r.step("interrupt"), ExpectedRevision: 2})
	return err
}

func (r sourceReadRecord) observed(ctx context.Context, observation application.Observation) error {
	_, err := r.journal.Observe(ctx, application.ObserveRecord{ChangeRecord: application.ChangeRecord{Key: r.step("observe"), ExpectedRevision: 2}, Observation: observation})
	return err
}
