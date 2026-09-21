package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// KnownServices are the only services that can be switched off. Anything else is
// rejected, so an unauthenticated caller cannot invent keys.
// "scheduler" is special: switching it off pauses every scheduled run at once.
var KnownServices = []string{"meme", "chat_ai", "book", "scheduler"}

// KnownJobs are the scheduler jobs, named as the scheduler stamps them in Context["schedule"].
var KnownJobs = []string{
	"meme.refresh",
	"meme.dispatch",
}

const (
	MinJobInterval = time.Minute
	MaxJobInterval = 30 * 24 * time.Hour
)

// ServiceForEvent names the service an event needs, or "" when it needs none of the
// switchable ones (admin and status events are always allowed through).
func ServiceForEvent(eventType string) string {
	switch {
	case strings.HasPrefix(eventType, "meme."):
		return "meme"
	case strings.HasPrefix(eventType, "book."):
		return "book"
	case strings.HasPrefix(eventType, "message."):
		return "chat_ai"
	}
	return ""
}

type persistedJob struct {
	Enabled  *bool  `json:"enabled,omitempty"`
	Interval string `json:"interval,omitempty"`
}

type persistedState struct {
	Services map[string]bool         `json:"services,omitempty"` // only services that are OFF
	Jobs     map[string]persistedJob `json:"jobs,omitempty"`
}

// JobAnnounce is what the scheduler reports about one of its jobs.
type JobAnnounce struct {
	Name     string
	Interval time.Duration
	LastFire time.Time
	NextFire time.Time
}

type jobRuntime struct {
	announce    JobAnnounce
	announcedAt time.Time
	lastAt      time.Time
	lastOutcome string
	lastError   string
}

// ServiceInfo and JobInfo are the read model shown by status commands and the TUI.
type ServiceInfo struct {
	Name    string
	Enabled bool
}

type JobInfo struct {
	Name             string
	Enabled          bool
	Interval         time.Duration
	IntervalOverride bool
	NextAt           time.Time
	LastAt           time.Time
	LastOutcome      string
	LastError        string
	AnnouncedAt      time.Time
}

// Controls holds the switches the operator can flip at runtime. It is read from the
// ZeroMQ, request and HTTP ingresses at once, so every access goes through the mutex.
type Controls struct {
	mu       sync.RWMutex
	path     string
	state    persistedState
	runtime  map[string]*jobRuntime
	loadNote string
}

// NewControls loads the state file (missing means defaults). A corrupt file starts
// with every job paused: never resume posting because a file could not be read.
func NewControls(path string) (*Controls, error) {
	c := &Controls{path: path, runtime: map[string]*jobRuntime{}, state: persistedState{
		Services: map[string]bool{}, Jobs: map[string]persistedJob{},
	}}
	if path == "" {
		return c, nil
	}
	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return c, nil
	case err != nil:
		return nil, fmt.Errorf("read controls state: %w", err)
	}

	var loaded persistedState
	if err := json.Unmarshal(raw, &loaded); err != nil {
		off := false
		for _, name := range KnownJobs {
			c.state.Jobs[name] = persistedJob{Enabled: &off}
		}
		c.loadNote = fmt.Sprintf("state file %s is unreadable (%v): every job starts paused", path, err)
		return c, nil
	}
	if loaded.Services != nil {
		c.state.Services = loaded.Services
	}
	if loaded.Jobs != nil {
		c.state.Jobs = loaded.Jobs
	}
	return c, nil
}

// LoadNote is non-empty when the state file could not be read.
func (c *Controls) LoadNote() string { return c.loadNote }

func (c *Controls) ServiceEnabled(name string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return !c.state.Services[name]
}

