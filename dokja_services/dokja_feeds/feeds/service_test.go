//go:build unix

package feeds

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeRunner stands in for the processes: what each plugin prints is a function.
type fakeRunner struct {
	mu    sync.Mutex
	calls map[string]int
	run   func(ctx context.Context, plugin Plugin) ([]byte, error)
}

func newFakeRunner(run func(ctx context.Context, plugin Plugin) ([]byte, error)) *fakeRunner {
	return &fakeRunner{calls: map[string]int{}, run: run}
}

func (f *fakeRunner) Run(ctx context.Context, plugin Plugin, _ []string) ([]byte, error) {
	f.mu.Lock()
	f.calls[plugin.ID]++
	f.mu.Unlock()
	return f.run(ctx, plugin)
}

func (f *fakeRunner) called(id string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[id]
}

func itemsOutput(prefix string, count int) []byte {
	items := make([]map[string]string, count)
	for i := range items {
		name := fmt.Sprintf("%s-%d", prefix, i+1)
		items[i] = map[string]string{"id": name, "title": "Title " + name, "published": "2026-10-01T08:00:00Z"}
	}
	body, _ := json.Marshal(map[string]any{"items": items})
	return body
}

func testLimits() Limits {
	limits := DefaultLimits()
	limits.MinInterval = 10 * time.Millisecond
	return limits
}

// startService runs a service over root until the test ends.
func startService(t *testing.T, root string, runner Runner) *Service {
	t.Helper()
	service := New(Config{PluginsDir: root, Limits: testLimits()}, Options{
		Runner: runner,
		Now:    func() time.Time { return testNow },
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		service.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("the service did not stop")
		}
	})
	return service
}

// answer asks the service and fails the test, instead of hanging it, when no answer comes.
func answer(t *testing.T, s *Service, event string) map[string]any {
	t.Helper()
	type result struct {
		reply map[string]any
		err   error
	}
	done := make(chan result, 1)
	go func() {
		reply, err := s.Dispatch(context.Background(), event, nil)
		done <- result{reply, err}
	}()
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatal(r.err)
		}
		return r.reply
	case <-time.After(2 * time.Second):
		t.Fatalf("%s did not answer within 2s", event)
		return nil
	}
}

func statusOf(t *testing.T, s *Service) map[string]any {
	t.Helper()
	return answer(t, s, "feeds.status")
}

func rowOf(t *testing.T, s *Service, id string) map[string]any {
	t.Helper()
	for _, row := range statusOf(t, s)["plugins"].([]map[string]any) {
		if row["id"] == id {
			return row
		}
	}
	t.Fatalf("no plugin %q in the status", id)
	return nil
}

func waitForRow(t *testing.T, s *Service, id string, condition func(map[string]any) bool) map[string]any {
	t.Helper()
	var row map[string]any
	waitUntil(t, 5*time.Second, func() bool {
		row = rowOf(t, s, id)
		return condition(row)
	})
	return row
}

func itemsOf(t *testing.T, s *Service) (items []map[string]any, total int) {
	t.Helper()
	reply := answer(t, s, "feeds.items")
	return reply["items"].([]map[string]any), reply["total"].(int)
}

func titlesOf(items []map[string]any) string {
	var titles []string
	for _, item := range items {
		titles = append(titles, item["title"].(string))
	}
	return strings.Join(titles, ",")
}

