package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	knowledge "read_books/dokja_domain/dokja_knowledge"
	application "read_books/dokja_domain/dokja_memory/application"
	"read_books/internal/core"
	"read_books/internal/sourceaccess"
)

type desktopApplication struct {
	owner, operation, digest string
	host                     *SourceSupervisor
	created                  time.Time
}

// DesktopSourceApplications is a quota-limited host for the source-only native
// coordinator. Records are volatile; original receipts use the supplied journal.
type DesktopSourceApplications struct {
	instance     string
	mu           sync.Mutex
	handler      *KnowledgeDomainHandler
	store        application.JournalStore
	lifetime     context.Context
	enabled      func() bool
	applications map[string]desktopApplication
}

func NewDesktopSourceApplications(lifetime context.Context, handler *KnowledgeDomainHandler, store application.JournalStore, enabled func() bool) *DesktopSourceApplications {
	return &DesktopSourceApplications{instance: uuid.NewString(), handler: handler, store: store, lifetime: lifetime, enabled: enabled, applications: map[string]desktopApplication{}}
}
func (d *DesktopSourceApplications) Handle(ctx context.Context, event core.Event) (map[string]any, error) {
	principal, ok := sourceaccess.Current(ctx)
	if !ok {
		return nil, ErrSourceControlDenied
	}
	if d.store == nil || d.handler == nil {
		return nil, fmt.Errorf("source supervision is not configured")
	}
	switch event.Type {
	case "knowledge.application.supervision.start":
		return d.start(ctx, event, principal)
	case "knowledge.application.supervision.preview", "knowledge.application.supervision.status", "knowledge.application.supervision.control":
		if event.Type == "knowledge.application.supervision.status" && len(event.Payload) == 0 {
			d.mu.Lock()
			remaining := 16 - len(d.applications)
			type recent struct {
				ref   string
				entry desktopApplication
			}
			entries := make([]recent, 0, len(d.applications))
			for ref, entry := range d.applications {
				if entry.owner == principal.Actor {
					entries = append(entries, recent{ref, entry})
				}
			}
			sort.Slice(entries, func(i, j int) bool { return entries[i].entry.created.After(entries[j].entry.created) })
			applications := make([]any, 0, len(entries))
			for _, entry := range entries {
				applications = append(applications, map[string]any{"application_ref": entry.ref, "operation_ref": entry.entry.operation})
			}
			d.mu.Unlock()
			return map[string]any{"available": true, "host_ref": d.instance, "remaining": remaining, "applications": applications}, nil
		}
		ref, _ := event.Payload["application_ref"].(string)
		if !sourceReadIdentity.MatchString(ref) {
			return nil, fmt.Errorf("invalid application reference")
		}
		d.mu.Lock()
		entry, exists := d.applications[ref]
		d.mu.Unlock()
		if !exists || entry.owner != principal.Actor {
			return nil, fmt.Errorf("source application is unavailable; its process may have restarted")
		}
		if event.Type == "knowledge.application.supervision.preview" {
			raw, _ := json.Marshal(event.Payload)
			var input struct {
				ApplicationRef   string         `json:"application_ref"`
				ExpectedRevision uint64         `json:"expected_revision"`
				Application      map[string]any `json:"application"`
			}
			decoder := json.NewDecoder(strings.NewReader(string(raw)))
			decoder.DisallowUnknownFields()
			if len(raw) > 12*1024 || decoder.Decode(&input) != nil || len(input.Application) == 0 {
				return nil, fmt.Errorf("invalid candidate preview")
			}
			return entry.host.Preview(ctx, input.ExpectedRevision, input.Application)
		}
		if event.Type == "knowledge.application.supervision.control" {
			raw, _ := json.Marshal(event.Payload)
			var input struct {
				ApplicationRef   string                        `json:"application_ref"`
				OperationRef     string                        `json:"operation_ref"`
				ExpectedRevision uint64                        `json:"expected_revision"`
				Action           string                        `json:"action"`
				Reason           string                        `json:"reason"`
				Application      *knowledge.ApplicationRequest `json:"application"`
			}
			decoder := json.NewDecoder(strings.NewReader(string(raw)))
			decoder.DisallowUnknownFields()
			if len(raw) > 12*1024 || decoder.Decode(&input) != nil {
				return nil, fmt.Errorf("invalid source control")
			}
			if _, err := entry.host.Control(ctx, knowledge.SourceControl{OperationRef: input.OperationRef, ExpectedRevision: input.ExpectedRevision, Action: input.Action, Reason: input.Reason, Application: input.Application}); err != nil {
				return nil, err
			}
		} else if len(event.Payload) != 1 {
			return nil, fmt.Errorf("invalid status request")
		}
		snapshot, err := entry.host.Status(ctx)
		if err != nil {
			return nil, err
		}
		return desktopSourceSnapshot(snapshot), nil
	default:
		return nil, fmt.Errorf("unsupported source supervision action")
	}
}
func (d *DesktopSourceApplications) start(ctx context.Context, event core.Event, p sourceaccess.Principal) (map[string]any, error) {
	if d.enabled != nil && !d.enabled() {
		return nil, fmt.Errorf("Knowledge is switched off")
	}
	operation, _ := event.Payload["operation_ref"].(string)
	instance, _ := event.Payload["host_ref"].(string)
	if instance != d.instance {
		return nil, fmt.Errorf("source host changed; an old start cannot be replayed after restart")
	}
	body, ok := event.Payload["application"].(map[string]any)
	if !ok || len(event.Payload) != 3 || !sourceReadIdentity.MatchString(operation) {
		return nil, fmt.Errorf("invalid source application start")
	}
	request, err := knowledge.ParseApplicationRequest(body)
	if err != nil {
		return nil, err
	}
	if _, err = knowledge.ValidateSupervisedApplication(request); err != nil {
		return nil, err
	}
	digest := sourceReadDigest(request)
	// Serialize admission and start replay so a lost reply never creates another run.
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, entry := range d.applications {
		if entry.owner == p.Actor && entry.operation == operation && entry.digest == digest {
			snapshot, err := entry.host.Status(ctx)
			if err != nil {
				return nil, err
			}
			return desktopSourceSnapshot(snapshot), nil
		}
		if entry.owner == p.Actor && entry.operation == operation {
			return nil, ErrSourceControlConflict
		}
	}
	// Retain records for this process so expired operation keys cannot resurrect a
	// lost run. Admission stops at the quota instead of silently dropping fences.
	if len(d.applications) >= 16 {
		return nil, fmt.Errorf("source application quota reached for this process")
	}
	owner := func(ctx context.Context) bool { actual, ok := sourceaccess.Current(ctx); return ok && actual == p }
	host, err := NewSourceSupervisor(d.lifetime, d.handler, SourceSupervisorOptions{Scope: p.Scope, Store: d.store, Clock: time.Now,
		Authorize: func(ctx context.Context, permission SourceSupervisorPermission) (SourceSupervisorActor, error) {
			if !owner(ctx) || permission.Scope != p.Scope {
				return SourceSupervisorActor{}, ErrSourceControlDenied
			}
			return SourceSupervisorActor{Ref: p.Actor, DecisionRef: "desktop:credential"}, nil
		},
		AuthorizeSource: func(ctx context.Context, permission SourceReadPermission) (string, error) {
			if !owner(ctx) || permission.Scope != p.Scope {
				return "", ErrSourceControlDenied
			}
			if permission.Purpose == "execute" && d.enabled != nil && !d.enabled() {
				return "", fmt.Errorf("Knowledge is switched off")
			}
			return "desktop:source-read", nil
		},
	})
	if err != nil {
		return nil, err
	}
	startEvent := core.Event{Source: core.SourceDesktop, Type: "knowledge.application.prepare", User: core.EventUser{ID: p.Actor}, Channel: core.EventChannel{ID: p.Scope}, Payload: body}
	snapshot, err := host.Start(ctx, startEvent)
	if err != nil {
		return nil, err
	}
	d.applications[snapshot.ApplicationRef] = desktopApplication{owner: p.Actor, operation: operation, digest: digest, host: host, created: time.Now()}
	return desktopSourceSnapshot(snapshot), nil
}
func desktopSourceSnapshot(s SourceSupervisorSnapshot) map[string]any {
	controls := make([]any, 0, len(s.Controls))
	for _, c := range s.Controls {
		controls = append(controls, map[string]any{"operation_ref": c.OperationRef, "actor": c.ActorRef, "action": c.Action, "reason": c.Reason, "control_revision": c.ControlRevision, "plan_revision": c.PlanRevision})
	}
	receipts := make([]any, 0, len(s.Settlements))
	for _, r := range s.Settlements {
		receipts = append(receipts, map[string]any{"attempt_ref": r.AttemptRef, "source_id": r.SourceID, "state_revision": r.StateRevision, "plan_revision": r.PlanRevision, "outcome": r.Outcome, "confirmed": r.Confirmed})
	}
	return map[string]any{"application_ref": s.ApplicationRef, "state": s.State, "reason": s.Reason, "sequence": s.Sequence, "control_revision": s.ControlRevision, "plan_revision": s.PlanRevision, "read_attempts": s.ReadAttempts, "pending": s.Pending, "uncertain": s.Uncertain, "deadline": s.Deadline, "application": s.Request, "controls": controls, "receipts": receipts}
}
