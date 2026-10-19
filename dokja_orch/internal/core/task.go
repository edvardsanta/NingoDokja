package core

import (
	"context"
	"time"
)

// The task workspace of ADR 0001: one execution attempt of a typed workflow. The
// orchestrator is its only writer; steps get copies and return typed results.

type TaskStatus string

const (
	TaskCreated   TaskStatus = "created"
	TaskRunning   TaskStatus = "running"
	TaskCompleted TaskStatus = "completed"
	TaskStopped   TaskStatus = "stopped"
	TaskFailed    TaskStatus = "failed"
	TaskCancelled TaskStatus = "cancelled"
)

type StepDisposition string

const (
	StepContinue StepDisposition = "continue"
	StepStop     StepDisposition = "stop"
	StepAbstain  StepDisposition = "abstain"
	StepFail     StepDisposition = "fail"
)

// Stable reason codes the runner itself produces.
const (
	ReasonDeadlineExceeded = "deadline_exceeded"
	ReasonCancelled        = "cancelled"
	ReasonStepLimit        = "step_limit"
	ReasonEvidenceLimit    = "evidence_limit"
	ReasonProposalLimit    = "proposal_limit"
	ReasonSizeLimit        = "size_limit"
	ReasonStepError        = "step_error"
	ReasonInvalidResult    = "invalid_step_result"
	ReasonAbstained        = "abstained"
	// ReasonOutcomeUnknown marks a step that was still running when the task ended. A
	// step that ignores cancellation can still finish its side effect afterwards, so its
	// outcome is unconfirmed, not failed.
	ReasonOutcomeUnknown = "outcome_unknown"
)

// TaskScope is copied from the ingress path. It is not an authorization grant.
type TaskScope struct {
	Source     EventSource `json:"source"`
	ActorID    string      `json:"actor_id"`
	ChannelID  string      `json:"channel_id"`
	SessionRef string      `json:"session_ref,omitempty"`
}

// Evidence refers to content held elsewhere; the reference grants no access to it. A zero
// Scope means the producing store has no scope model (Knowledge and Memory today): the
// evidence is kept and counted as unscoped, never stamped with the task's scope.
type Evidence struct {
	Ref        string    `json:"ref"`
	Capability string    `json:"capability"`
	Version    string    `json:"version,omitempty"`
	ObservedAt time.Time `json:"observed_at"`
	Scope      TaskScope `json:"scope"`
	Reason     string    `json:"reason,omitempty"`
}

type Proposal struct {
	ActionID      string   `json:"action_id"`
	Domain        Domain   `json:"domain"`
	Decision      string   `json:"decision"`
	EvidenceRefs  []string `json:"evidence_refs,omitempty"`
	Preconditions []string `json:"preconditions,omitempty"`
	Reason        string   `json:"reason,omitempty"`
}

// Receipt records a side effect a step performed for an action attempt.
type Receipt struct {
	ActionID string    `json:"action_id"`
	Step     string    `json:"step"`
	Status   string    `json:"status"`
	At       time.Time `json:"at"`
}

type StepRecord struct {
	Name        string          `json:"name"`
	Domain      Domain          `json:"domain"`
	Action      string          `json:"action"`
	Disposition StepDisposition `json:"disposition"`
	Reason      string          `json:"reason,omitempty"`
	Degraded    bool            `json:"degraded,omitempty"`
	Dropped     int             `json:"dropped_evidence,omitempty"`
	Unscoped    int             `json:"unscoped_evidence,omitempty"`
	Duration    time.Duration   `json:"duration"`
}

// StepInput is a copy of what a step may read.
type StepInput struct {
	TaskID    string
	Goal      string
	Scope     TaskScope
	Payload   map[string]any
	Evidence  []Evidence
	Proposals []Proposal
	Outputs   map[string]map[string]any
}

type StepResult struct {
	Disposition StepDisposition
	Reason      string
	Evidence    []Evidence
	Proposals   []Proposal
	Receipts    []Receipt
	Output      map[string]any
}

type TaskStepFunc func(ctx context.Context, input StepInput) (StepResult, error)

type TaskStep struct {
	Name   string
	Domain Domain
	Action string
	// Optional is declared by the workflow: a failure is recorded as degraded and the
	// task continues. A step cannot make its own failure optional.
	Optional bool
	// ContinueOnAbstain lets the workflow fall through to a later step.
	ContinueOnAbstain bool
	Run               TaskStepFunc
}

type TypedWorkflow struct {
	Name     string
	Version  string
	Goal     string
	Deadline time.Duration
	Steps    []TaskStep
}

// TaskLimits are the provisional prototype bounds of ADR 0001. Every field must be
// positive; there is no implicit default.
type TaskLimits struct {
	MaxSteps     int
	MaxEvidence  int
	MaxProposals int
	MaxBytes     int
}

func PrototypeTaskLimits() TaskLimits {
	return TaskLimits{MaxSteps: 16, MaxEvidence: 32, MaxProposals: 8, MaxBytes: 64 * 1024}
}

// TaskResult is the bounded summary returned when a task ends. The workspace itself is
// released.
type TaskResult struct {
	TaskID    string                    `json:"task_id"`
	AttemptID string                    `json:"attempt_id"`
	EventID   string                    `json:"event_id"`
	Workflow  string                    `json:"workflow"`
	Version   string                    `json:"version"`
	Scope     TaskScope                 `json:"scope"`
	Deadline  time.Time                 `json:"deadline"`
	Status    TaskStatus                `json:"status"`
	Reason    string                    `json:"reason,omitempty"`
	Summary   string                    `json:"summary,omitempty"`
	Steps     []StepRecord              `json:"steps"`
	Evidence  []Evidence                `json:"evidence,omitempty"`
	Proposals []Proposal                `json:"proposals,omitempty"`
	Receipts  []Receipt                 `json:"receipts,omitempty"`
	Outputs   map[string]map[string]any `json:"outputs,omitempty"`
}
