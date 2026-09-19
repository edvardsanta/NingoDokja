package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// Job bounds mirror the orchestrator's; it enforces them again, this only fails early.
const (
	minJobInterval = time.Minute
	maxJobInterval = 30 * 24 * time.Hour
)

// JobNames are the scheduler jobs, as the orchestrator knows them.
var JobNames = []string{
	"meme.refresh",
	"meme.dispatch",
}

// ServiceNames are the services that can be switched off.
// "scheduler" pauses every scheduled run at once (the container itself is not touched).
var ServiceNames = []string{"meme", "chat_ai", "book", "scheduler"}

// JobEvent is the event that runs a job right now. Manual runs carry no schedule
// stamp, so a paused job does not block them; a disabled service still does.
func JobEvent(name string, memeLimit int) (string, map[string]any, error) {
	switch name {
	case "meme.refresh":
		return "meme.pool.refresh", map[string]any{"max_items_per_scraper": 20}, nil
	case "meme.dispatch":
		payload, err := buildDispatchPayload(memeLimit)
		return "meme.dispatch.scheduled", payload, err
	}
	return "", nil, fmt.Errorf("unknown job %q (known: %s)", name, strings.Join(JobNames, ", "))
}

func checkName(kind, name string, known []string) error {
	for _, candidate := range known {
		if candidate == name {
			return nil
		}
	}
	return fmt.Errorf("unknown %s %q (known: %s)", kind, name, strings.Join(known, ", "))
}

func buildServicePayload(name string, enabled bool) (map[string]any, error) {
	if err := checkName("service", name, ServiceNames); err != nil {
		return nil, err
	}
	return map[string]any{"name": name, "enabled": enabled}, nil
}

func buildJobTogglePayload(name string, enabled bool) (map[string]any, error) {
	if err := checkName("job", name, JobNames); err != nil {
		return nil, err
	}
	return map[string]any{"name": name, "enabled": enabled}, nil
}

// BuildJobIntervalPayload accepts a Go duration ("45m", "6h") or "default" to drop the override.
func BuildJobIntervalPayload(name, interval string) (map[string]any, error) {
	if err := checkName("job", name, JobNames); err != nil {
		return nil, err
	}
	interval = strings.TrimSpace(interval)
	if interval == "default" {
		return map[string]any{"name": name, "interval": "default"}, nil
	}
	parsed, err := time.ParseDuration(interval)
	if err != nil {
		return nil, fmt.Errorf("interval %q is not a duration like 45m or 6h (or \"default\")", interval)
	}
	if parsed < minJobInterval || parsed > maxJobInterval {
		return nil, fmt.Errorf("interval must be between %s and %s, got %s", minJobInterval, maxJobInterval, parsed)
	}
	return map[string]any{"name": name, "interval": parsed.String()}, nil
}

// domainField digs a field out of the compact orchestrator response
// {"result": {"<domain>": {...}}} (or {"result": {...}} when there are several domains).
func domainField(result any, key string) (any, error) {
	envelope, ok := result.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unexpected orchestrator response %T", result)
	}
	inner, ok := envelope["result"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("orchestrator response has no result")
	}
	if value, present := inner[key]; present {
		return value, nil
	}
	for _, candidate := range inner {
		if domain, ok := candidate.(map[string]any); ok {
			if value, present := domain[key]; present {
				return value, nil
			}
		}
	}
	return nil, fmt.Errorf("orchestrator response has no %q", key)
}

func (a *App) baseEvent(eventType string, payload map[string]any) EmitOptions {
	return EmitOptions{
		Type:      eventType,
		UserID:    a.emitOpts.userID,
		UserName:  a.emitOpts.userName,
		ChannelID: a.emitOpts.channelID,
		Payload:   payload,
		Context:   map[string]any{"interface": "cli"},
		Source:    "cli",
	}
}

