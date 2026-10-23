package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func providerApp(t *testing.T, revision uint64) (string, *[]map[string]any) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "docker-compose.lite.yml"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	base, err := os.MkdirTemp("", "dokja-prov-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(base) })
	t.Setenv("XDG_STATE_HOME", base)
	markers := containerMarkers
	containerMarkers = nil
	t.Cleanup(func() { containerMarkers = markers })
	writes := []map[string]any{}
	client := fakeDaemon(t, root, func(event map[string]any) map[string]any {
		result := any(map[string]any{"configuration_revision": revision, "providers": []any{map[string]any{"id": "codex", "enabled": false, "declared_models": []string{"fixture/old"}}}})
		if event["type"] == "runtime.providers.configure" {
			payload := event["payload"].(map[string]any)
			writes = append(writes, payload)
			result = map[string]any{"configuration_revision": revision + 1, "provider": "codex"}
		}
		return map[string]any{"version": 1, "project_id": RuntimeProjectID(root), "status": "ok", "result": map[string]any{"domain": "runtime", "result": result}}
	})
	state, err := RuntimeStateDir(root, os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(client.StateDir, "control.sock"), filepath.Join(state, "control.sock")); err != nil {
		t.Fatal(err)
	}
	return root, &writes
}
func TestConfigureProviderCommandPreservesUnchangedFields(t *testing.T) {
	root, writes := providerApp(t, 7)
	err := NewApp().Run(t.Context(), []string{"dokja-cli", "providers", "configure", "codex", "--runtime-root", root, "--models=fixture/one, fixture/two", "--yes"})
	if err != nil || len(*writes) != 1 {
		t.Fatal(err, *writes)
	}
	payload := (*writes)[0]
	if payload["configuration_revision"] != float64(7) || payload["enabled"] != false || len(payload["models"].([]any)) != 2 {
		t.Fatal(payload)
	}
}
func TestConfigureProviderCanClearDeclarationsAndRefusesOldController(t *testing.T) {
	root, writes := providerApp(t, 2)
	if err := NewApp().Run(t.Context(), []string{"dokja-cli", "providers", "configure", "codex", "--runtime-root", root, "--models=", "--enabled=true", "--yes"}); err != nil {
		t.Fatal(err)
	}
	if len(*writes) != 1 || (*writes)[0]["enabled"] != true || len((*writes)[0]["models"].([]any)) != 0 {
		t.Fatal(*writes)
	}
	oldRoot, oldWrites := providerApp(t, 0)
	if err := NewApp().Run(t.Context(), []string{"dokja-cli", "providers", "configure", "codex", "--runtime-root", oldRoot, "--enabled=false", "--yes"}); err == nil || len(*oldWrites) != 0 {
		t.Fatal(err, *oldWrites)
	}
}
func TestProviderSaveConflictIsNotRetried(t *testing.T) {
	calls := 0
	client := fakeDaemon(t, "/checkout", func(map[string]any) map[string]any {
		calls++
		return map[string]any{"version": 1, "project_id": RuntimeProjectID("/checkout"), "status": "error", "error": "provider configuration changed; reload before saving"}
	})
	if _, err := client.ConfigureProvider(t.Context(), 1, ProviderRegistration{ID: "codex", Enabled: false}); err == nil || calls != 1 {
		t.Fatal(err, calls)
	}
}
