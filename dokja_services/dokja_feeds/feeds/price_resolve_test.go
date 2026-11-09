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
func TestAPriceResolverIsInvokedWithItsInputAndPassesItsResultBack(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is not installed")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "prices")
	writeFile(t, filepath.Join(dir, "plugin.json"), `{"plugin_api": 1, "id": "prices", "version": "1", "runtime": "subprocess",
 "provides": ["price.resolve.v1"], "enabled": true, "command": ["python3", "-I", "-B", "run.py"],
 "limits": {"timeout_ms": 5000, "output_bytes": 1048576}}`, 0o644)
	writeFile(t, filepath.Join(dir, "run.py"), `import json, sys
request = json.load(sys.stdin)
searches = request["payload"]["searches"]
print(json.dumps({"result": {"operation": request["operation"], "action": request["action"],
  "observations": [{"for": s["category"]} for s in searches]}}))
`, 0o644)
	service := New(Config{PluginsDir: root, Limits: DefaultLimits()}, Options{})

	got, err := service.Dispatch(context.Background(), "feeds.invoke", map[string]any{
		"capability": CapabilityPriceResolve, "action": "search",
		"input": map[string]any{"searches": []any{map[string]any{"category": "ssd sata", "brand": "samsung"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got["operation"] != CapabilityPriceResolve || got["action"] != "search" {
		t.Fatalf("result %v", got)
	}
	observations, _ := got["observations"].([]any)
	if len(observations) != 1 || observations[0].(map[string]any)["for"] != "ssd sata" {
		t.Fatalf("observations %v", got["observations"])
	}

	if _, err := service.Dispatch(context.Background(), "feeds.invoke", map[string]any{
		"capability": CapabilityPriceResolve, "action": "../escape",
	}); err == nil {
		t.Fatal("an unbounded action was accepted")
	}
}
