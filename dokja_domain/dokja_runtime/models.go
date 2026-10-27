package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// PullProgress is what a running download reports: a phase from a fixed vocabulary and, while
// layers download, the share done. Percent is -1 when the engine has not said yet.
type PullProgress struct {
	Phase   string
	Percent int
}

// Pull phases, in order. Nothing else reaches a screen.
var pullPhases = map[string]bool{"starting": true, "downloading": true, "verifying": true, "writing": true}

// ErrModelNotFound is the engine's answer for a name its registry does not have.
var ErrModelNotFound = errors.New("model not found")

// PullFailure is a download the engine refused, with the engine's own words for why.
type PullFailure struct{ Detail string }

func (f PullFailure) Error() string { return "model pull failed: " + f.Detail }

// ModelEngine downloads and removes the local engine's models. It never chooses a model by itself.
type ModelEngine interface {
	Pull(ctx context.Context, model string, progress func(PullProgress)) error
	Remove(ctx context.Context, model string) error
	// EmbeddingModel is the model memory and knowledge embed with.
	EmbeddingModel() string
}

type ModelPull struct {
	Model string `json:"model"`
	// pulling, done, failed or cancelled
	State   string `json:"state"`
	Phase   string `json:"phase,omitempty"`
	Percent *int   `json:"percent,omitempty"`
	Error   string `json:"error,omitempty"`
}

type ModelsStatus struct {
	EmbeddingModel string     `json:"embedding_model"`
	Pull           *ModelPull `json:"pull,omitempty"`
	// What this controller last did to the engine: starting, ready or failed; empty when nothing.
	Engine      string `json:"engine,omitempty"`
	EngineError string `json:"engine_error,omitempty"`
}

type modelState struct {
	mu     sync.Mutex
	engine ModelEngine
	pull   *ModelPull
	cancel context.CancelFunc
	// running stays true until the last pull's work has ended, cancelled or not, so a new pull never
	// overlaps a stopping one; generation tells a pull's work which pull it belongs to.
	running    bool
	generation uint64
}

// pullTimeout bounds one download; models are gigabytes and a slow line takes long.
const pullTimeout = 2 * time.Hour

func (d *Domain) WithModels(engine ModelEngine) *Domain {
	d.models = &modelState{engine: engine}
	return d
}

func (d *Domain) ModelsStatus() (ModelsStatus, error) {
	if d.models == nil {
		return ModelsStatus{}, fmt.Errorf("local models are unavailable")
	}
	d.models.mu.Lock()
	defer d.models.mu.Unlock()
	return d.modelsStatusLocked(), nil
}

func (d *Domain) modelsStatusLocked() ModelsStatus {
	status := ModelsStatus{EmbeddingModel: d.models.engine.EmbeddingModel()}
	d.mu.Lock()
	if engine, ok := d.states["embeddings"]; ok && (engine.State == "starting" || engine.State == "ready" || engine.State == "failed") {
		status.Engine, status.EngineError = engine.State, engine.Error
	}
	d.mu.Unlock()
	if d.models.pull != nil {
		pull := *d.models.pull
		if pull.Percent != nil {
			percent := *pull.Percent
			pull.Percent = &percent
		}
		status.Pull = &pull
	}
	return status
}

// PullModel acknowledges a download and runs it in the background; poll ModelsStatus for its end.
// One download runs at a time, and only while the engine runs.
func (d *Domain) PullModel(ctx context.Context, model string) (ModelsStatus, error) {
	if d.models == nil {
		return ModelsStatus{}, fmt.Errorf("local models are unavailable")
	}
	if !modelName.MatchString(model) {
		return ModelsStatus{}, fmt.Errorf("invalid model name")
	}
	if err := d.engineRunning(ctx); err != nil {
		return ModelsStatus{}, err
	}
	d.models.mu.Lock()
	defer d.models.mu.Unlock()
	if d.models.pull != nil && d.models.pull.State == "pulling" {
		if d.models.pull.Model == model {
			return d.modelsStatusLocked(), nil
		}
		return ModelsStatus{}, fmt.Errorf("another model is downloading")
	}
	if d.models.running {
		return ModelsStatus{}, fmt.Errorf("the last download is still stopping; try again in a moment")
	}
	// The request's cancellation never stops a download the controller accepted; cancel does.
	pullCtx, cancel := context.WithTimeout(context.Background(), pullTimeout)
	d.models.generation++
	d.models.pull = &ModelPull{Model: model, State: "pulling", Phase: "starting"}
	d.models.cancel = cancel
	d.models.running = true
	go d.runPull(pullCtx, cancel, model, d.models.generation)
	return d.modelsStatusLocked(), nil
}

