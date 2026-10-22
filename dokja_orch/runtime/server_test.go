package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	service "read_books/dokja_services/dokja_runtime"
	"sync"
	"testing"
	"time"

	domain "read_books/dokja_domain/dokja_runtime"
)

type fake struct {
	mu     sync.Mutex
	starts int
	gate   chan struct{}
}

func (f *fake) Status(context.Context) (map[string]domain.Container, error) {
	return map[string]domain.Container{}, nil
}
func (f *fake) Start(context.Context, domain.Module) error {
	f.mu.Lock()
	f.starts++
	f.mu.Unlock()
	return nil
}
func (f *fake) Stop(context.Context, domain.Module) error     { return nil }
func (f *fake) Recreate(context.Context, domain.Module) error { return nil }
func (f *fake) Ready(ctx context.Context, _ domain.Module) error {
	select {
	case <-f.gate:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func TestBootstrapRoutesRequestsWithoutStartingTheMainStack(t *testing.T) {
	f := &fake{gate: make(chan struct{})}
	defer close(f.gate)
	handler := Handler(domain.New(f, domain.Journal{}, nil), "fixture")
	ask := func(kind string, payload map[string]any) map[string]any {
		raw, _ := json.Marshal(Event{Type: kind, Source: "desktop", Payload: payload})
		req := httptest.NewRequest(http.MethodPost, "/request", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		var reply map[string]any
		json.Unmarshal(w.Body.Bytes(), &reply)
		return reply
	}
	if got := ask("runtime.modules", nil); got["status"] != "ok" || got["project_id"] != "fixture" {
		t.Fatal(got)
	}
	f.mu.Lock()
	count := f.starts
	f.mu.Unlock()
	if count != 0 {
		t.Fatal("listing must not start anything")
	}
	for _, kind := range []string{"services.set", "memory.forget", "arbitrary.command"} {
		if ask(kind, nil)["status"] != "error" {
			t.Fatal(kind)
		}
	}
	if ask("runtime.set", map[string]any{"module": "memory", "enabled": "yes"})["status"] != "error" {
		t.Fatal("enabled must be boolean")
	}
	before := time.Now()
	reply := ask("runtime.set", map[string]any{"module": "memory", "enabled": true})
	if reply["status"] != "ok" || time.Since(before) > time.Second {
		t.Fatal("daemon must acknowledge before background work finishes")
	}
}
func TestBrowserOriginsAndOversizedPayloadsAreRejected(t *testing.T) {
	f := &fake{}
	handler := Handler(domain.New(f, domain.Journal{}, nil), "fixture")
	for _, origin := range []string{"http://localhost", "https://example.com"} {
		req := httptest.NewRequest(http.MethodPost, "/request", bytes.NewBufferString(`{"source":"desktop","type":"runtime.modules"}`))
		req.Header.Set("Origin", origin)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code == 200 {
			t.Fatal("browser origin accepted")
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/request", bytes.NewBuffer(append([]byte(`{"source":"desktop","type":"runtime.modules","context":{"long":"`), bytes.Repeat([]byte("x"), 9000)...)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code == 200 {
		t.Fatal("oversized request accepted")
	}
}
func TestEveryLocalInterfaceGetsTheSameControl(t *testing.T) {
	handler := Handler(domain.New(&fake{}, domain.Journal{}, nil), "fixture")
	status := func(source string) int {
		req := httptest.NewRequest(http.MethodPost, "/request", bytes.NewBufferString(`{"source":"`+source+`","type":"runtime.modules"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w.Code
	}
	for _, source := range []string{"desktop", "cli", "tui"} {
		if status(source) != http.StatusOK {
			t.Fatal(source)
		}
	}
	for _, source := range []string{"", "discord", "whatsapp"} {
		if status(source) == http.StatusOK {
			t.Fatal(source)
		}
	}
}
func TestSettingsAreRoutedAndRefusedValuesAnswerAnError(t *testing.T) {
	handler := Handler(domain.New(&fake{}, domain.Journal{}, nil), "fixture")
	ask := func(kind string, payload map[string]any) map[string]any {
		raw, _ := json.Marshal(Event{Type: kind, Source: "cli", Payload: payload})
		req := httptest.NewRequest(http.MethodPost, "/request", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		var reply map[string]any
		json.Unmarshal(w.Body.Bytes(), &reply)
		return reply
	}
	if ask("runtime.settings", nil)["status"] != "ok" {
		t.Fatal("settings are listed")
	}
	for _, payload := range []map[string]any{nil, {"values": map[string]any{}}, {"values": map[string]any{"discord.direct_messages": 1}}, {"values": map[string]any{"discord.direct_messages": "open"}}} {
		if ask("runtime.settings.set", payload)["status"] != "error" {
			t.Fatalf("%v must be refused (this stack has no settings store)", payload)
		}
	}
}

func TestDatabaseSettingsAreSharedByEveryInterfaceAndSurviveRestart(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DOKJA_DB_FILE", "")
	t.Setenv("DISCORD_DM_POLICY", "open")
	envPath := filepath.Join(root, ".env")
	legacy := "DISCORD_DM_POLICY=open\nDISCORD_BOT_TOKEN=fixture-token\n"
	if err := os.WriteFile(envPath, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	settings, err := service.OpenDatabaseSettings(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { settings.Close() }()
	backend := &fake{}
	d := domain.New(backend, domain.Journal{}, nil).WithSettings(settings)
	handler := Handler(d, "fixture")
	ask := func(source, kind string, payload map[string]any) domain.SettingsView {
		t.Helper()
		raw, _ := json.Marshal(Event{Source: source, Type: kind, Payload: payload})
		req := httptest.NewRequest(http.MethodPost, "/request", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		var reply struct {
			Status string `json:"status"`
			Error  string `json:"error"`
			Result struct {
				Result domain.SettingsView `json:"result"`
			} `json:"result"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil || reply.Status != "ok" {
			t.Fatal(w.Body.String(), err)
		}
		if bytes.Contains(w.Body.Bytes(), []byte("fixture-token")) {
			t.Fatal("secret exposed")
		}
		return reply.Result.Result
	}
	ask("cli", "runtime.settings.set", map[string]any{"values": map[string]string{"discord.direct_messages": "closed"}})
	deadline := time.Now().Add(time.Second)
	for d.Settings().Busy {
		if time.Now().After(deadline) {
			t.Fatal("settings did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	if err := d.Settings().Error; err != "" {
		t.Fatal(err)
	}
	// Restart the persistence adapter and router, keeping the same database file.
	if err := settings.Close(); err != nil {
		t.Fatal(err)
	}
	settings, err = service.OpenDatabaseSettings(root)
	if err != nil {
		t.Fatal(err)
	}
	d = domain.New(backend, domain.Journal{}, nil).WithSettings(settings)
	handler = Handler(d, "fixture")
	for _, source := range []string{"cli", "tui", "desktop"} {
		view := ask(source, "runtime.settings", nil)
		found := false
		for _, row := range view.Settings {
			if row.ID == "discord.direct_messages" {
				found = row.Value == "closed"
			}
		}
		if !found || view.Error != "" {
			t.Fatal(source, view)
		}
	}
	if raw, _ := os.ReadFile(envPath); string(raw) != legacy {
		t.Fatal("legacy configuration changed")
	}
	if backend.starts != 0 {
		t.Fatal("settings started stopped modules")
	}
}

func TestProviderInventoryIsReadOnlyAndSharedByLocalInterfaces(t *testing.T) {
	backend := &fake{}
	handler := Handler(domain.New(backend, domain.Journal{}, nil), "fixture")
	for _, source := range []string{"cli", "tui", "desktop"} {
		raw, _ := json.Marshal(Event{Source: source, Type: "runtime.providers", Payload: map[string]any{"command": "ignored"}})
		req := httptest.NewRequest(http.MethodPost, "/request", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		var reply struct {
			Status string
			Result struct{ Result domain.ProvidersView }
		}
		if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil || reply.Status != "ok" || len(reply.Result.Result.Providers) != 3 {
			t.Fatal(w.Body.String(), err)
		}
		for _, row := range reply.Result.Result.Providers {
			if row.InferenceStatus != "not_tested" {
				t.Fatal(row)
			}
		}
	}
	if backend.starts != 0 {
		t.Fatal("inventory started modules")
	}
}