func TestStatusListsEveryDirectoryWithItsState(t *testing.T) {
	root := t.TempDir()
	newPlugin(t, root, "alpha", map[string]any{"enabled": true, "interval": "1h"})
	newPlugin(t, root, "beta", nil)
	writeFile(t, filepath.Join(root, "gamma", "plugin.json"), "{", 0o644)

	release := make(chan struct{})
	service := startService(t, root, newFakeRunner(func(ctx context.Context, plugin Plugin) ([]byte, error) {
		select {
		case <-release:
			return itemsOutput("a", 2), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}))

	waitForRow(t, service, "alpha", func(row map[string]any) bool { return row["running"] == true })
	status := statusOf(t, service)
	if status["configured"] != true || status["directory_error"] != "" {
		t.Fatalf("status = %v", status)
	}
	if status["enabled"] != 1 || status["pending"] != 1 || status["disabled"] != 1 || status["invalid"] != 1 || status["ok"] != 0 {
		t.Fatalf("counts = %v", status)
	}
	if row := rowOf(t, service, "alpha"); row["state"] != "pending" || row["enabled"] != true || row["interval_seconds"] != 3600 {
		t.Fatalf("alpha = %v", row)
	}
	if row := rowOf(t, service, "beta"); row["state"] != "disabled" || row["enabled"] != false {
		t.Fatalf("beta = %v", row)
	}
	if row := rowOf(t, service, "gamma"); row["state"] != "invalid" || !strings.Contains(row["last_error"].(string), "not valid") {
		t.Fatalf("gamma = %v", row)
	}

	close(release)
	row := waitForRow(t, service, "alpha", func(row map[string]any) bool { return row["state"] == "ok" })
	if row["items"] != 2 || row["last_ok"] != "2026-10-02T12:00:00Z" || row["last_error"] != "" || row["running"] != false {
		t.Fatalf("alpha = %v", row)
	}
}

func TestADisabledPluginNeverRuns(t *testing.T) {
	root := t.TempDir()
	newPlugin(t, root, "alpha", map[string]any{"enabled": true})
	newPlugin(t, root, "beta", map[string]any{"enabled": false})
	runner := newFakeRunner(func(context.Context, Plugin) ([]byte, error) { return itemsOutput("x", 1), nil })
	service := startService(t, root, runner)

	waitForRow(t, service, "alpha", func(row map[string]any) bool { return row["state"] == "ok" })
	time.Sleep(100 * time.Millisecond)
	if runner.called("beta") != 0 {
		t.Fatal("a disabled plugin was run")
	}
}

func TestItemsAreServedFromTheLastGoodRun(t *testing.T) {
	root := t.TempDir()
	newPlugin(t, root, "alpha", map[string]any{"enabled": true, "interval": "1h", "name": "Alpha"})
	service := startService(t, root, newFakeRunner(func(context.Context, Plugin) ([]byte, error) {
		return []byte(`{"items":[{"id":"1","title":"One","summary":"S","url":"https://example.com/1","published":"2026-10-01T08:00:00Z","source":"Own"},{"id":"2","title":"Two"}]}`), nil
	}))
	waitForRow(t, service, "alpha", func(row map[string]any) bool { return row["state"] == "ok" })

	reply, _ := service.Dispatch(context.Background(), "feeds.items", nil)
	items := reply["items"].([]map[string]any)
	if len(items) != 2 || reply["total"] != 2 || reply["updated"] != "2026-10-02T12:00:00Z" {
		t.Fatalf("reply = %v", reply)
	}
	first := items[0]
	for key, want := range map[string]string{
		"id": "1", "plugin": "alpha", "title": "One", "summary": "S",
		"url": "https://example.com/1", "published": "2026-10-01T08:00:00Z", "source": "Own",
	} {
		if first[key] != want {
			t.Errorf("%s = %v, want %q", key, first[key], want)
		}
	}
	if items[1]["source"] != "Alpha" || items[1]["published"] != "" || items[1]["url"] != "" {
		t.Errorf("second item = %v", items[1])
	}
}

func TestAFailedRunKeepsTheLastGoodItemsAndSaysWhy(t *testing.T) {
	root := t.TempDir()
	newPlugin(t, root, "alpha", map[string]any{"enabled": true, "interval": "20ms"})
	var mode atomic.Int32 // 0 good, 1 failing, 2 good again with other items
	service := startService(t, root, newFakeRunner(func(context.Context, Plugin) ([]byte, error) {
		switch mode.Load() {
		case 1:
			return nil, errors.New("exit status 2")
		case 2:
			return itemsOutput("new", 1), nil
		}
		return itemsOutput("old", 2), nil
	}))

	waitForRow(t, service, "alpha", func(row map[string]any) bool { return row["state"] == "ok" })
	mode.Store(1)
	row := waitForRow(t, service, "alpha", func(row map[string]any) bool { return row["state"] == "failed" })
	if row["last_error"] != "exit status 2" || row["items"] != 2 || row["last_ok"] != "2026-10-02T12:00:00Z" {
		t.Fatalf("row = %v", row)
	}
	if items, _ := itemsOf(t, service); titlesOf(items) != "Title old-1,Title old-2" {
		t.Fatalf("items after a failure = %s", titlesOf(items))
	}

	mode.Store(2)
	row = waitForRow(t, service, "alpha", func(row map[string]any) bool { return row["state"] == "ok" })
	if row["last_error"] != "" || row["items"] != 1 {
		t.Fatalf("row = %v", row)
	}
	if items, _ := itemsOf(t, service); titlesOf(items) != "Title new-1" {
		t.Fatalf("items after recovering = %s", titlesOf(items))
	}
}

func TestAFailingPluginDoesNotHideTheOthers(t *testing.T) {
	root := t.TempDir()
	newPlugin(t, root, "alpha", map[string]any{"enabled": true})
	newPlugin(t, root, "beta", map[string]any{"enabled": true})
	newPlugin(t, root, "gamma", map[string]any{"enabled": true})
	service := startService(t, root, newFakeRunner(func(_ context.Context, plugin Plugin) ([]byte, error) {
		switch plugin.ID {
		case "alpha":
			return nil, errors.New("exit status 1")
		case "beta":
			return []byte(`not json`), nil
		}
		return itemsOutput("g", 2), nil
	}))

	waitForRow(t, service, "gamma", func(row map[string]any) bool { return row["state"] == "ok" })
	waitForRow(t, service, "alpha", func(row map[string]any) bool { return row["state"] == "failed" })
	row := waitForRow(t, service, "beta", func(row map[string]any) bool { return row["state"] == "failed" })
	if row["last_error"] != "output is not valid JSON" {
		t.Fatalf("beta = %v", row)
	}
	items, total := itemsOf(t, service)
	if total != 2 || titlesOf(items) != "Title g-1,Title g-2" {
		t.Fatalf("items = %s total=%d", titlesOf(items), total)
	}
	if status := statusOf(t, service); status["ok"] != 1 || status["failed"] != 2 {
		t.Fatalf("counts = %v", status)
	}
}

func TestAHangingPluginDoesNotDelayAnAnswer(t *testing.T) {
	root := t.TempDir()
	newPlugin(t, root, "hangs", map[string]any{"enabled": true})
	newPlugin(t, root, "works", map[string]any{"enabled": true})
	service := startService(t, root, newFakeRunner(func(ctx context.Context, plugin Plugin) ([]byte, error) {
		if plugin.ID == "hangs" {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return itemsOutput("w", 1), nil
	}))

	waitForRow(t, service, "works", func(row map[string]any) bool { return row["state"] == "ok" })
	waitForRow(t, service, "hangs", func(row map[string]any) bool { return row["running"] == true })

	for _, event := range []string{"feeds.status", "feeds.items"} {
		started := time.Now()
		answer(t, service, event)
		if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
			t.Fatalf("%s took %v while a plugin was hanging", event, elapsed)
		}
	}
	if items, _ := itemsOf(t, service); titlesOf(items) != "Title w-1" {
		t.Fatalf("items = %s", titlesOf(items))
	}
}

func TestStoppingTheServiceIsNotAFailureOfThePlugin(t *testing.T) {
	root := t.TempDir()
	newPlugin(t, root, "alpha", map[string]any{"enabled": true})
	service := New(Config{PluginsDir: root, Limits: testLimits()}, Options{
		Runner: newFakeRunner(func(ctx context.Context, plugin Plugin) ([]byte, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}),
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		service.Run(ctx)
	}()
	waitForRow(t, service, "alpha", func(row map[string]any) bool { return row["running"] == true })

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the service did not stop")
	}
	if row := rowOf(t, service, "alpha"); row["state"] != "pending" || row["last_error"] != "" || row["running"] != false {
		t.Fatalf("alpha = %v", row)
	}
}

func TestOnlyWhatTheManifestNamesReachesAPlugin(t *testing.T) {
	values := map[string]string{"PATH": "/custom/bin", "DECLARED": "yes", "SECRET": "no", "FEEDS_SERVICE_ENDPOINT": "tcp://*:1"}
	lookup := func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
	service := New(Config{}, Options{Lookup: lookup})
	got := service.environment(Plugin{Env: []string{"DECLARED", "NOT_SET"}})
	if strings.Join(got, " ") != "PATH=/custom/bin DECLARED=yes" {
		t.Fatalf("environment = %v", got)
	}

	delete(values, "PATH")
	got = service.environment(Plugin{})
	if len(got) != 1 || !strings.HasPrefix(got[0], "PATH=/usr/local/bin") {
		t.Fatalf("environment without PATH = %v", got)
	}
}

func TestOneReplyIsBoundedHoweverManyItemsThereAre(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"a", "b", "c", "d", "e", "f"} {
		newPlugin(t, root, id, map[string]any{"enabled": true, "interval": "1h", "max_items": 200})
	}
	service := startService(t, root, newFakeRunner(func(_ context.Context, plugin Plugin) ([]byte, error) {
		return itemsOutput(plugin.ID, 200), nil
	}))
	waitUntil(t, 5*time.Second, func() bool { return statusOf(t, service)["ok"] == 6 })

	items, total := itemsOf(t, service)
	if len(items) != MaxItemsReturned || total != 1200 {
		t.Fatalf("len=%d total=%d", len(items), total)
	}
}

