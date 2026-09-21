package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type countingHandler struct {
	domain Domain
	calls  int
	err    error
}

func (h *countingHandler) Domain() Domain { return h.domain }

func (h *countingHandler) Handle(context.Context, Event, WorkflowStep) (map[string]any, error) {
	h.calls++
	return map[string]any{"handled": true}, h.err
}

func gatedService(t *testing.T) (*Service, *Controls, map[Domain]*countingHandler) {
	t.Helper()
	controls, err := NewControls(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	handlers := map[Domain]*countingHandler{
		DomainMeme:   {domain: DomainMeme},
		DomainSystem: {domain: DomainSystem},
		DomainBook:   {domain: DomainBook},
	}
	service := DefaultService(handlers[DomainMeme], handlers[DomainSystem], handlers[DomainBook]).WithControls(controls)
	return service, controls, handlers
}

func process(t *testing.T, service *Service, eventType, schedule string) ProcessResult {
	t.Helper()
	event := Event{Type: eventType, Source: SourceCLI, Payload: map[string]any{}, Context: map[string]any{}}
	if schedule != "" {
		event.Context["schedule"] = schedule
	}
	result, err := service.ProcessWithResult(context.Background(), event)
	if err != nil {
		t.Fatalf("%s: unexpected error: %v", eventType, err)
	}
	return result
}

func TestDisabledServiceRefusesItsEventsWithoutAnError(t *testing.T) {
	service, controls, handlers := gatedService(t)
	if err := controls.SetService("meme", false); err != nil {
		t.Fatal(err)
	}

	for _, eventType := range []string{"meme.status", "meme.list", "meme.dispatch.scheduled"} {
		result := process(t, service, eventType, "")
		if result.Result["skipped"] != true || !strings.Contains(result.Result["reason"].(string), "meme is disabled") {
			t.Fatalf("%s should be skipped, got %#v", eventType, result.Result)
		}
	}
	if handlers[DomainMeme].calls != 0 || handlers[DomainSystem].calls != 0 {
		t.Fatal("no handler may run for a disabled service")
	}

	if err := controls.SetService("meme", true); err != nil {
		t.Fatal(err)
	}
	if result := process(t, service, "meme.status", ""); result.Result["skipped"] != nil {
		t.Fatalf("re-enabling must let events through, got %#v", result.Result)
	}
}

func TestOtherServicesAndAdminEventsAreNotAffected(t *testing.T) {
	service, controls, handlers := gatedService(t)
	for _, name := range KnownServices {
		if err := controls.SetService(name, false); err != nil {
			t.Fatal(err)
		}
	}

	for _, eventType := range []string{"ningo.status", "discord.send", "services.set", "scheduler.jobs.set", "scheduler.jobs.announce"} {
		if result := process(t, service, eventType, ""); result.Result["skipped"] != nil {
			t.Fatalf("%s must never be gated, got %#v", eventType, result.Result)
		}
	}
	if handlers[DomainSystem].calls != 5 {
		t.Fatalf("expected all admin events to reach the system handler, got %d", handlers[DomainSystem].calls)
	}
}

func TestPausedJobSkipsScheduledRunsButNotManualOnes(t *testing.T) {
	service, controls, handlers := gatedService(t)
	off := false
	if err := controls.SetJob("meme.dispatch", JobPatch{Enabled: &off}); err != nil {
		t.Fatal(err)
	}

	if result := process(t, service, "meme.dispatch.scheduled", "meme.dispatch"); result.Result["skipped"] != true {
		t.Fatalf("the scheduled run must be skipped, got %#v", result.Result)
	}
	if handlers[DomainSystem].calls != 0 {
		t.Fatal("a paused job must not reach the handler")
	}

	// A manual run (CLI/TUI) carries no schedule stamp, so an explicit request still works.
	if result := process(t, service, "meme.dispatch.scheduled", ""); result.Result["skipped"] != nil {
		t.Fatalf("manual runs are not gated by a paused job, got %#v", result.Result)
	}

	// Another job is untouched.
	if result := process(t, service, "meme.pool.refresh", "meme.refresh"); result.Result["skipped"] != nil {
		t.Fatalf("other jobs must keep running, got %#v", result.Result)
	}
}

func TestServiceSwitchStillBlocksAManualRun(t *testing.T) {
	service, controls, _ := gatedService(t)
	_ = controls.SetService("meme", false)
	if result := process(t, service, "meme.dispatch.scheduled", ""); result.Result["skipped"] != true {
		t.Fatalf("a disabled service refuses manual runs too, got %#v", result.Result)
	}
}

func TestScheduledRunOutcomesAreRecorded(t *testing.T) {
	service, controls, handlers := gatedService(t)
	find := func() JobInfo {
		for _, job := range controls.Jobs() {
			if job.Name == "meme.refresh" {
				return job
			}
		}
		t.Fatal("job missing")
		return JobInfo{}
	}

	process(t, service, "meme.pool.refresh", "meme.refresh")
	if job := find(); job.LastOutcome != "ran" || job.LastAt.IsZero() {
		t.Fatalf("expected a ran outcome, got %+v", job)
	}

	handlers[DomainMeme].err = errors.New("scrapers down")
	event := Event{Type: "meme.pool.refresh", Source: SourceCLI, Payload: map[string]any{}, Context: map[string]any{"schedule": "meme.refresh"}}
	if _, err := service.ProcessWithResult(context.Background(), event); err == nil {
		t.Fatal("handler errors must still surface")
	}
	if job := find(); job.LastOutcome != "error" || !strings.Contains(job.LastError, "scrapers down") {
		t.Fatalf("expected an error outcome, got %+v", job)
	}
	handlers[DomainMeme].err = nil

	off := false
	_ = controls.SetJob("meme.refresh", JobPatch{Enabled: &off})
	process(t, service, "meme.pool.refresh", "meme.refresh")
	if job := find(); job.LastOutcome != "skipped" || !strings.Contains(job.LastError, "paused") {
		t.Fatalf("expected a skipped outcome with its reason, got %+v", job)
	}

	process(t, service, "meme.pool.refresh", "")
	if job := find(); job.LastOutcome != "skipped" {
		t.Fatal("unscheduled events must not overwrite the job history")
	}
}

func TestControlsPersistAcrossRestarts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "state.json")
	controls, err := NewControls(path)
	if err != nil {
		t.Fatal(err)
	}
	off, interval := false, 90*time.Minute
	if err := controls.SetService("book", false); err != nil {
		t.Fatal(err)
	}
	if err := controls.SetJob("meme.dispatch", JobPatch{Enabled: &off, Interval: &interval}); err != nil {
		t.Fatal(err)
	}

	reloaded, err := NewControls(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.ServiceEnabled("book") || !reloaded.ServiceEnabled("meme") {
		t.Fatal("service switches must survive a restart")
	}
	if reloaded.JobEnabled("meme.dispatch") || !reloaded.JobEnabled("meme.refresh") {
		t.Fatal("job pauses must survive a restart")
	}
	for _, job := range reloaded.Jobs() {
		if job.Name == "meme.dispatch" && (job.Interval != interval || !job.IntervalOverride) {
			t.Fatalf("interval override lost: %+v", job)
		}
	}

	// Turning everything back on leaves an empty (default) state, not stale entries.
	on := true
	_ = reloaded.SetService("book", true)
	_ = reloaded.SetJob("meme.dispatch", JobPatch{Enabled: &on, ClearInterval: true})
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "book") || strings.Contains(string(raw), "meme.dispatch") {
		t.Fatalf("defaults should not be stored, file is %s", raw)
	}
}

