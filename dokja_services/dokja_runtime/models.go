package runtime

import (
	"context"
	"fmt"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	domain "read_books/dokja_domain/dokja_runtime"
)

// OllamaModels pulls and removes models in the stack's Ollama container. Model names reach the
// engine as one argument of a fixed script, never as shell text, and the domain has already checked
// them against a strict pattern that no flag passes.
type OllamaModels struct {
	Compose *Compose
	// Environment is the stack's settings projection, where the embedding model is named.
	Environment func([]string) ([]string, error)
}

const defaultEmbeddingModel = "bge-m3"

var embeddingName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)

func (o OllamaModels) EmbeddingModel() string {
	if o.Environment == nil {
		return defaultEmbeddingModel
	}
	values, err := o.Environment(os.Environ())
	if err != nil {
		return defaultEmbeddingModel
	}
	model := defaultEmbeddingModel
	for _, value := range values {
		if name, ok := strings.CutPrefix(value, "DOKJA_EMBED_MODEL="); ok && embeddingName.MatchString(name) {
			model = name
		}
	}
	return model
}

// The pull runs under a shell in the container that records its pid, so a cancelled pull can be
// stopped there too: ending `docker compose exec` leaves the process it started running.
const (
	pullScript = `echo $$ > /tmp/dokja-model-pull.pid; exec ollama pull "$1"`
	stopScript = `kill "$(cat /tmp/dokja-model-pull.pid 2>/dev/null)" 2>/dev/null; rm -f /tmp/dokja-model-pull.pid`
)

func (o OllamaModels) Pull(ctx context.Context, model string, progress func(domain.PullProgress)) error {
	if o.Compose == nil || o.Compose.Stream == nil {
		return fmt.Errorf("the local model engine is unavailable")
	}
	reader := &pullReader{report: progress, layers: map[string]layer{}}
	err := o.Compose.Stream(ctx, reader, "exec", "-T", "dokja-ollama", "sh", "-c", pullScript, "dokja-pull", model)
	if ctx.Err() != nil {
		stop, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		o.Compose.Run(stop, "exec", "-T", "dokja-ollama", "sh", "-c", stopScript)
		return ctx.Err()
	}
	if reader.notFound() {
		return domain.ErrModelNotFound
	}
	// The command's exit status says whether the model is there; the drawn text is only for progress.
	if err != nil {
		return fmt.Errorf("model pull failed")
	}
	return nil
}

func (o OllamaModels) Remove(ctx context.Context, model string) error {
	if o.Compose == nil {
		return fmt.Errorf("the local model engine is unavailable")
	}
	reader := &pullReader{layers: map[string]layer{}}
	err := o.Compose.Stream(ctx, reader, "exec", "-T", "dokja-ollama", "ollama", "rm", model)
	if reader.notFound() {
		return domain.ErrModelNotFound
	}
	return err
}

type layer struct{ done, total float64 }

// pullReader reads the engine's progress as it is drawn: lines redrawn in place with carriage
// returns and cursor escapes. Only phases and byte counts are taken from it; its text is never
// passed on.
type pullReader struct {
	mu      sync.Mutex
	report  func(domain.PullProgress)
	pending string
	layers  map[string]layer
	missing bool
}

var (
	terminalEscape = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
	layerLine      = regexp.MustCompile(`^pulling ([0-9a-f]{12})\S*\s+([0-9]{1,3})%.*?([0-9]+(?:\.[0-9]+)?) ?([KMGT]?B)(?:\s*/\s*([0-9]+(?:\.[0-9]+)?) ?([KMGT]?B))?`)
)

// The longest piece kept while waiting for the end of a line; progress lines are short.
const maxPending = 4096

// Without a terminal the CLI redraws in place with escapes and no line breaks: "ESC[1G" takes it back
// to the first column. So every escape, carriage return and line break ends a piece of text. A write
// may stop in the middle of an escape, which then waits for the next one.
func (r *pullReader) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pending += string(p)
	for {
		end := strings.IndexAny(r.pending, "\r\n\x1b")
		if end < 0 {
			break
		}
		r.read(strings.TrimSpace(r.pending[:end]))
		rest := r.pending[end:]
		if rest[0] != 0x1b {
			r.pending = rest[1:]
			continue
		}
		escape := terminalEscape.FindStringIndex(rest)
		if escape == nil || escape[0] != 0 {
			if len(rest) < 32 {
				r.pending = rest
				break
			}
			escape = []int{0, 1}
		}
		r.pending = rest[escape[1]:]
	}
	if len(r.pending) > maxPending {
		r.pending = r.pending[len(r.pending)-maxPending:]
	}
	return len(p), nil
}

func (r *pullReader) read(line string) {
	lower := strings.ToLower(line)
	switch {
	case line == "":
	case strings.HasPrefix(lower, "error"):
		if strings.Contains(lower, "file does not exist") || strings.Contains(lower, "not found") {
			r.missing = true
		}
	case strings.HasPrefix(lower, "pulling manifest"):
		r.emit("starting", -1)
	case strings.HasPrefix(lower, "verifying"):
		r.emit("verifying", -1)
	case strings.HasPrefix(lower, "writing manifest"), strings.HasPrefix(lower, "removing"):
		r.emit("writing", -1)
	default:
		if m := layerLine.FindStringSubmatch(line); m != nil {
			r.layer(m)
		}
	}
}

// A layer line gives its share and either "done/total" while it downloads or only its total once
// done. The share reported is over every layer seen, weighted by size.
func (r *pullReader) layer(m []string) {
	percent, _ := strconv.Atoi(m[2])
	first := byteCount(m[3], m[4])
	l := layer{done: first, total: first}
	if m[5] != "" {
		l.total = byteCount(m[5], m[6])
	} else if percent < 100 {
		l.done = l.total * float64(percent) / 100
	}
	r.layers[m[1]] = l
	var done, total float64
	for _, each := range r.layers {
		done, total = done+each.done, total+each.total
	}
	if total <= 0 {
		r.emit("downloading", percent)
		return
	}
	r.emit("downloading", int(math.Min(100, math.Floor(done/total*100))))
}

func (r *pullReader) emit(phase string, percent int) {
	if r.report != nil {
		r.report(domain.PullProgress{Phase: phase, Percent: percent})
	}
}

func (r *pullReader) notFound() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.missing
}

func byteCount(value, unit string) float64 {
	n, _ := strconv.ParseFloat(value, 64)
	return n * map[string]float64{"B": 1, "KB": 1e3, "MB": 1e6, "GB": 1e9, "TB": 1e12}[unit]
}
