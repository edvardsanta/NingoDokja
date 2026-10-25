//go:build unix

package feeds

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInvokeSelectsOnlyAnEnabledProviderAndRejectsAmbiguity(t *testing.T) {
	root := t.TempDir()
	for _, id := range []string{"library", "alternate"} {
		newPlugin(t, root, id, v1(map[string]any{"id": id, "enabled": true, "formats": nil, "provides": []string{CapabilityMediaLibrary}}))
	}
	runner := &inputRunner{reply: func(Plugin) ([]byte, error) { return []byte(`{"result":{"total":7}}`), nil }}
	s := extractService(root, runner)
	payload := map[string]any{"capability": CapabilityMediaLibrary, "action": "list", "input": map[string]any{"scope": "unsent"}}
	if _, err := s.Dispatch(context.Background(), "feeds.invoke", payload); err == nil || !strings.HasPrefix(err.Error(), "ambiguous:") {
		t.Fatal(err)
	}
	payload["plugin"] = "library"
	result, err := s.Dispatch(context.Background(), "feeds.invoke", payload)
	if err != nil || result["total"] != float64(7) {
		t.Fatal(result, err)
	}
	var request map[string]any
	if err := json.Unmarshal(runner.inputs["library"], &request); err != nil {
		t.Fatal(err)
	}
	if request["operation"] != CapabilityMediaLibrary || request["action"] != "list" {
		t.Fatal(request)
	}
	payload["capability"] = CapabilitySourceCollect
	if _, err = s.Dispatch(context.Background(), "feeds.invoke", payload); err == nil {
		t.Fatal("scheduled collectors must not be invoked as arbitrary processors")
	}
}

func TestInvokeReportsBadProviderOutputWithoutDiscardingSourceState(t *testing.T) {
	root := t.TempDir()
	newPlugin(t, root, "processor", v1(map[string]any{"id": "processor", "enabled": true, "formats": nil, "provides": []string{CapabilityDocumentProcess}}))
	runner := &inputRunner{reply: func(Plugin) ([]byte, error) { return []byte(`{"result":[]}`), nil }}
	s := extractService(root, runner)
	if _, err := s.Dispatch(context.Background(), "feeds.invoke", map[string]any{"capability": CapabilityDocumentProcess, "action": "classify"}); err == nil {
		t.Fatal("invalid result accepted")
	}
	status := s.status()
	providers := status["providers"].([]map[string]any)
	if len(providers) != 1 || providers[0]["state"] != "failed" || status["failed"] != 0 || status["provider_failed"] != 1 {
		t.Fatal(status)
	}
}

func TestBundledMediaLibraryKeepsReceiptsAcrossHostRestarts(t *testing.T) {
	db := filepath.Join(t.TempDir(), "media.db")
	root, err := filepath.Abs("../plugins.builtin")
	if err != nil {
		t.Fatal(err)
	}
	newHost := func() *Service {
		return New(Config{BuiltinPluginsDir: root, Limits: DefaultLimits()}, Options{Lookup: func(key string) (string, bool) {
			if key == "FEEDS_MEDIA_DB_FILE" {
				return db, true
			}
			return os.LookupEnv(key)
		}})
	}
	invoke := func(s *Service, action string, input map[string]any) map[string]any {
		t.Helper()
		result, err := s.Dispatch(context.Background(), "feeds.invoke", map[string]any{"capability": CapabilityMediaLibrary, "action": action, "input": input})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	s := newHost()
	items := []any{map[string]any{"kind": "media", "url": "https://images.example/a.png", "title": "A"}, map[string]any{"kind": "article", "url": "https://example.com/article", "title": "Article"}}
	if r := invoke(s, "ingest", map[string]any{"items": items}); r["added"] != float64(1) {
		t.Fatal(r)
	}
	// Fetch is now a read: neither a repeated fetch nor a host restart confirms delivery.
	for i := 0; i < 2; i++ {
		if r := invoke(s, "fetch", nil); r["count"] != float64(1) {
			t.Fatal(r)
		}
		s = newHost()
	}
	if r := invoke(s, "status", nil); r["unsent_count"] != float64(1) || r["sent_count"] != float64(0) {
		t.Fatal(r)
	}
	if r := invoke(s, "mark-sent", map[string]any{"url": "https://images.example/a.png"}); r["marked"] != true {
		t.Fatal(r)
	}
	s = newHost()
	if r := invoke(s, "ingest", map[string]any{"items": items}); r["added"] != float64(0) {
		t.Fatal(r)
	}
	if r := invoke(s, "list", map[string]any{"scope": "unsent"}); r["total"] != float64(0) {
		t.Fatal(r)
	}
	if r := invoke(s, "list", map[string]any{"scope": "sent"}); r["total"] != float64(1) {
		t.Fatal(r)
	}
	if r := invoke(s, "contains", map[string]any{"url": "https://images.example/' OR 1=1 --"}); r["exists"] != false {
		t.Fatal(r)
	}
}

func TestCustomDisabledProviderReplacesBundledProvider(t *testing.T) {
	builtin, custom := t.TempDir(), t.TempDir()
	config := v1(map[string]any{"id": "library", "enabled": true, "formats": nil, "provides": []string{CapabilityMediaLibrary}})
	newPlugin(t, builtin, "library", config)
	config["enabled"] = false
	newPlugin(t, custom, "library", config)
	s := New(Config{BuiltinPluginsDir: builtin, PluginsDir: custom, Limits: testLimits()}, Options{})
	if _, err := s.Dispatch(context.Background(), "feeds.invoke", map[string]any{"capability": CapabilityMediaLibrary, "action": "list"}); err == nil || !strings.HasPrefix(err.Error(), "unavailable:") {
		t.Fatal(err)
	}
}

func TestRefreshWaitingForAnActiveCollectorHonorsCancellation(t *testing.T) {
	root := t.TempDir()
	newPlugin(t, root, "source", map[string]any{"enabled": true})
	started, release := make(chan struct{}), make(chan struct{})
	runner := newFakeRunner(func(ctx context.Context, _ Plugin) ([]byte, error) {
		close(started)
		<-release
		return itemsOutput("cached", 1), nil
	})
	s := extractService(root, runner)
	done := make(chan struct{})
	go func() { defer close(done); _, _ = s.Dispatch(context.Background(), "feeds.refresh", nil) }()
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Dispatch(ctx, "feeds.refresh", nil); err == nil {
		t.Fatal("cancelled refresh waited for active collector")
	}
	close(release)
	<-done
	if runner.called("source") != 1 {
		t.Fatal("cancelled refresh ran another collection")
	}
}