func TestOtherEventsAreRefusedAndTheirNameIsCleaned(t *testing.T) {
	service := New(Config{}, Options{})
	_, err := service.Dispatch(context.Background(), "knowledge.search", nil)
	if err == nil || !strings.Contains(err.Error(), "unsupported feeds event type") {
		t.Fatalf("err=%v", err)
	}
	_, err = service.Dispatch(context.Background(), "x\ny"+strings.Repeat("z", 200), nil)
	if err == nil || strings.Contains(err.Error(), "\n") || len(err.Error()) > 120 {
		t.Fatalf("err=%q", err)
	}
}

func TestWithoutPluginsTheServiceStillAnswers(t *testing.T) {
	t.Run("no directory configured", func(t *testing.T) {
		service := New(Config{}, Options{})
		status := statusOf(t, service)
		if status["configured"] != false || status["enabled"] != 0 || status["items"] != 0 || len(status["plugins"].([]map[string]any)) != 0 {
			t.Fatalf("status = %v", status)
		}
		if items, total := itemsOf(t, service); len(items) != 0 || total != 0 {
			t.Fatalf("items=%v total=%d", items, total)
		}
	})

	t.Run("a directory that does not exist", func(t *testing.T) {
		service := New(Config{PluginsDir: filepath.Join(t.TempDir(), "missing")}, Options{})
		status := statusOf(t, service)
		if status["configured"] != true || status["directory_error"] == "" {
			t.Fatalf("status = %v", status)
		}
	})

	t.Run("an empty directory", func(t *testing.T) {
		service := New(Config{PluginsDir: t.TempDir()}, Options{})
		if status := statusOf(t, service); status["configured"] != true || status["directory_error"] != "" || status["enabled"] != 0 {
			t.Fatalf("status = %v", status)
		}
	})
}

