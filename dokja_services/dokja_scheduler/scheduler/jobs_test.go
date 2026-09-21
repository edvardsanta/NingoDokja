package scheduler

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type recorder struct {
	mu     sync.Mutex
	events []Event
}

func (r *recorder) emit(_ context.Context, event Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
	return nil
}

func (r *recorder) count(eventType string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, event := range r.events {
		if event.Type == eventType {
			n++
		}
	}
	return n
}

func (r *recorder) snapshot() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Event(nil), r.events...)
}

func testService(t *testing.T, refresh time.Duration, stateFile string) (*Service, *recorder) {
	t.Helper()
	rec := &recorder{}
	return &Service{
		logger:            log.New(io.Discard, "", 0),
		refreshInterval:   refresh,
		dispatchInterval:  time.Hour,
		refreshMaxItems:   20,
		dispatchMemeLimit: 1,
		stateFile:         stateFile,
		statePoll:         10 * time.Millisecond,
		now:               time.Now,
		emit:              rec.emit,
	}, rec
}

func writeState(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadOverridesKeepsOnlyUsableIntervals(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	writeState(t, path, `{"services":{"meme":true},"jobs":{
		"meme.dispatch":{"interval":"2h"},
		"meme.refresh":{"interval":"5s"},
		"meme.dispatch.nonsense":{"interval":"nonsense"},
		"unknown.job":{"enabled":false}}}`)
	service, _ := testService(t, time.Hour, path)

	overrides, ok := service.loadOverrides()

	if !ok || len(overrides) != 1 || overrides["meme.dispatch"] != 2*time.Hour {
		t.Fatalf("only the valid 2h override should survive, got %v (ok=%v)", overrides, ok)
	}
}

func TestLoadOverridesToleratesMissingUnreadableAndUnsetFiles(t *testing.T) {
	dir := t.TempDir()

	missing, _ := testService(t, time.Hour, filepath.Join(dir, "none.json"))
	if overrides, ok := missing.loadOverrides(); len(overrides) != 0 || !ok {
		t.Fatalf("a missing file means no overrides, got %v ok=%v", overrides, ok)
	}

	broken := filepath.Join(dir, "broken.json")
	writeState(t, broken, "{oops")
	corrupt, _ := testService(t, time.Hour, broken)
	if overrides, ok := corrupt.loadOverrides(); len(overrides) != 0 || ok {
		t.Fatalf("a corrupt file must be ignored and reported, got %v ok=%v", overrides, ok)
	}

	unset, _ := testService(t, time.Hour, "")
	if overrides, ok := unset.loadOverrides(); len(overrides) != 0 || !ok {
		t.Fatalf("no state file configured means no overrides, got %v", overrides)
	}
}

func TestPlanRestartsTheCountdownWhenAnIntervalChanges(t *testing.T) {
	service, _ := testService(t, time.Hour, "")
	jobs := service.jobs()
	start := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	p := newPlan(jobs, start)

	if got := p.next["meme.refresh"]; !got.Equal(start.Add(time.Hour)) {
		t.Fatalf("first run is one interval away, got %s", got)
	}

	later := start.Add(30 * time.Minute)
	if !p.applyOverrides(map[string]time.Duration{"meme.refresh": 10 * time.Minute}, later) {
		t.Fatal("a new interval is a change")
	}
	if got := p.next["meme.refresh"]; !got.Equal(later.Add(10 * time.Minute)) {
		t.Fatalf("lowering an interval must not fire immediately, next=%s", got)
	}
	if p.applyOverrides(map[string]time.Duration{"meme.refresh": 10 * time.Minute}, later) {
		t.Fatal("re-applying the same interval is not a change")
	}

	p.applyOverrides(nil, later.Add(time.Minute))
	if p.interval(jobs[0]) != time.Hour {
		t.Fatalf("dropping the override goes back to the default, got %s", p.interval(jobs[0]))
	}

	ranAt := later.Add(2 * time.Minute)
	p.ran(jobs[0], ranAt)
	if !p.last["meme.refresh"].Equal(ranAt) || !p.next["meme.refresh"].Equal(ranAt.Add(time.Hour)) {
		t.Fatal("running a job restarts its countdown")
	}
}

func TestPlanDueAndEarliest(t *testing.T) {
	service, _ := testService(t, time.Minute, "")
	start := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	p := newPlan(service.jobs(), start)

	if got := p.earliest(); !got.Equal(start.Add(time.Minute)) {
		t.Fatalf("earliest should be the 1 minute refresh, got %s", got)
	}
	if due := p.due(start); len(due) != 0 {
		t.Fatalf("nothing is due yet, got %d", len(due))
	}
	due := p.due(start.Add(time.Minute))
	if len(due) != 1 || due[0].name != "meme.refresh" {
		t.Fatalf("only the refresh is due, got %+v", due)
	}
}

func TestAnnounceReportsJobsWithoutClaimingToBeAJobRun(t *testing.T) {
	service, rec := testService(t, time.Hour, "")
	start := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	p := newPlan(service.jobs(), start)
	p.ran(service.jobs()[1], start) // meme.dispatch

	service.announce(context.Background(), p)

	events := rec.snapshot()
	if len(events) != 1 || events[0].Type != "scheduler.jobs.announce" || events[0].Source != "scheduler" {
		t.Fatalf("unexpected announce %+v", events)
	}
	if _, stamped := events[0].Context["schedule"]; stamped {
		t.Fatal("an announce must not carry a schedule stamp, or the orchestrator would gate it as a job run")
	}
	entries := events[0].Payload["jobs"].([]map[string]any)
	if len(entries) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(entries))
	}
	for _, entry := range entries {
		_, hasLast := entry["last_fire"]
		if entry["name"] == "meme.dispatch" && (!hasLast || entry["next_fire"] != "2026-09-18T13:00:00Z" || entry["interval"] != "1h0m0s") {
			t.Fatalf("unexpected dispatch entry %#v", entry)
		}
		if entry["name"] == "meme.refresh" && hasLast {
			t.Fatalf("a job that never ran has no last_fire: %#v", entry)
		}
	}
}

