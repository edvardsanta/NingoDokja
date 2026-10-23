package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"dokja_interfaces/cli/cli"
)

type fakeRuntime struct {
	snapshot  cli.RuntimeSnapshot
	settings  cli.SettingsView
	providers cli.ProvidersView
	calls     []call
}

func (f *fakeRuntime) Providers(context.Context) (cli.ProvidersView, error) {
	f.calls = append(f.calls, call{"runtime.providers", nil})
	return f.providers, nil
}

func (f *fakeRuntime) Settings(context.Context) (cli.SettingsView, error) {
	f.calls = append(f.calls, call{"runtime.settings", nil})
	return f.settings, nil
}

func (f *fakeRuntime) SetSettings(_ context.Context, values map[string]string) (cli.SettingsView, error) {
	payload := map[string]any{}
	for k, v := range values {
		payload[k] = v
	}
	f.calls = append(f.calls, call{"runtime.settings.set", payload})
	return f.settings, nil
}

func (f *fakeRuntime) Request(_ context.Context, eventType string, payload map[string]any) (cli.RuntimeSnapshot, error) {
	f.calls = append(f.calls, call{eventType, payload})
	return f.snapshot, nil
}

func withModules(t *testing.T, fake *fakeClient, runtime *fakeRuntime) *Model {
	t.Helper()
	m := NewModel(fake, time.Millisecond, time.Second)
	m.SetRuntime(runtime, "")
	pump(m, m.Init())
	return m
}

func modulesSnapshot(busy bool) cli.RuntimeSnapshot {
	return cli.RuntimeSnapshot{Busy: busy, Modules: []cli.RuntimeModule{
		{Name: "orchestrator", State: "ready"},
		{Name: "communications", State: "stopped"},
	}}
}

func TestModulesShowEvenWhenTheOrchestratorIsDown(t *testing.T) {
	fake := newFake()
	fake.errs["ningo.status"] = errors.New("connection refused")
	m := withModules(t, fake, &fakeRuntime{snapshot: modulesSnapshot(false)})
	view := m.View()
	for _, want := range []string{"communications", "connection refused"} {
		if !strings.Contains(view, want) {
			t.Fatalf("panel is missing %q:\n%s", want, view)
		}
	}
}

func TestSpaceOnAModuleStartsItWithoutTouchingSwitchesOrJobs(t *testing.T) {
	fake := newFake()
	runtime := &fakeRuntime{snapshot: modulesSnapshot(false)}
	m := withModules(t, fake, runtime)
	before := fake.count("services.set") + fake.count("scheduler.jobs.set")
	press(m, "down", "space")
	var set []call
	for _, c := range runtime.calls {
		if c.eventType == "runtime.set" {
			set = append(set, c)
		}
	}
	if len(set) != 1 || set[0].payload["module"] != "communications" || set[0].payload["enabled"] != true {
		t.Fatalf("%+v", runtime.calls)
	}
	if fake.count("services.set")+fake.count("scheduler.jobs.set") != before {
		t.Fatal("a module toggle must not change a service switch or a job")
	}
}

func TestModuleTogglesWaitWhileAnOperationRuns(t *testing.T) {
	runtime := &fakeRuntime{snapshot: modulesSnapshot(true)}
	m := withModules(t, newFake(), runtime)
	press(m, "space")
	for _, c := range runtime.calls {
		if c.eventType == "runtime.set" {
			t.Fatal("no change may start while the controller is busy")
		}
	}
}

func TestServicesStillToggleBelowTheModules(t *testing.T) {
	fake := newFake()
	m := withModules(t, fake, &fakeRuntime{snapshot: modulesSnapshot(false)})
	press(m, "down", "down", "space") // past the two modules, onto the first service
	if fake.count("services.set") != 1 {
		t.Fatalf("the first service row must toggle its switch, calls: %+v", fake.calls)
	}
}

func TestWithoutAControllerThePanelSaysWhy(t *testing.T) {
	m := NewModel(newFake(), time.Millisecond, time.Second)
	m.SetRuntime(nil, "this interface runs in a container")
	pump(m, m.Init())
	if !strings.Contains(m.View(), "runs in a container") {
		t.Fatal(m.View())
	}
}

func runtimeSets(runtime *fakeRuntime) int {
	n := 0
	for _, c := range runtime.calls {
		if c.eventType == "runtime.set" {
			n++
		}
	}
	return n
}

func TestStartingTheSchedulerSaysWhatWillRunAndWaitsForAYes(t *testing.T) {
	runtime := &fakeRuntime{snapshot: cli.RuntimeSnapshot{Modules: []cli.RuntimeModule{{Name: "scheduler", State: "stopped"}}}}
	m := withModules(t, newFake(), runtime)
	press(m, "space")
	view := m.View()
	if runtimeSets(runtime) != 0 || !strings.Contains(view, "meme.refresh") || strings.Contains(view, "meme.dispatch roda") {
		t.Fatalf("the scheduler must not start before a yes, and the notice names only enabled jobs:\n%s", view)
	}
	press(m, "n")
	if runtimeSets(runtime) != 0 {
		t.Fatal("a no must not start it")
	}
	press(m, "space", "y")
	if runtimeSets(runtime) != 1 {
		t.Fatal("a yes starts it")
	}
}

func TestStoppingDiscordNeedsNoConfirmation(t *testing.T) {
	runtime := &fakeRuntime{snapshot: cli.RuntimeSnapshot{Modules: []cli.RuntimeModule{{Name: "discord", State: "ready"}}}}
	m := withModules(t, newFake(), runtime)
	press(m, "space")
	if runtimeSets(runtime) != 1 {
		t.Fatal("stopping posts nothing and is not confirmed")
	}
}