func (c *Controls) SetService(name string, enabled bool) error {
	if !contains(KnownServices, name) {
		return fmt.Errorf("unknown service %q (known: %s)", name, strings.Join(KnownServices, ", "))
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	previous, had := c.state.Services[name]
	if enabled {
		delete(c.state.Services, name)
	} else {
		c.state.Services[name] = true
	}
	if err := c.saveLocked(); err != nil {
		if had {
			c.state.Services[name] = previous
		} else {
			delete(c.state.Services, name)
		}
		return err
	}
	return nil
}

func (c *Controls) JobEnabled(name string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	job := c.state.Jobs[name]
	return job.Enabled == nil || *job.Enabled
}

// JobPatch changes a job. Nil fields are left alone; ClearInterval drops the override.
type JobPatch struct {
	Enabled       *bool
	Interval      *time.Duration
	ClearInterval bool
}

func (c *Controls) SetJob(name string, patch JobPatch) error {
	if !contains(KnownJobs, name) {
		return fmt.Errorf("unknown job %q (known: %s)", name, strings.Join(KnownJobs, ", "))
	}
	if patch.Interval != nil && (*patch.Interval < MinJobInterval || *patch.Interval > MaxJobInterval) {
		return fmt.Errorf("interval must be between %s and %s, got %s", MinJobInterval, MaxJobInterval, *patch.Interval)
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	previous, had := c.state.Jobs[name]
	job := previous
	if patch.Enabled != nil {
		enabled := *patch.Enabled
		job.Enabled = &enabled
		if enabled {
			job.Enabled = nil // enabled is the default; keep the file minimal
		}
	}
	if patch.ClearInterval {
		job.Interval = ""
	}
	if patch.Interval != nil {
		job.Interval = patch.Interval.String()
	}
	if job.Enabled == nil && job.Interval == "" {
		delete(c.state.Jobs, name)
	} else {
		c.state.Jobs[name] = job
	}
	if err := c.saveLocked(); err != nil {
		if had {
			c.state.Jobs[name] = previous
		} else {
			delete(c.state.Jobs, name)
		}
		return err
	}
	return nil
}

func (c *Controls) intervalOverrideLocked(name string) time.Duration {
	parsed, err := time.ParseDuration(c.state.Jobs[name].Interval)
	if err != nil || parsed < MinJobInterval || parsed > MaxJobInterval {
		return 0
	}
	return parsed
}

func (c *Controls) runtimeLocked(name string) *jobRuntime {
	entry := c.runtime[name]
	if entry == nil {
		entry = &jobRuntime{}
		c.runtime[name] = entry
	}
	return entry
}

// RecordRun notes how the last scheduled run of a job ended (ran, skipped or error).
func (c *Controls) RecordRun(schedule, outcome string, cause error, at time.Time) {
	if !contains(KnownJobs, schedule) {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry := c.runtimeLocked(schedule)
	entry.lastAt, entry.lastOutcome, entry.lastError = at, outcome, ""
	if cause != nil {
		entry.lastError = cause.Error()
	}
}

// Announce stores the scheduler's own view of its jobs (interval and next fire).
func (c *Controls) Announce(jobs []JobAnnounce, at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, job := range jobs {
		if !contains(KnownJobs, job.Name) {
			continue
		}
		entry := c.runtimeLocked(job.Name)
		entry.announce, entry.announcedAt = job, at
	}
}

// LastAnnounce is when the scheduler last reported in (zero if it never has), which is
// the only sign the orchestrator has that the scheduler is running.
func (c *Controls) LastAnnounce() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var newest time.Time
	for _, entry := range c.runtime {
		if entry.announcedAt.After(newest) {
			newest = entry.announcedAt
		}
	}
	return newest
}

func (c *Controls) Services() []ServiceInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]ServiceInfo, 0, len(KnownServices))
	for _, name := range KnownServices {
		out = append(out, ServiceInfo{Name: name, Enabled: !c.state.Services[name]})
	}
	return out
}

func (c *Controls) Jobs() []JobInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]JobInfo, 0, len(KnownJobs))
	for _, name := range KnownJobs {
		job := c.state.Jobs[name]
		info := JobInfo{Name: name, Enabled: job.Enabled == nil || *job.Enabled}
		if override := c.intervalOverrideLocked(name); override > 0 {
			info.Interval, info.IntervalOverride = override, true
		}
		if entry := c.runtime[name]; entry != nil {
			if info.Interval == 0 {
				info.Interval = entry.announce.Interval
			}
			info.NextAt, info.AnnouncedAt = entry.announce.NextFire, entry.announcedAt
			info.LastAt, info.LastOutcome, info.LastError = entry.lastAt, entry.lastOutcome, entry.lastError
			if info.LastAt.IsZero() {
				info.LastAt = entry.announce.LastFire
			}
		}
		out = append(out, info)
	}
	return out
}

// saveLocked writes the state atomically. The caller holds the write lock.
func (c *Controls) saveLocked() error {
	if c.path == "" {
		return nil
	}
	body, err := json.MarshalIndent(c.state, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(c.path), ".state-*")
	if err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	defer os.Remove(temp.Name())
	if _, err := temp.Write(body); err != nil {
		temp.Close()
		return fmt.Errorf("write state: %w", err)
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(temp.Name(), 0o644); err != nil {
		return err
	}
	if err := os.Rename(temp.Name(), c.path); err != nil {
		return fmt.Errorf("save state: %w", err)
	}
	return nil
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