func TestACorruptStateFilePausesEveryJobInsteadOfResumingThem(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	controls, err := NewControls(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range KnownJobs {
		if controls.JobEnabled(name) {
			t.Fatalf("job %s must start paused after a corrupt state file", name)
		}
	}
	if controls.LoadNote() == "" {
		t.Fatal("the problem must be reported")
	}
	if !controls.ServiceEnabled("meme") {
		t.Fatal("services stay enabled; only posting is held back")
	}
}

func TestControlsRejectUnknownNamesAndBadIntervals(t *testing.T) {
	controls, _ := NewControls("")
	if err := controls.SetService("../etc/passwd", false); err == nil {
		t.Fatal("unknown service must be rejected")
	}
	off := false
	if err := controls.SetJob("meme.fetch", JobPatch{Enabled: &off}); err == nil {
		t.Fatal("a routable event that is not a scheduler job must be rejected")
	}
	for _, bad := range []time.Duration{time.Second, 59 * time.Second, MaxJobInterval + time.Second} {
		bad := bad
		if err := controls.SetJob("meme.refresh", JobPatch{Interval: &bad}); err == nil {
			t.Fatalf("interval %s must be rejected", bad)
		}
	}
	if !controls.JobEnabled("meme.refresh") {
		t.Fatal("a rejected change must not alter state")
	}
}

func TestFailedSaveRollsTheChangeBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	controls, err := NewControls(path)
	if err != nil {
		t.Fatal(err)
	}
	// A directory where the file should go makes the final rename fail.
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := controls.SetService("meme", false); err == nil {
		t.Fatal("expected the save to fail")
	}
	if !controls.ServiceEnabled("meme") {
		t.Fatal("state must not change when it cannot be persisted")
	}
	off := false
	if err := controls.SetJob("meme.refresh", JobPatch{Enabled: &off}); err == nil || !controls.JobEnabled("meme.refresh") {
		t.Fatal("a failed job change must roll back too")
	}
}

