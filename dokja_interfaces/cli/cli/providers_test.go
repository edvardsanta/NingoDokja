package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestProviderInventoryUsesSharedReadRequestAndHonestStates(t *testing.T) {
	var seen map[string]any
	client := fakeDaemon(t, "/checkout", func(event map[string]any) map[string]any {
		seen = event
		return map[string]any{"version": 1, "project_id": RuntimeProjectID("/checkout"), "status": "ok", "result": map[string]any{"domain": "runtime", "result": map[string]any{"providers": []any{
			map[string]any{"id": "codex", "name": "Codex", "state": "installed", "model_discovery": "unsupported", "inference_status": "not_tested"},
			map[string]any{"id": "ollama", "name": "Ollama", "state": "reachable", "models": []string{"fixture/model:latest"}, "model_discovery": "listed", "inference_status": "not_tested"},
		}}}}
	})
	view, err := client.Providers(t.Context())
	if err != nil || seen["type"] != "runtime.providers" {
		t.Fatal(seen, err)
	}
	var out bytes.Buffer
	printProviders(&out, view)
	for _, want := range []string{"Codex: installed", "fixture/model:latest", "have not been tested", "Model catalog unavailable"} {
		if !strings.Contains(out.String(), want) {
			t.Fatal(out.String())
		}
	}
}
