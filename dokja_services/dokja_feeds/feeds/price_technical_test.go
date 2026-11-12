//go:build unix

package feeds

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
)

// A price resolver reads its request on stdin and answers with observations; the host only
// selects it and passes the input and the result through, unchanged.
func TestTechnicalEvidenceProviderUsesBoundedPluginTransport(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is not installed")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "prices")
	writeFile(t, filepath.Join(dir, "plugin.json"), `{"plugin_api": 1, "id": "prices", "version": "1", "runtime": "subprocess",
 "provides": ["price.technical.v1"], "enabled": true, "command": ["python3", "-I", "-B", "run.py"],
 "limits": {"timeout_ms": 5000, "output_bytes": 1048576}}`, 0o644)
	writeFile(t, filepath.Join(dir, "run.py"), `import json, sys
request = json.load(sys.stdin)
products = request["payload"]["products"]
print(json.dumps({"result": {"operation": request["operation"], "action": request["action"],
  "evidence": [{"product_key": p["product"]} for p in products]}}))
`, 0o644)
	service := New(Config{PluginsDir: root, Limits: DefaultLimits()}, Options{})

	got, err := service.Dispatch(context.Background(), "feeds.invoke", map[string]any{
		"capability": CapabilityPriceTechnical, "action": "collect",
		"input": map[string]any{"products": []any{map[string]any{"product": "example-gpu", "title": "RX 7600"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got["operation"] != CapabilityPriceTechnical || got["action"] != "collect" {
		t.Fatalf("result %v", got)
	}
	evidence, _ := got["evidence"].([]any)
	if len(evidence) != 1 || evidence[0].(map[string]any)["product_key"] != "example-gpu" {
		t.Fatalf("observations %v", got["evidence"])
	}

	if _, err := service.Dispatch(context.Background(), "feeds.invoke", map[string]any{
		"capability": CapabilityPriceTechnical, "action": "../escape",
	}); err == nil {
		t.Fatal("an unbounded action was accepted")
	}
}

func TestTechnicalCollectorExample(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is unavailable")
	}
	command := exec.Command("python3", "-m", "unittest", "discover", "-s", filepath.Join("..", "plugins.example", "technical-benchmarks"), "-p", "test_*.py")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("collector tests: %v\n%s", err, output)
	}
}
