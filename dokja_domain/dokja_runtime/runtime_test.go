package runtime

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

type fake struct {
	mu         sync.Mutex
	containers map[string]Container
	calls      []string
	fail       string
	gate       chan struct{}
}

func (f *fake) Status(context.Context) (map[string]Container, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]Container{}
	for k, v := range f.containers {
		out[k] = v
	}
	return out, nil
}
func (f *fake) Start(_ context.Context, m Module) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "start:"+m.Name)
	f.containers[m.Service] = Container{Running: true}
	if m.Name == "embedding_init" {
		f.containers[m.Service] = Container{Completed: true}
	}
	return nil
}
func (f *fake) Stop(_ context.Context, m Module) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "stop:"+m.Name)
	delete(f.containers, m.Service)
	return nil
}
func (f *fake) Ready(ctx context.Context, m Module) error {
	if f.gate != nil {
		select {
		case <-f.gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if f.fail == m.Name {
		return errors.New("readiness failed")
	}
	return nil
}
func (f *fake) trace() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}
func fresh() (*Domain, *fake) {
	f := &fake{containers: map[string]Container{}}
	return New(f, Journal{}, nil), f
}
func settle(t *testing.T, d *Domain) Snapshot {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		s := d.List(context.Background())
		if !s.Busy {
			return s
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("operation did not settle")
	return Snapshot{}
}
func row(s Snapshot, name string) State {
	for _, m := range s.Modules {
		if m.Name == name {
			return m
		}
	}
	return State{}
}
func change(t *testing.T, d *Domain, name string, on bool) Snapshot {
	t.Helper()
	if _, err := d.Set(name, on); err != nil {
		t.Fatal(err)
	}
	return settle(t, d)
}

func TestOpeningDesktopDoesNotStartContainersAndOneModuleStartsOnlyDependencies(t *testing.T) {
	d, f := fresh()
	s := d.List(context.Background())
	if s.Busy || len(f.trace()) != 0 {
		t.Fatal("read must never start containers")
	}
	if got := row(change(t, d, "memory", true), "memory").State; got != "ready" {
		t.Fatal(got)
	}
	want := []string{"start:orchestrator", "start:embeddings", "start:embedding_init", "start:memory"}
	if !reflect.DeepEqual(f.trace(), want) {
		t.Fatalf("unexpected startup %v", f.trace())
	}
	change(t, d, "memory", true)
	if !reflect.DeepEqual(f.trace(), want) {
		t.Fatal("repeated On must not duplicate starts")
	}
	change(t, d, "memory", false)
	want = append(want, "stop:memory", "stop:embedding_init", "stop:embeddings", "stop:orchestrator")
	if !reflect.DeepEqual(f.trace(), want) {
		t.Fatalf("unused owned dependencies must stop in reverse order: %v", f.trace())
	}
}
func TestSharedDependenciesAndExplicitCoreSurviveStoppingOneModule(t *testing.T) {
	d, f := fresh()
	change(t, d, "memory", true)
	change(t, d, "knowledge", true)
	before := len(f.trace())
	change(t, d, "memory", false)
	if got := f.trace()[before:]; !reflect.DeepEqual(got, []string{"stop:memory"}) {
		t.Fatalf("shared dependencies stopped: %v", got)
	}
	s := change(t, d, "orchestrator", false)
	if row(s, "orchestrator").State != "failed" {
		t.Fatal("cannot stop an in-use core")
	}
	change(t, d, "knowledge", false)
	d, f = fresh()
	change(t, d, "orchestrator", true)
	change(t, d, "meme", true)
	before = len(f.trace())
	change(t, d, "meme", false)
	if got := f.trace()[before:]; !reflect.DeepEqual(got, []string{"stop:meme"}) {
		t.Fatalf("explicit core should remain: %v", got)
	}
}
func TestBorrowedAndExternallyRunningDependenciesArePreserved(t *testing.T) {
	d, f := fresh()
	f.containers["dokja-orchestrator"] = Container{Running: true}
	f.containers["dokja-knowledge"] = Container{Running: true}
	change(t, d, "memory", true)
	before := len(f.trace())
	change(t, d, "memory", false)
	if got := f.trace()[before:]; !reflect.DeepEqual(got, []string{"stop:memory"}) {
		t.Fatalf("external module's dependencies stopped: %v", got)
	}
}
func TestStoppedContainerWithZeroExitCodeIsRestarted(t *testing.T) {
	d, f := fresh()
	f.containers["dokja-book"] = Container{Completed: true}
	change(t, d, "book", true)
	if !reflect.DeepEqual(f.trace(), []string{"start:orchestrator", "start:book"}) {
		t.Fatal(f.trace())
	}
}
func TestReadinessFailureIsNotReadyAndCanBeRetried(t *testing.T) {
	d, f := fresh()
	f.fail = "memory"
	s := change(t, d, "memory", true)
	if row(s, "memory").State != "failed" || row(s, "memory").Error == "" {
		t.Fatal(s)
	}
	f.fail = ""
	s = change(t, d, "memory", true)
	if row(s, "memory").State != "ready" {
		t.Fatal(s)
	}
}
func TestDuplicateTransitionIsIdempotentAndOtherOperationsCannotCross(t *testing.T) {
	d, f := fresh()
	f.gate = make(chan struct{})
	s, err := d.Set("memory", true)
	if err != nil || !s.Busy {
		t.Fatal(s, err)
	}
	if _, err = d.Set("memory", true); err != nil {
		t.Fatal(err)
	}
	if _, err = d.Set("book", true); err == nil {
		t.Fatal("parallel lifecycle operations must be refused")
	}
	close(f.gate)
	settle(t, d)
	if len(f.trace()) != 4 {
		t.Fatal(f.trace())
	}
}
func TestOwnershipJournalSurvivesDaemonRestartWithoutStartingAnything(t *testing.T) {
	d, f := fresh()
	var saved Journal
	d.save = func(j Journal) error { saved = j; return nil }
	change(t, d, "memory", true)
	restored := New(f, saved, d.save)
	before := len(f.trace())
	restored.List(context.Background())
	if len(f.trace()) != before {
		t.Fatal("restoring daemon must not start anything")
	}
	change(t, restored, "memory", false)
	if got := f.trace()[before:]; !reflect.DeepEqual(got, []string{"stop:memory", "stop:embedding_init", "stop:embeddings", "stop:orchestrator"}) {
		t.Fatal(got)
	}
}
func TestInvalidModuleAndUnwritableOwnershipNeverExecuteCommands(t *testing.T) {
	d, f := fresh()
	for _, name := range []string{"", "embeddings", "embedding_init", "--all", "meme; shutdown"} {
		if _, err := d.Set(name, true); err == nil {
			t.Fatal(name)
		}
	}
	d.save = func(Journal) error { return errors.New("disk error") }
	if row(change(t, d, "memory", true), "memory").State != "failed" || len(f.trace()) != 0 {
		t.Fatal("must fail before starting without ownership persistence")
	}
}

func TestExternalInterfacesKeepTheirCoreAndVoiceDependencies(t *testing.T) {
	d, f := fresh()
	change(t, d, "voice", true)
	f.mu.Lock()
	f.containers["dokja-discord"] = Container{Running: true}
	f.mu.Unlock()
	before := len(f.trace())
	s := change(t, d, "voice", false)
	if row(s, "voice").State != "failed" || len(f.trace()) != before {
		t.Fatal("an external interface's required service must not stop")
	}
}

func TestWhatsAppStartsOnlyItsGatewayAndCoreAndPreservesSessionContainers(t *testing.T) {
	d, f := fresh()
	change(t, d, "whatsapp", true)
	if !reflect.DeepEqual(f.trace(), []string{"start:orchestrator", "start:whatsapp"}) {
		t.Fatal(f.trace())
	}
	change(t, d, "whatsapp", false)
	if !reflect.DeepEqual(f.trace(), []string{"start:orchestrator", "start:whatsapp", "stop:whatsapp", "stop:orchestrator"}) {
		t.Fatal(f.trace())
	}
}

func TestCommunicationsStartsOnlyItsReaderAndCoreNeverDelivery(t *testing.T) {
	d, f := fresh()
	change(t, d, "communications", true)
	if !reflect.DeepEqual(f.trace(), []string{"start:orchestrator", "start:communications"}) {
		t.Fatal(f.trace())
	}
	change(t, d, "communications", false)
	if !reflect.DeepEqual(f.trace(), []string{"start:orchestrator", "start:communications", "stop:communications", "stop:orchestrator"}) {
		t.Fatal(f.trace())
	}
}

func TestAStopThatEndsWithANonZeroExitIsStillStopped(t *testing.T) {
	d, f := fresh()
	change(t, d, "book", true)
	change(t, d, "book", false)
	// A process killed by the stop exits non-zero; the engine reports it as a failed exit.
	f.mu.Lock()
	f.containers["dokja-book"] = Container{Failed: true}
	f.containers["dokja-orchestrator"] = Container{Failed: true}
	f.mu.Unlock()
	s := d.List(context.Background())
	if row(s, "book").State != "stopped" || row(s, "orchestrator").State != "stopped" {
		t.Fatalf("%+v", s.Modules)
	}
}

func TestAContainerThatCrashesOnItsOwnIsAFailure(t *testing.T) {
	d, f := fresh()
	change(t, d, "book", true)
	f.mu.Lock()
	f.containers["dokja-book"] = Container{Failed: true}
	f.mu.Unlock()
	if row(d.List(context.Background()), "book").State != "failed" {
		t.Fatal("a crash the controller did not cause must show as failed")
	}
}

func TestDiscordStartsWithItsVoiceAndSchedulerOnlyWithTheCore(t *testing.T) {
	d, f := fresh()
	change(t, d, "discord", true)
	if !reflect.DeepEqual(f.trace(), []string{"start:orchestrator", "start:voice", "start:discord"}) {
		t.Fatal(f.trace())
	}
	d, f = fresh()
	change(t, d, "scheduler", true)
	if !reflect.DeepEqual(f.trace(), []string{"start:orchestrator", "start:scheduler"}) {
		t.Fatal(f.trace())
	}
}