func TestAnnouncedScheduleFeedsTheJobView(t *testing.T) {
	controls, _ := NewControls("")
	next := time.Date(2026, 9, 18, 21, 0, 0, 0, time.UTC)
	controls.Announce([]JobAnnounce{
		{Name: "meme.dispatch", Interval: 6 * time.Hour, NextFire: next},
		{Name: "not.a.job", Interval: time.Hour},
	}, time.Now())

	for _, job := range controls.Jobs() {
		if job.Name == "meme.dispatch" && (job.Interval != 6*time.Hour || !job.NextAt.Equal(next) || job.IntervalOverride) {
			t.Fatalf("unexpected view %+v", job)
		}
	}
	if len(controls.Jobs()) != len(KnownJobs) {
		t.Fatal("announcements for unknown jobs must be ignored")
	}

	fast := 10 * time.Minute
	_ = controls.SetJob("meme.dispatch", JobPatch{Interval: &fast})
	for _, job := range controls.Jobs() {
		if job.Name == "meme.dispatch" && (job.Interval != fast || !job.IntervalOverride) {
			t.Fatalf("an override must win over the announced interval, got %+v", job)
		}
	}
}

func TestControlsAreSafeForConcurrentIngresses(t *testing.T) {
	controls, _ := NewControls(filepath.Join(t.TempDir(), "state.json"))
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for n := 0; n < 40; n++ {
				controls.SetService("meme", n%2 == 0)
				controls.ServiceEnabled("meme")
				controls.Jobs()
				controls.RecordRun("meme.refresh", "ran", nil, time.Now())
				controls.Announce([]JobAnnounce{{Name: "meme.refresh", Interval: time.Hour}}, time.Now())
			}
		}(i)
	}
	wg.Wait()
}

func TestSwitchingTheSchedulerOffPausesEveryScheduledRunButNotManualOnes(t *testing.T) {
	service, controls, _ := gatedService(t)
	if err := controls.SetService("scheduler", false); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct{ eventType, schedule string }{
		{"meme.pool.refresh", "meme.refresh"},
		{"meme.dispatch.scheduled", "meme.dispatch"},
	} {
		result := process(t, service, tc.eventType, tc.schedule)
		if result.Result["skipped"] != true || !strings.Contains(result.Result["reason"].(string), "scheduler is paused") {
			t.Fatalf("%s must be skipped while the scheduler is off, got %#v", tc.schedule, result.Result)
		}
	}
	if result := process(t, service, "meme.dispatch.scheduled", ""); result.Result["skipped"] != nil {
		t.Fatalf("manual runs must still work, got %#v", result.Result)
	}
	if result := process(t, service, "scheduler.jobs.announce", ""); result.Result["skipped"] != nil {
		t.Fatal("the scheduler's announcements must never be gated, or the panel could not see it")
	}

	_ = controls.SetService("scheduler", true)
	if result := process(t, service, "meme.pool.refresh", "meme.refresh"); result.Result["skipped"] != nil {
		t.Fatalf("switching it back on resumes the jobs, got %#v", result.Result)
	}
}

func TestLastAnnounceIsTheNewestSchedulerReport(t *testing.T) {
	controls, _ := NewControls("")
	if !controls.LastAnnounce().IsZero() {
		t.Fatal("a scheduler that never reported has no announce time")
	}
	older, newer := time.Now().Add(-time.Hour), time.Now()
	controls.Announce([]JobAnnounce{{Name: "meme.refresh", Interval: time.Hour}}, older)
	controls.Announce([]JobAnnounce{{Name: "meme.dispatch", Interval: time.Hour}}, newer)
	if !controls.LastAnnounce().Equal(newer) {
		t.Fatalf("expected the newest announce, got %s", controls.LastAnnounce())
	}
}