func (a *App) printStatusPart(ctx context.Context, key string) error {
	response, err := a.call(ctx, a.baseEvent("ningo.status", map[string]any{}))
	if err != nil {
		return err
	}
	value, err := domainField(response.Result, key)
	if err != nil {
		return err
	}
	return printJSON(value)
}

func (a *App) newServicesCommand(ctx context.Context) *cobra.Command {
	command := &cobra.Command{
		Use:   "services",
		Short: "Switch services on or off in the orchestrator",
		Long: "A switch is logical: while a service is off the orchestrator refuses its events " +
			"(scheduled jobs included) and reports why. The container keeps running, and the setting survives restarts.",
	}

	list := &cobra.Command{
		Use:   "list",
		Short: "Show every service with its state (ningo.status)",
		RunE:  func(cmd *cobra.Command, args []string) error { return a.printStatusPart(ctx, "services") },
	}

	toggle := func(use, short string, enabled bool) *cobra.Command {
		return &cobra.Command{
			Use:       use + " <service>",
			Short:     short,
			Args:      cobra.ExactArgs(1),
			ValidArgs: ServiceNames,
			RunE: func(cmd *cobra.Command, args []string) error {
				payload, err := buildServicePayload(args[0], enabled)
				if err != nil {
					return err
				}
				return a.request(ctx, a.baseEvent("services.set", payload))
			},
		}
	}

	command.AddCommand(list, toggle("enable", "Switch a service on (services.set)", true), toggle("disable", "Switch a service off (services.set)", false))
	return command
}

func (a *App) newJobsCommand(ctx context.Context) *cobra.Command {
	command := &cobra.Command{
		Use:   "jobs",
		Short: "Pause, resume, retime or run scheduler jobs",
		Long: "Pausing a job makes the orchestrator skip its scheduled runs (the scheduler keeps ticking). " +
			"Changes survive restarts, so a paused job stays paused when the scheduler boots.",
	}

	list := &cobra.Command{
		Use:   "list",
		Short: "Show every job: state, interval, last and next run (ningo.status)",
		RunE:  func(cmd *cobra.Command, args []string) error { return a.printStatusPart(ctx, "jobs") },
	}

	toggle := func(use, short string, enabled bool) *cobra.Command {
		return &cobra.Command{
			Use:       use + " <job>",
			Short:     short,
			Args:      cobra.ExactArgs(1),
			ValidArgs: JobNames,
			RunE: func(cmd *cobra.Command, args []string) error {
				payload, err := buildJobTogglePayload(args[0], enabled)
				if err != nil {
					return err
				}
				return a.request(ctx, a.baseEvent("scheduler.jobs.set", payload))
			},
		}
	}

	interval := &cobra.Command{
		Use:       "interval <job> <duration|default>",
		Short:     "Change how often a job runs, e.g. 45m or 6h (scheduler.jobs.set)",
		Args:      cobra.ExactArgs(2),
		ValidArgs: JobNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			payload, err := BuildJobIntervalPayload(args[0], args[1])
			if err != nil {
				return err
			}
			return a.request(ctx, a.baseEvent("scheduler.jobs.set", payload))
		},
	}

	var runLimit int
	run := &cobra.Command{
		Use:       "run <job>",
		Short:     "Run a job right now, even if it is paused",
		Long:      "Sends the job's event immediately. A paused job does not block a manual run; a disabled service does.",
		Args:      cobra.ExactArgs(1),
		ValidArgs: JobNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			eventType, payload, err := JobEvent(args[0], runLimit)
			if err != nil {
				return err
			}
			return a.request(ctx, a.baseEvent(eventType, payload))
		},
	}
	run.Flags().IntVar(&runLimit, "limit", 1, "For meme.dispatch: how many memes to send (at least 1)")

	command.AddCommand(list,
		toggle("enable", "Resume a paused job (scheduler.jobs.set)", true),
		toggle("disable", "Pause a job (scheduler.jobs.set)", false),
		interval, run)
	return command
}
