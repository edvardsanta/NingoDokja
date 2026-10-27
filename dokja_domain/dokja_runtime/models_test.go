package runtime

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// engine stands in for the local model engine: a pull reports what the test feeds it, then ends
// with the test's answer, or when it is cancelled.
type engine struct {
	mu       sync.Mutex
	progress chan PullProgress
	end      chan error
	removed  []string
	pulled   []string
	returned chan error
}

func newEngine() *engine {
	return &engine{progress: make(chan PullProgress), end: make(chan error, 1), returned: make(chan error, 8)}
}

func (e *engine) Pull(ctx context.Context, model string, progress func(PullProgress)) (err error) {
	defer func() { e.returned <- err }()
	e.mu.Lock()
	e.pulled = append(e.pulled, model)
	e.mu.Unlock()
	for {
		select {
		case p := <-e.progress:
			progress(p)
		case err := <-e.end:
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
func (e *engine) Remove(_ context.Context, model string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.removed = append(e.removed, model)
	return nil
}
func (e *engine) EmbeddingModel() string { return "bge-m3" }

func modelsDomain(containers map[string]Container) (*Domain, *engine) {
	e := newEngine()
	d, _ := newDomainWith(containers)
	return d.WithModels(e), e
}

func newDomainWith(containers map[string]Container) (*Domain, *fake) {
	f := &fake{containers: containers}
	return New(f, Journal{}, nil), f
}

var engineUp = map[string]Container{"dokja-ollama": {Running: true}}

// waitPull polls the status until the check holds, as a screen would.
func waitPull(t *testing.T, d *Domain, check func(*ModelPull) bool) *ModelPull {
	t.Helper()
	for i := 0; i < 200; i++ {
		status, _ := d.ModelsStatus()
		if status.Pull != nil && check(status.Pull) {
			return status.Pull
		}
		time.Sleep(5 * time.Millisecond)
	}
	status, _ := d.ModelsStatus()
	t.Fatalf("pull never reached the state: %+v", status.Pull)
	return nil
}

func TestPullRunsInTheBackgroundAndReportsItsProgress(t *testing.T) {
	d, e := modelsDomain(engineUp)
	status, err := d.PullModel(context.Background(), "qwen3:8b")
	if err != nil || status.Pull == nil || status.Pull.State != "pulling" || status.Pull.Phase != "starting" || status.EmbeddingModel != "bge-m3" {
		t.Fatal(status, err)
	}
	e.progress <- PullProgress{Phase: "downloading", Percent: 42}
	pull := waitPull(t, d, func(p *ModelPull) bool { return p.Percent != nil && *p.Percent == 42 })
	if pull.Phase != "downloading" {
		t.Fatal(pull)
	}
	// An unknown phase or an impossible share never reaches the screen.
	e.progress <- PullProgress{Phase: "<b>raw engine text</b>", Percent: 900}
	e.progress <- PullProgress{Phase: "verifying", Percent: -1}
	pull = waitPull(t, d, func(p *ModelPull) bool { return p.Phase == "verifying" })
	if *pull.Percent != 42 {
		t.Fatal(pull)
	}
	e.end <- nil
	pull = waitPull(t, d, func(p *ModelPull) bool { return p.State == "done" })
	if pull.Phase != "" || pull.Percent != nil || pull.Error != "" {
		t.Fatal(pull)
	}
}

func TestOnePullAtATimeAndOnlyWhileTheEngineRuns(t *testing.T) {
	stopped, _ := modelsDomain(map[string]Container{})
	if _, err := stopped.PullModel(context.Background(), "qwen3:8b"); err == nil {
		t.Fatal("a pull needs the engine running")
	}
	d, e := modelsDomain(engineUp)
	for _, name := range []string{"", "-rm", "--insecure", "a b", "x;rm -rf /"} {
		if _, err := d.PullModel(context.Background(), name); err == nil {
			t.Fatalf("%q must be refused", name)
		}
	}
	if _, err := d.PullModel(context.Background(), "qwen3:8b"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.PullModel(context.Background(), "qwen3:8b"); err != nil {
		t.Fatal("asking again for the running pull is not an error", err)
	}
	if _, err := d.PullModel(context.Background(), "gemma3:4b"); err == nil {
		t.Fatal("a second model must wait")
	}
	e.end <- nil
	waitPull(t, d, func(p *ModelPull) bool { return p.State == "done" })
	if _, err := d.PullModel(context.Background(), "gemma3:4b"); err != nil {
		t.Fatal("after the first ends another may start", err)
	}
}

func TestPullFailuresAreSaidPlainly(t *testing.T) {
	d, e := modelsDomain(engineUp)
	d.PullModel(context.Background(), "no-such-model")
	e.end <- ErrModelNotFound
	pull := waitPull(t, d, func(p *ModelPull) bool { return p.State == "failed" })
	if pull.Error != "the registry has no model by this name" {
		t.Fatal(pull)
	}
	d.PullModel(context.Background(), "qwen3:8b")
	e.end <- errors.New("exit status 1: some engine output")
	pull = waitPull(t, d, func(p *ModelPull) bool { return p.State == "failed" && p.Model == "qwen3:8b" })
	if pull.Error != "the download failed; check the network and the free disk space" {
		t.Fatal(pull)
	}
	// When the engine said why, its words are what the owner reads.
	d.PullModel(context.Background(), "qwen3.5:27b")
	e.end <- PullFailure{Detail: "digest mismatch, file must be downloaded again"}
	pull = waitPull(t, d, func(p *ModelPull) bool { return p.State == "failed" && p.Model == "qwen3.5:27b" })
	if pull.Error != "the engine reported: digest mismatch, file must be downloaded again" {
		t.Fatal(pull)
	}
}

func TestCancelStopsTheDownload(t *testing.T) {
	d, e := modelsDomain(engineUp)
	d.PullModel(context.Background(), "qwen3:8b")
	status, err := d.CancelPull()
	if err != nil || status.Pull.State != "cancelled" {
		t.Fatal(status, err)
	}
	// The engine saw its context end, and the state stays cancelled, not failed.
	time.Sleep(20 * time.Millisecond)
	status, _ = d.ModelsStatus()
	if status.Pull.State != "cancelled" || status.Pull.Error != "" {
		t.Fatal(status.Pull)
	}
	select {
	case err := <-e.returned:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("the pull should have returned on cancel")
	}
}

func TestRemoveRefusesWhatIsInUse(t *testing.T) {
	d, e := modelsDomain(map[string]Container{"dokja-ollama": {Running: true}, "dokja-knowledge": {Running: true}})
	d.WithChat(&chatStore{b: ChatBinding{Provider: "ollama", Model: "qwen3:8b"}}, nil)
	if _, err := d.RemoveModel(context.Background(), "qwen3:8b"); err == nil {
		t.Fatal("the chat model must not be removed")
	}
	for _, name := range []string{"bge-m3", "bge-m3:latest"} {
		if _, err := d.RemoveModel(context.Background(), name); err == nil {
			t.Fatalf("the embedding model must not be removed while knowledge runs, named %q", name)
		}
	}
	if _, err := d.RemoveModel(context.Background(), "qwen3:8b"); err == nil {
		t.Fatal("the chat model must not be removed")
	}
	d.PullModel(context.Background(), "gemma3:4b")
	if _, err := d.RemoveModel(context.Background(), "gemma3:4b"); err == nil {
		t.Fatal("a model downloading must not be removed")
	}
	if _, err := d.RemoveModel(context.Background(), "--all"); err == nil {
		t.Fatal("an invalid name must be refused")
	}
	if _, err := d.RemoveModel(context.Background(), "llama3.2:3b"); err != nil {
		t.Fatal(err)
	}
	if len(e.removed) != 1 || e.removed[0] != "llama3.2:3b" {
		t.Fatal(e.removed)
	}
	d.CancelPull()

	quiet, qe := modelsDomain(engineUp)
	if _, err := quiet.RemoveModel(context.Background(), "bge-m3"); err != nil || len(qe.removed) != 1 {
		t.Fatal("with memory and knowledge stopped the embedding model may go", err)
	}
}

func TestSameModelReadsAMissingTagAsLatest(t *testing.T) {
	for _, pair := range [][2]string{{"bge-m3", "bge-m3:latest"}, {"library/qwen3", "library/qwen3:latest"}, {"host:5000/team/m", "host:5000/team/m:latest"}} {
		if !SameModel(pair[0], pair[1]) {
			t.Fatal(pair)
		}
	}
	if SameModel("qwen3:8b", "qwen3:latest") || SameModel("qwen3", "qwen3:8b") {
		t.Fatal("different tags are different models")
	}
}

func TestProvidersGiveTheEngineSizesAndItsEmbeddingModel(t *testing.T) {
	d, _ := modelsDomain(engineUp)
	d.WithProviders(providerFixture{{ID: "ollama", State: "reachable", Models: []string{"bge-m3:latest", "qwen3:8b"}, Sizes: map[string]string{"bge-m3:latest": "1.2 GB", "qwen3:8b": "<script>"}}})
	view := d.Providers(context.Background())
	for _, row := range view.Providers {
		if row.ID != "ollama" {
			continue
		}
		if row.EmbeddingModel != "bge-m3:latest" || row.ModelSizes["bge-m3:latest"] != "1.2 GB" || row.ModelSizes["qwen3:8b"] != "" {
			t.Fatal(row)
		}
		return
	}
	t.Fatal("no ollama row")
}

// gatedEngine holds each pull's return until the test lets it go, as a pull that is still stopping.
type gatedEngine struct {
	*engine
	release chan struct{}
}

func (g gatedEngine) Pull(ctx context.Context, model string, progress func(PullProgress)) error {
	err := g.engine.Pull(ctx, model, progress)
	<-g.release
	return err
}

func TestAPullStillStoppingNeverTakesOverTheNextOne(t *testing.T) {
	g := gatedEngine{engine: newEngine(), release: make(chan struct{})}
	d, _ := newDomainWith(engineUp)
	d.WithModels(g)
	d.PullModel(context.Background(), "qwen3:8b")
	d.CancelPull()
	if _, err := d.PullModel(context.Background(), "qwen3:8b"); err == nil {
		t.Fatal("a new pull must wait for the cancelled one to stop")
	}
	g.release <- struct{}{}
	waitFor(t, func() bool { _, err := d.PullModel(context.Background(), "qwen3:8b"); return err == nil })
	status, _ := d.ModelsStatus()
	if status.Pull.State != "pulling" {
		t.Fatal(status.Pull)
	}
	// The new pull is still the one that cancel reaches.
	if status, _ := d.CancelPull(); status.Pull.State != "cancelled" {
		t.Fatal(status.Pull)
	}
	g.release <- struct{}{}
}

func waitFor(t *testing.T, check func() bool) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if check() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("never happened")
}

func TestTheEngineCanStartOnItsOwn(t *testing.T) {
	d, f := newDomainWith(map[string]Container{})
	d.WithModels(newEngine())
	status, err := d.StartEngine()
	if err != nil || status.Engine != "starting" {
		t.Fatal(status, err)
	}
	waitFor(t, func() bool { s, _ := d.ModelsStatus(); return s.Engine == "ready" })
	if calls := f.trace(); len(calls) == 0 || strings.Join(calls, " ") != "start:embeddings" {
		t.Fatal(calls)
	}
	if _, err := d.Set("embeddings", false); err == nil {
		t.Fatal("the engine stays internal to the module switches")
	}
}
