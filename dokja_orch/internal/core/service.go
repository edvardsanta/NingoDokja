package core

import (
	"context"
	"errors"
	"fmt"
	"read_books/internal/logger"
	"time"
)

type ProcessResult struct {
	Event    Event          `json:"event"`
	Workflow string         `json:"workflow"`
	Domains  []string       `json:"domains"`
	Result   map[string]any `json:"result"`
}

type Service struct {
	router         EventRouter
	contextBuilder ContextBuilder
	workflowEngine WorkflowEngine
	dispatcher     DomainDispatcher
	controls       *Controls
}

// WithControls makes the service honour the operator's service and job switches.
func (s *Service) WithControls(controls *Controls) *Service {
	s.controls = controls
	return s
}

func scheduleOf(event Event) string {
	schedule, _ := event.Context["schedule"].(string)
	return schedule
}

// skipped answers an event the operator switched off. It is a normal result, not an
// error: a paused job would otherwise log a failure on every tick.
func (s *Service) skipped(event Event, route Route) (ProcessResult, bool) {
	if s.controls == nil {
		return ProcessResult{}, false
	}
	schedule := scheduleOf(event)
	reason := ""
	if service := ServiceForEvent(event.Type); service != "" && !s.controls.ServiceEnabled(service) {
		reason = fmt.Sprintf("service %s is disabled", service)
	} else if schedule != "" && !s.controls.ServiceEnabled("scheduler") {
		reason = "scheduler is paused"
	} else if schedule != "" && !s.controls.JobEnabled(schedule) {
		reason = fmt.Sprintf("job %s is paused", schedule)
	}
	if reason == "" {
		return ProcessResult{}, false
	}

	logger.Info(fmt.Sprintf("orchestrator skipped event_id=%s type=%s reason=%q", event.EventID, event.Type, reason))
	s.controls.RecordRun(schedule, "skipped", errors.New(reason), time.Now().UTC())
	return ProcessResult{
		Event:    event,
		Workflow: route.Workflow,
		Domains:  domainsToStrings(route.Domains),
		Result:   map[string]any{"skipped": true, "reason": reason},
	}, true
}

func (s *Service) recordRun(event Event, cause error) {
	if s.controls == nil || scheduleOf(event) == "" {
		return
	}
	outcome := "ran"
	if cause != nil {
		outcome = "error"
	}
	s.controls.RecordRun(scheduleOf(event), outcome, cause, time.Now().UTC())
}

func NewService(
	router EventRouter,
	contextBuilder ContextBuilder,
	workflowEngine WorkflowEngine,
	dispatcher DomainDispatcher,
) *Service {
	return &Service{
		router:         router,
		contextBuilder: contextBuilder,
		workflowEngine: workflowEngine,
		dispatcher:     dispatcher,
	}
}

func DefaultService(handlers ...DomainHandler) *Service {
	return NewService(
		NewRuleBasedEventRouter(),
		NewDefaultContextBuilder(),
		NewDefaultWorkflowEngine(),
		NewDomainDispatcher(handlers...),
	)
}

func (s *Service) Process(ctx context.Context, event Event) error {
	_, err := s.ProcessWithResult(ctx, event)
	return err
}

func (s *Service) ProcessWithResult(ctx context.Context, event Event) (ProcessResult, error) {
	if s == nil {
		return ProcessResult{}, fmt.Errorf("orchestrator service is nil")
	}
	if s.router == nil {
		return ProcessResult{}, fmt.Errorf("event router is not configured")
	}
	if s.contextBuilder == nil {
		return ProcessResult{}, fmt.Errorf("context builder is not configured")
	}
	if s.workflowEngine == nil {
		return ProcessResult{}, fmt.Errorf("workflow engine is not configured")
	}
	if s.dispatcher == nil {
		return ProcessResult{}, fmt.Errorf("domain dispatcher is not configured")
	}

	if err := event.Normalize(); err != nil {
		return ProcessResult{}, err
	}

	route, err := s.router.Route(ctx, event)
	if err != nil {
		return ProcessResult{}, fmt.Errorf("route event: %w", err)
	}
	logger.Info(fmt.Sprintf(
		"orchestrator routed event_id=%s type=%s workflow=%s domains=%v",
		event.EventID,
		event.Type,
		route.Workflow,
		domainsToStrings(route.Domains),
	))

	if result, skip := s.skipped(event, route); skip {
		return result, nil
	}

	event.Context, err = s.contextBuilder.Build(ctx, event, route)
	if err != nil {
		return ProcessResult{}, fmt.Errorf("build context: %w", err)
	}

	workflow, err := s.workflowEngine.Plan(ctx, event, route)
	if err != nil {
		return ProcessResult{}, fmt.Errorf("plan workflow: %w", err)
	}
	logger.Info(fmt.Sprintf(
		"orchestrator planned event_id=%s workflow=%s steps=%d",
		event.EventID,
		workflow.Name,
		len(workflow.Steps),
	))

	dispatchResult, err := s.dispatcher.Dispatch(ctx, event, workflow)
	s.recordRun(event, err)
	if err != nil {
		return ProcessResult{}, fmt.Errorf("dispatch workflow: %w", err)
	}
	logger.Info(fmt.Sprintf(
		"orchestrator dispatched event_id=%s workflow=%s result_domains=%d",
		event.EventID,
		workflow.Name,
		len(dispatchResult),
	))

	return ProcessResult{
		Event:    event,
		Workflow: route.Workflow,
		Domains:  domainsToStrings(route.Domains),
		Result:   dispatchResult,
	}, nil
}

func domainsToStrings(domains []Domain) []string {
	values := make([]string, 0, len(domains))
	for _, domain := range domains {
		values = append(values, string(domain))
	}
	return values
}
