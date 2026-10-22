package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
func (f *fake) Stop(context.Context, domain.Module) error { return nil }
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