func TestEveryJobRunCarriesItsScheduleStamp(t *testing.T) {
	service, rec := testService(t, time.Hour, "")
	for _, j := range service.jobs() {
		if err := j.run(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	events := rec.snapshot()
	jobs := service.jobs()
	if len(events) != len(jobs) {
		t.Fatalf("expected one event per job, got %d", len(events))
	}
	for i, event := range events {
		if event.Context["schedule"] != jobs[i].name {
			t.Errorf("job %s emitted an event stamped %v", jobs[i].name, event.Context["schedule"])
		}
	}
}

func TestRunBootstrapsEveryJobThenKeepsTheRefreshTicking(t *testing.T) {
	service, rec := testService(t, 40*time.Millisecond, "")
	ctx, cancel := context.WithTimeout(context.Background(), 320*time.Millisecond)
	defer cancel()

	if err := service.Run(ctx); err != context.DeadlineExceeded {
		t.Fatalf("Run should end with the context, got %v", err)
	}

	for _, eventType := range []string{"meme.pool.refresh", "meme.dispatch.scheduled"} {
		if rec.count(eventType) < 1 {
			t.Errorf("bootstrap should emit %s once", eventType)
		}
	}
	if rec.count("meme.pool.refresh") < 4 {
		t.Errorf("the 40ms refresh should have ticked several times, got %d", rec.count("meme.pool.refresh"))
	}
	if rec.count("meme.dispatch.scheduled") != 1 {
		t.Errorf("an hourly job must not repeat within the test, got %d", rec.count("meme.dispatch.scheduled"))
	}
	if rec.count("scheduler.jobs.announce") < 3 {
		t.Errorf("expected announcements at start and after runs, got %d", rec.count("scheduler.jobs.announce"))
	}
}

func TestAnIntervalOverrideWrittenWhileRunningTakesEffect(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	service, rec := testService(t, 30*time.Millisecond, path)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		service.Run(ctx)
		close(done)
	}()

	time.Sleep(150 * time.Millisecond)
	if rec.count("meme.pool.refresh") < 3 {
		t.Fatalf("the refresh should be ticking before the override, got %d", rec.count("meme.pool.refresh"))
	}

	writeState(t, path, `{"jobs":{"meme.refresh":{"interval":"1h"}}}`)
	time.Sleep(100 * time.Millisecond) // let a state poll adopt it
	settled := rec.count("meme.pool.refresh")
	time.Sleep(200 * time.Millisecond)
	if after := rec.count("meme.pool.refresh"); after != settled {
		t.Fatalf("an hourly override must stop the 30ms ticks (%d -> %d)", settled, after)
	}

	writeState(t, path, `{}`)
	time.Sleep(200 * time.Millisecond)
	if rec.count("meme.pool.refresh") <= settled {
		t.Fatal("removing the override must bring the default interval back")
	}

	cancel()
	<-done

	announced := false
	for _, event := range rec.snapshot() {
		if event.Type != "scheduler.jobs.announce" {
			continue
		}
		for _, entry := range event.Payload["jobs"].([]map[string]any) {
			if entry["name"] == "meme.refresh" && strings.HasPrefix(entry["interval"].(string), "1h") {
				announced = true
			}
		}
	}
	if !announced {
		t.Fatal("the new interval should have been announced to the orchestrator")
	}
}

func TestJobNamesMatchWhatTheOrchestratorSwitchesOn(t *testing.T) {
	service, _ := testService(t, time.Hour, "")
	got := []string{}
	for name := range service.JobNames() {
		got = append(got, name)
	}
	want := []string{"meme.refresh", "meme.dispatch"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("job names changed: %v", got)
	}
}
