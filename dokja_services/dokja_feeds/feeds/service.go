//go:build unix

package feeds

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"
)

// Options are the parts of the service a test replaces.
type Options struct {
	Runner Runner
	Lookup func(string) (string, bool)
	Now    func() time.Time
	Logger *log.Logger
}

// entry is one directory of the plugins directory and what the service knows about it.
type entry struct {
	dir     string
	plugin  *Plugin // nil when the manifest is not usable
	invalid string  // why, when plugin is nil

	running bool
	ran     bool
	lastRun time.Time
	lastOK  time.Time
	lastErr string
	skipped int
	items   []Item
}

func (e *entry) state() string {
	switch {
	case e.plugin == nil:
		return "invalid"
	case !e.plugin.Enabled:
		return "disabled"
	case !e.ran:
		return "pending"
	case e.lastErr != "":
		return "failed"
	}
	return "ok"
}

// Service follows the plugins the owner put in the plugins directory. Each enabled plugin
// runs on its own schedule and its last good result is kept in memory; a request only reads
// that memory, so a slow or hung plugin can never delay an answer.
type Service struct {
	config  Config
	runner  Runner
	lookup  func(string) (string, bool)
	now     func() time.Time
	logger  *log.Logger
	dirErr  string
	mu      sync.RWMutex
	entries []*entry
}

// New reads the plugins directory. Nothing runs until Run is called.
func New(config Config, options Options) *Service {
	s := &Service{config: config, runner: options.Runner, lookup: options.Lookup, now: options.Now, logger: options.Logger}
	if s.logger == nil {
		s.logger = log.New(io.Discard, "", 0)
	}
	if s.runner == nil {
		s.runner = ExecRunner{Logger: s.logger}
	}
	if s.lookup == nil {
		s.lookup = os.LookupEnv
	}
	if s.now == nil {
		s.now = time.Now
	}

	found, err := Discover(config.PluginsDir, config.Limits)
	if err != nil {
		s.dirErr = cleanText(err.Error(), 200)
		s.logger.Printf("plugins directory unusable: %s", s.dirErr)
	}
	for _, f := range found {
		e := &entry{dir: f.Dir, plugin: f.Plugin}
		if f.Err != nil {
			e.invalid = cleanText(f.Err.Error(), 300)
			s.logger.Printf("plugin %s is not usable: %s", cleanText(f.Dir, 64), e.invalid)
		}
		s.entries = append(s.entries, e)
	}
	return s
}

// Run starts every enabled plugin and returns when ctx ends and they have stopped.
func (s *Service) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for _, e := range s.entries {
		if e.plugin == nil || !e.plugin.Enabled {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.follow(ctx, e)
		}()
	}
	wg.Wait()
}

func (s *Service) follow(ctx context.Context, e *entry) {
	for {
		s.refresh(ctx, e)
		timer := time.NewTimer(e.plugin.Interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// refresh runs one plugin and records the outcome. A failed run keeps the last good items
// and says why it failed, so a flaky source does not blank the digest.
func (s *Service) refresh(ctx context.Context, e *entry) {
	s.mu.Lock()
	e.running = true
	s.mu.Unlock()

	plugin := *e.plugin
	items, skipped, err := s.collect(ctx, plugin)

	s.mu.Lock()
	defer s.mu.Unlock()
	e.running = false
	if ctx.Err() != nil {
		return // shutting down: a cancelled run is not a failure of the plugin
	}
	e.ran = true
	e.lastRun = s.now()
	if err != nil {
		e.lastErr = cleanText(err.Error(), 200)
		s.logger.Printf("plugin %s failed: %s", plugin.ID, e.lastErr)
		return
	}
	e.items, e.skipped = items, skipped
	e.lastOK, e.lastErr = e.lastRun, ""
	s.logger.Printf("plugin %s ok items=%d skipped=%d", plugin.ID, len(items), skipped)
}

func (s *Service) collect(ctx context.Context, plugin Plugin) ([]Item, int, error) {
	output, err := s.runner.Run(ctx, plugin, s.environment(plugin))
	if err != nil {
		return nil, 0, err
	}
	return parseOutput(output, plugin, s.now())
}

// environment is PATH and the variables the manifest names, taken from the service's own
// environment. A secret reaches a plugin only by being listed there, by name.
func (s *Service) environment(plugin Plugin) []string {
	path, ok := s.lookup("PATH")
	if !ok || path == "" {
		path = "/usr/local/bin:/usr/bin:/bin"
	}
	env := []string{"PATH=" + path}
	for _, name := range plugin.Env {
		if value, ok := s.lookup(name); ok {
			env = append(env, name+"="+value)
		}
	}
	return env
}

// Dispatch answers the two events of the service, both from memory.
func (s *Service) Dispatch(_ context.Context, eventType string, _ map[string]any) (map[string]any, error) {
	switch eventType {
	case "feeds.status":
		return s.status(), nil
	case "feeds.items":
		return s.itemsReply(), nil
	}
	return nil, fmt.Errorf("unsupported feeds event type %q", cleanText(eventType, 64))
}

func (s *Service) status() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()

	plugins := make([]map[string]any, 0, len(s.entries))
	counts := map[string]int{}
	total := 0
	for _, e := range s.entries {
		state := e.state()
		counts[state]++
		total += len(e.items)

		row := map[string]any{
			"state":      state,
			"running":    e.running,
			"items":      len(e.items),
			"skipped":    e.skipped,
			"last_run":   stamp(e.lastRun),
			"last_ok":    stamp(e.lastOK),
			"last_error": e.lastErr,
		}
		if e.plugin == nil {
			row["id"], row["name"], row["enabled"] = cleanText(e.dir, 64), "", false
			row["interval_seconds"], row["last_error"] = 0, e.invalid
		} else {
			row["id"], row["name"], row["enabled"] = e.plugin.ID, e.plugin.Name, e.plugin.Enabled
			row["interval_seconds"] = int(e.plugin.Interval / time.Second)
		}
		plugins = append(plugins, row)
	}
	return map[string]any{
		"configured":      s.config.PluginsDir != "",
		"directory_error": s.dirErr,
		"plugins":         plugins,
		"enabled":         counts["ok"] + counts["failed"] + counts["pending"],
		"ok":              counts["ok"],
		"failed":          counts["failed"],
		"pending":         counts["pending"],
		"disabled":        counts["disabled"],
		"invalid":         counts["invalid"],
		"items":           total,
	}
}

func (s *Service) itemsReply() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()

	items := []map[string]any{}
	total := 0
	var updated time.Time
	for _, e := range s.entries {
		if e.plugin == nil {
			continue
		}
		if e.lastOK.After(updated) {
			updated = e.lastOK
		}
		for _, item := range e.items {
			total++
			if len(items) >= MaxItemsReturned {
				continue
			}
			items = append(items, map[string]any{
				"id":        item.ID,
				"plugin":    e.plugin.ID,
				"title":     item.Title,
				"summary":   item.Summary,
				"url":       item.URL,
				"published": stamp(item.Published),
				"source":    item.Source,
			})
		}
	}
	return map[string]any{"items": items, "total": total, "updated": stamp(updated)}
}

func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