func (d *Domain) runPull(ctx context.Context, cancel context.CancelFunc, model string, generation uint64) {
	defer cancel()
	err := d.models.engine.Pull(ctx, model, func(p PullProgress) {
		d.models.mu.Lock()
		defer d.models.mu.Unlock()
		pull := d.models.pull
		if d.models.generation != generation || pull == nil || pull.State != "pulling" || !pullPhases[p.Phase] {
			return
		}
		pull.Phase = p.Phase
		if p.Percent >= 0 && p.Percent <= 100 {
			percent := p.Percent
			pull.Percent = &percent
		}
	})
	d.models.mu.Lock()
	defer d.models.mu.Unlock()
	pull := d.models.pull
	if d.models.generation != generation || pull == nil {
		return
	}
	d.models.running = false
	d.models.cancel = nil
	pull.Phase = ""
	switch {
	case pull.State == "cancelled":
	case err == nil:
		pull.State, pull.Percent = "done", nil
	case errors.Is(err, ErrModelNotFound):
		pull.State, pull.Error = "failed", "the registry has no model by this name"
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		pull.State, pull.Error = "failed", "the download took too long and was stopped"
	case pullFailureDetail(err) != "":
		pull.State, pull.Error = "failed", "the engine reported: "+pullFailureDetail(err)
	default:
		pull.State, pull.Error = "failed", "the download failed; check the network and the free disk space"
	}
}

// CancelPull stops the running download, if any. What was downloaded so far stays with the engine,
// which resumes it on the next pull of the same model.
func (d *Domain) CancelPull() (ModelsStatus, error) {
	if d.models == nil {
		return ModelsStatus{}, fmt.Errorf("local models are unavailable")
	}
	d.models.mu.Lock()
	defer d.models.mu.Unlock()
	if d.models.pull != nil && d.models.pull.State == "pulling" {
		d.models.pull.State, d.models.pull.Phase, d.models.pull.Percent = "cancelled", "", nil
		if d.models.cancel != nil {
			d.models.cancel()
		}
	}
	return d.modelsStatusLocked(), nil
}

// RemoveModel deletes an installed model. It refuses the model downloading now, the model chat is
// bound to, and the embedding model while memory or knowledge run.
func (d *Domain) RemoveModel(ctx context.Context, model string) (ModelsStatus, error) {
	if d.models == nil {
		return ModelsStatus{}, fmt.Errorf("local models are unavailable")
	}
	if !modelName.MatchString(model) {
		return ModelsStatus{}, fmt.Errorf("invalid model name")
	}
	d.models.mu.Lock()
	pulling := d.models.pull != nil && d.models.pull.State == "pulling" && d.models.pull.Model == model
	d.models.mu.Unlock()
	if pulling {
		return ModelsStatus{}, fmt.Errorf("this model is downloading; cancel the download first")
	}
	if d.chatSettings != nil {
		if binding, err := d.chatSettings.ReadChat(); err == nil && binding.Provider == "ollama" && SameModel(binding.Model, model) {
			return ModelsStatus{}, fmt.Errorf("chat uses this model; choose another chat model first")
		}
	}
	containers, err := d.backend.Status(ctx)
	if err != nil {
		return ModelsStatus{}, fmt.Errorf("cannot read the modules' state")
	}
	if !containers[engineService].Running {
		return ModelsStatus{}, fmt.Errorf("the local model engine is stopped")
	}
	if SameModel(model, d.models.engine.EmbeddingModel()) {
		for _, name := range []string{"memory", "knowledge"} {
			if m, ok := Lookup(name); ok && containers[m.Service].Running {
				return ModelsStatus{}, fmt.Errorf("memory and knowledge embed with this model; stop them first")
			}
		}
	}
	if err := d.models.engine.Remove(ctx, model); err != nil {
		if errors.Is(err, ErrModelNotFound) {
			return ModelsStatus{}, fmt.Errorf("the engine has no model by this name")
		}
		return ModelsStatus{}, fmt.Errorf("the engine could not remove the model")
	}
	d.models.mu.Lock()
	defer d.models.mu.Unlock()
	return d.modelsStatusLocked(), nil
}

// StartEngine starts the local model engine on its own, so a model can be downloaded and chatted with
// without memory or knowledge. Poll ModelsStatus for its end. Stopping it stays with the modules that
// need it.
func (d *Domain) StartEngine() (ModelsStatus, error) {
	if d.models == nil {
		return ModelsStatus{}, fmt.Errorf("local models are unavailable")
	}
	m, _ := Lookup("embeddings")
	if _, err := d.set(m, true); err != nil {
		return ModelsStatus{}, err
	}
	d.models.mu.Lock()
	defer d.models.mu.Unlock()
	return d.modelsStatusLocked(), nil
}

// SameModel says whether two names are one model: a name without a tag means its "latest" tag.
func SameModel(a, b string) bool {
	return withTag(a) == withTag(b)
}

func withTag(name string) string {
	if strings.Contains(name[strings.LastIndex(name, "/")+1:], ":") {
		return name
	}
	return name + ":latest"
}

// engineService is the container of the local model engine, the internal module "embeddings".
const engineService = "dokja-ollama"

func (d *Domain) engineRunning(ctx context.Context) error {
	containers, err := d.backend.Status(ctx)
	if err != nil {
		return fmt.Errorf("cannot read the modules' state")
	}
	if !containers[engineService].Running {
		return fmt.Errorf("the local model engine is stopped")
	}
	return nil
}

// pullFailureDetail is the engine's reason for a failed download, when it gave one.
func pullFailureDetail(err error) string {
	var failure PullFailure
	if errors.As(err, &failure) {
		return failure.Detail
	}
	return ""
}