// copyExample copies the shipped example plugin and enables it.
func copyExample(t *testing.T, root string) string {
	t.Helper()
	source := filepath.Join("..", "plugins.example", "local-file")
	dest := filepath.Join(root, "local-file")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"plugin.json", "run.sh", "items.example.json"} {
		data, err := os.ReadFile(filepath.Join(source, name))
		if err != nil {
			t.Fatal(err)
		}
		mode := os.FileMode(0o644)
		if name == "run.sh" {
			mode = 0o755
		}
		if name == "plugin.json" {
			var manifest map[string]any
			if err := json.Unmarshal(data, &manifest); err != nil {
				t.Fatal(err)
			}
			manifest["enabled"] = true
			if data, err = json.Marshal(manifest); err != nil {
				t.Fatal(err)
			}
		}
		writeFile(t, filepath.Join(dest, name), string(data), mode)
	}
	return dest
}

func TestTheShippedExamplePluginWorksThroughARealProcess(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	root := t.TempDir()
	copyExample(t, root)
	feedFile, err := filepath.Abs(filepath.Join("..", "plugins.example", "local-file", "items.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	service := New(Config{PluginsDir: root, Limits: DefaultLimits()}, Options{
		Now: func() time.Time { return testNow },
		Lookup: func(name string) (string, bool) {
			switch name {
			case "PATH":
				return os.Getenv("PATH"), true
			case "EXAMPLE_FEED_FILE":
				return feedFile, true
			}
			return "", false
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		service.Run(ctx)
	}()
	t.Cleanup(func() { cancel(); <-done })

	row := waitForRow(t, service, "local-file", func(row map[string]any) bool { return row["state"] != "pending" })
	if row["state"] != "ok" || row["items"] != 3 {
		t.Fatalf("local-file = %v", row)
	}
	items, _ := itemsOf(t, service)
	if titlesOf(items) != "A first example entry,A second example entry,An entry without a date or an address" {
		t.Fatalf("items = %s", titlesOf(items))
	}
	if items[0]["url"] != "https://example.com/entries/1" || items[0]["source"] != "Example source A" {
		t.Fatalf("first = %v", items[0])
	}
}

func TestTheShippedExampleIsDisabledUntilTheOwnerEnablesIt(t *testing.T) {
	found, err := Discover(filepath.Join("..", "plugins.example"), DefaultLimits())
	if err != nil || len(found) != 1 || found[0].Err != nil {
		t.Fatalf("found=%+v err=%v", found, err)
	}
	if found[0].Plugin.Enabled {
		t.Fatal("the example plugin ships enabled")
	}
}
