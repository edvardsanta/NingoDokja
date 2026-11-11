package runtime

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTaskQueueHonorsGlobalAndProviderLimitsAndCancellation(t *testing.T) {
	var work taskWorkbench
	first, err := work.acquire(t.Context(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	second, err := work.acquire(t.Context(), "kimi")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		release, err := work.acquire(ctx, "ollama")
		if release != nil {
			release()
		}
		done <- err
	}()
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("queued task was not cancelled: %v", err)
	}
	first()
	// Kimi still occupies one slot; its second call must wait despite global capacity.
	ctx2, cancel2 := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel2()
	if release, err := work.acquire(ctx2, "kimi"); !errors.Is(err, context.DeadlineExceeded) {
		if release != nil {
			release()
		}
		t.Fatalf("provider limit ignored: %v", err)
	}
	third, err := work.acquire(t.Context(), "ollama")
	if err != nil {
		t.Fatal(err)
	}
	third()
	second()
	if work.active != 0 || work.queued != 0 {
		t.Fatalf("leaked slots: %+v", work)
	}
}
func TestTaskDiagnosticsAreBoundedAndRejectFreeformFailures(t *testing.T) {
	d, _ := fresh()
	a := TaskAttempt{TaskID: 1, Profile: "analysis", Provider: "kimi", Model: "fixture", StartedAt: time.Now(), Outcome: "failed", Reason: "contract_rejected", Stage: "generation"}
	for i := 0; i < TaskHistoryLimit+5; i++ {
		a.TaskID = uint64(i + 1)
		if err := d.RecordTaskAttempt(a); err != nil {
			t.Fatal(err)
		}
	}
	view := d.TaskDiagnostics()
	if len(view.Attempts) != TaskHistoryLimit || view.Attempts[0].TaskID != 6 {
		t.Fatal(view)
	}
	a.Reason = "private prompt or provider error"
	if d.RecordTaskAttempt(a) == nil {
		t.Fatal("freeform failure retained")
	}
	view.Attempts[0].Model = "mutated"
	if d.TaskDiagnostics().Attempts[0].Model == "mutated" {
		t.Fatal("snapshot aliases live history")
	}
}

type profileFixture struct {
	*providerStoreFixture
	profiles TaskProfiles
}

func (s *profileFixture) ReadTaskProfiles() (TaskProfiles, error) { return s.profiles, nil }
func (s *profileFixture) WriteTaskProfiles(p TaskProfiles) (TaskProfiles, error) {
	if p.Revision != s.profiles.Revision {
		return p, ErrProviderConflict
	}
	p.Revision++
	s.profiles = p
	return p, nil
}

func TestTaskProfilesKeepExplicitFallbackOrderAndFilterDisabledModels(t *testing.T) {
	d, _ := fresh()
	p, _ := DecodeTaskProfiles(1, DefaultTaskProfileValues())
	s := &profileFixture{providerStoreFixture: preferencesFixture(t), profiles: p}
	s.preferences.Providers = []ProviderRegistration{{ID: "codex", Enabled: true, Models: []string{"luna", "large"}}, {ID: "kimi", Enabled: true, Models: []string{"fixture"}}}
	d.WithProviderSettings(s)
	p.Profiles["analysis"] = []ChatBinding{{Provider: "kimi", Model: "fixture"}, {Provider: "codex", Model: "large"}}
	saved, err := d.ConfigureTaskProfiles(p)
	if err != nil || saved.Revision != 2 {
		t.Fatal(saved, err)
	}
	rows, err := d.TaskBindings("analysis")
	if err != nil || len(rows) != 2 || rows[0].Provider != "kimi" || rows[1].Model != "large" {
		t.Fatal(rows, err)
	}
	if _, err = d.ConfigureTaskProfiles(p); !errors.Is(err, ErrProviderConflict) {
		t.Fatal("stale write accepted", err)
	}
	s.preferences.Providers[1].Enabled = false
	rows, err = d.TaskBindings("analysis")
	if err != nil || len(rows) != 1 || rows[0].Provider != "codex" {
		t.Fatal(rows, err)
	}
	p.Revision = 2
	p.Profiles["analysis"] = []ChatBinding{{Provider: "codex", Model: "undeclared"}}
	if _, err = d.ConfigureTaskProfiles(p); err == nil {
		t.Fatal("undeclared model accepted")
	}
}

type blockingTaskInference struct{ started chan struct{} }

func (f blockingTaskInference) Generate(ctx context.Context, b ChatBinding, m []ChatMessage) (string, error) {
	f.started <- struct{}{}
	<-ctx.Done()
	return "", ctx.Err()
}
func TestTaskCancellationReachesRunningProviderAndReleasesCapacity(t *testing.T) {
	d, _ := fresh()
	s := preferencesFixture(t)
	s.preferences.Providers = []ProviderRegistration{{ID: "codex", Enabled: true, Models: []string{"fixture"}}}
	started := make(chan struct{}, 1)
	d.WithProviderSettings(s).WithChat(nil, blockingTaskInference{started: started})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := d.ChatTask(ctx, "analysis", ChatBinding{Provider: "codex", Model: "fixture"}, []ChatMessage{{Role: "user", Content: "private"}})
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("provider not started")
	}
	if d.TaskDiagnostics().Active != 1 {
		t.Fatal("missing active task")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled provider kept running")
	}
	if d.TaskDiagnostics().Active != 0 {
		t.Fatal("slot leaked")
	}
}

func TestTaskQueueIsBounded(t *testing.T) {
	var work taskWorkbench
	work.queued = TaskQueueLimit
	if release, err := work.acquire(t.Context(), "codex"); err == nil {
		if release != nil {
			release()
		}
		t.Fatal("queue capacity ignored")
	}
}
