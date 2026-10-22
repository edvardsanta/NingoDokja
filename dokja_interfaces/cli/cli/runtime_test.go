package cli

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimeRootIsFoundFromAnyDirectoryOfTheCheckout(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "docker-compose.lite.yml"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	inner := filepath.Join(root, "dokja_interfaces", "cli")
	if err := os.MkdirAll(inner, 0o700); err != nil {
		t.Fatal(err)
	}
	got, err := RuntimeRoot(inner)
	want, _ := filepath.EvalSymlinks(root)
	if err != nil || got != want {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := RuntimeRoot(t.TempDir()); err == nil {
		t.Fatal("a directory outside a checkout must not be a runtime root")
	}
}

func TestEveryInterfaceUsesTheSameStateDirectory(t *testing.T) {
	env := func(values map[string]string) func(string) string {
		return func(key string) string { return values[key] }
	}
	id := RuntimeProjectID("/checkout")
	got, err := RuntimeStateDir("/checkout", env(map[string]string{"XDG_STATE_HOME": "/state", "HOME": "/home/user"}))
	if err != nil || got != "/state/dokja/runtime-"+id {
		t.Fatalf("got %q, %v", got, err)
	}
	got, err = RuntimeStateDir("/checkout", env(map[string]string{"XDG_STATE_HOME": "relative", "HOME": "/home/user"}))
	if err != nil || got != "/home/user/.local/state/dokja/runtime-"+id {
		t.Fatalf("a relative XDG_STATE_HOME must be ignored, got %q, %v", got, err)
	}
	if _, err := RuntimeStateDir("/checkout", env(map[string]string{})); err == nil {
		t.Fatal("no home must be an error")
	}
}

// fakeDaemon answers on a Unix socket the way the runtime controller does.
func fakeDaemon(t *testing.T, root string, answer func(event map[string]any) map[string]any) *RuntimeClient {
	t.Helper()
	// Unix socket paths are short; the test's own temporary directory can be too long.
	state, err := os.MkdirTemp("", "dokja-rt")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(state) })
	listener, err := net.Listen("unix", filepath.Join(state, "control.sock"))
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var event map[string]any
		json.NewDecoder(r.Body).Decode(&event)
		json.NewEncoder(w).Encode(answer(event))
	})}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	return &RuntimeClient{Root: root, StateDir: state, Source: "tui"}
}

func TestRuntimeRequestsCarryTheInterfaceAndKeepUnknownModules(t *testing.T) {
	var seen map[string]any
	client := fakeDaemon(t, "/checkout", func(event map[string]any) map[string]any {
		seen = event
		return map[string]any{"version": 1, "project_id": RuntimeProjectID("/checkout"), "status": "ok", "result": map[string]any{
			"domain": "runtime", "result": map[string]any{"busy": true, "modules": []any{
				map[string]any{"name": "memory", "state": "starting", "dependencies": []any{"orchestrator"}},
				map[string]any{"name": "future_module", "state": "stopped", "dependencies": []any{}},
			}},
		}}
	})
	snapshot, err := client.Request(context.Background(), "runtime.set", map[string]any{"module": "memory", "enabled": true})
	if err != nil {
		t.Fatal(err)
	}
	if seen["source"] != "tui" || seen["type"] != "runtime.set" {
		t.Fatal(seen)
	}
	if !snapshot.Busy || len(snapshot.Modules) != 2 || snapshot.Modules[1].Name != "future_module" || !snapshot.Modules[1].Stopped() {
		t.Fatalf("%+v", snapshot)
	}
}

func TestRuntimeReplyFromAnotherCheckoutIsRejected(t *testing.T) {
	client := fakeDaemon(t, "/checkout", func(map[string]any) map[string]any {
		return map[string]any{"version": 1, "project_id": RuntimeProjectID("/other"), "status": "ok", "result": map[string]any{"result": map[string]any{"modules": []any{}}}}
	})
	if _, err := client.Request(context.Background(), "runtime.modules", nil); err == nil {
		t.Fatal("a reply for another checkout must be rejected")
	}
}

func TestRuntimeErrorsAndUnknownActionsSurface(t *testing.T) {
	client := fakeDaemon(t, "/checkout", func(map[string]any) map[string]any {
		return map[string]any{"version": 1, "project_id": RuntimeProjectID("/checkout"), "status": "error", "error": "unknown module"}
	})
	if _, err := client.Request(context.Background(), "runtime.set", map[string]any{"module": "nope", "enabled": true}); err == nil || err.Error() != "unknown module" {
		t.Fatal(err)
	}
	if _, err := client.Request(context.Background(), "services.set", nil); err == nil {
		t.Fatal("only runtime actions go to the controller")
	}
}

func TestNoBinaryMeansAClearErrorInsteadOfALaunch(t *testing.T) {
	state, _ := os.MkdirTemp("", "dokja-rt")
	defer os.RemoveAll(state)
	client := &RuntimeClient{Root: "/checkout", StateDir: state, Source: "cli"}
	if _, err := client.Request(context.Background(), "runtime.modules", nil); err == nil {
		t.Fatal("expected an error")
	}
}

func TestModuleFailureMakesTheCommandFail(t *testing.T) {
	snapshot := RuntimeSnapshot{Modules: []RuntimeModule{{Name: "memory", State: "failed", Error: "model missing"}, {Name: "book", State: "ready"}}}
	if moduleFailure(snapshot, "memory") == nil || moduleFailure(snapshot, "book") != nil {
		t.Fatal("only the failed module fails the command")
	}
}
