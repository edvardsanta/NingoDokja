package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

const (
	// Overrides outside this range are ignored, whatever wrote them.
	minOverrideInterval = time.Minute
	maxOverrideInterval = 30 * 24 * time.Hour

	defaultStatePoll        = 5 * time.Second
	announceEveryStatePolls = 12 // re-announce about once a minute, so a restarted orchestrator catches up
	announceEventType       = "scheduler.jobs.announce"
)

type job struct {
	name            string
	defaultInterval time.Duration
	run             func(ctx context.Context) error
}

// jobs lists what the scheduler runs. The names are what the orchestrator switches on.
func (s *Service) jobs() []job {
	return []job{
		{"meme.refresh", s.refreshInterval, s.runMemeRefresh},
		{"meme.dispatch", s.dispatchInterval, s.runMemeDispatch},
	}
}

func (s *Service) JobNames() func(func(string) bool) {
	return func(yield func(string) bool) {
		for _, j := range s.jobs() {
			if !yield(j.name) {
				return
			}
		}
	}
}

// loadOverrides reads per-job intervals the operator set through the orchestrator,
// which owns the shared state file. Anything unusable is ignored so a bad file can
// never stop the scheduler or produce a runaway schedule.
func (s *Service) loadOverrides() (map[string]time.Duration, bool) {
	overrides := map[string]time.Duration{}
	if s.stateFile == "" {
		return overrides, true
	}
	raw, err := os.ReadFile(s.stateFile)
	if err != nil {
		return overrides, os.IsNotExist(err)
	}
	var state struct {
		Jobs map[string]struct {
			Interval string `json:"interval"`
		} `json:"jobs"`
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		s.logger.Printf("state file %s is unreadable, ignoring interval overrides: %v", s.stateFile, err)
		return overrides, false
	}
	for name, entry := range state.Jobs {
		if entry.Interval == "" {
			continue
		}
		interval, err := time.ParseDuration(entry.Interval)
		if err != nil || interval < minOverrideInterval || interval > maxOverrideInterval {
			s.logger.Printf("ignoring interval %q for job %s (need %s to %s)", entry.Interval, name, minOverrideInterval, maxOverrideInterval)
			continue
		}
		overrides[name] = interval
	}
	return overrides, true
}

// plan tracks when each job last ran and is due next.
type plan struct {
	jobs      []job
	overrides map[string]time.Duration
	last      map[string]time.Time
	next      map[string]time.Time
}

func newPlan(jobs []job, now time.Time) *plan {
	p := &plan{jobs: jobs, overrides: map[string]time.Duration{}, last: map[string]time.Time{}, next: map[string]time.Time{}}
	for _, j := range jobs {
		p.next[j.name] = now.Add(j.defaultInterval)
	}
	return p
}

func (p *plan) interval(j job) time.Duration {
	if override, ok := p.overrides[j.name]; ok {
		return override
	}
	return j.defaultInterval
}

// ran records a run: the countdown restarts from now.
func (p *plan) ran(j job, now time.Time) {
	p.last[j.name] = now
	p.next[j.name] = now.Add(p.interval(j))
}

// applyOverrides adopts new intervals; a job whose interval changed restarts its
// countdown, so lowering an interval never fires a job the moment it is saved.
func (p *plan) applyOverrides(overrides map[string]time.Duration, now time.Time) bool {
	changed := false
	for _, j := range p.jobs {
		before := p.interval(j)
		if override, ok := overrides[j.name]; ok {
			p.overrides[j.name] = override
		} else {
			delete(p.overrides, j.name)
		}
		if p.interval(j) != before {
			p.next[j.name] = now.Add(p.interval(j))
			changed = true
		}
	}
	return changed
}

func (p *plan) earliest() time.Time {
	var first time.Time
	for _, j := range p.jobs {
		if at := p.next[j.name]; first.IsZero() || at.Before(first) {
			first = at
		}
	}
	return first
}

func (p *plan) due(now time.Time) []job {
	var due []job
	for _, j := range p.jobs {
		if !p.next[j.name].After(now) {
			due = append(due, j)
		}
	}
	return due
}

func (p *plan) announcePayload() map[string]any {
	entries := make([]map[string]any, 0, len(p.jobs))
	for _, j := range p.jobs {
		entry := map[string]any{
			"name":      j.name,
			"interval":  p.interval(j).String(),
			"next_fire": p.next[j.name].UTC().Format(time.RFC3339),
		}
		if last := p.last[j.name]; !last.IsZero() {
			entry["last_fire"] = last.UTC().Format(time.RFC3339)
		}
		entries = append(entries, entry)
	}
	return map[string]any{"jobs": entries}
}

// announce tells the orchestrator how the jobs are scheduled. It is not a job run,
// so it carries no "schedule" stamp.
func (s *Service) announce(ctx context.Context, p *plan) {
	event := newEvent(announceEventType, p.announcePayload(), map[string]any{"interface": "scheduler"})
	if err := s.emit(ctx, event); err != nil {
		s.logger.Printf("announce failed: %v", err)
	}
}

func (s *Service) runJob(ctx context.Context, p *plan, j job) {
	if err := j.run(ctx); err != nil {
		s.logger.Printf("scheduled %s failed: %v", j.name, err)
	}
	p.ran(j, s.now())
}

func (s *Service) Run(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("scheduler service is nil")
	}

	s.logger.Printf(
		"config refresh_interval=%s dispatch_interval=%s bootstrap_grace=%s refresh_max_items=%d dispatch_limit=%d event_endpoint=%s topic=%s state_file=%q",
		s.refreshInterval,
		s.dispatchInterval,
		s.bootstrapGrace,
		s.refreshMaxItems,
		s.dispatchMemeLimit,
		s.eventEndpoint,
		s.topic,
		s.stateFile,
	)

	jobs := s.jobs()
	p := newPlan(jobs, s.now())
	if overrides, ok := s.loadOverrides(); ok {
		p.applyOverrides(overrides, s.now())
	}
	s.announce(ctx, p)

	// Bootstrap: the meme refresh first, then, after the grace period, every job once.
	// The orchestrator skips the ones an operator paused, so a restart never posts
	// something that was switched off.
	s.runJob(ctx, p, jobs[0])
	if err := s.waitBootstrapGrace(ctx); err != nil {
		return err
	}
	for _, j := range jobs[1:] {
		s.runJob(ctx, p, j)
	}
	s.announce(ctx, p)

	poll := time.NewTicker(s.statePoll)
	defer poll.Stop()
	polls := 0

	for {
		wait := p.earliest().Sub(s.now())
		if wait < 0 {
			wait = 0
		}
		timer := time.NewTimer(wait)

		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-poll.C:
			timer.Stop()
			polls++
			changed := false
			if overrides, ok := s.loadOverrides(); ok {
				changed = p.applyOverrides(overrides, s.now())
			}
			if changed || polls%announceEveryStatePolls == 0 {
				s.announce(ctx, p)
			}
		case <-timer.C:
			for _, j := range p.due(s.now()) {
				s.runJob(ctx, p, j)
			}
			s.announce(ctx, p)
		}
	}
}
