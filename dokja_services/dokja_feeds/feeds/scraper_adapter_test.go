//go:build unix

package feeds

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestScraperAdapterFeedsMediaToTheSameCollectorContract(t *testing.T) {
	root := t.TempDir()
	newPlugin(t, root, "source", v1(map[string]any{"id": "source", "enabled": true, "formats": nil, "provides": []string{CapabilitySourceCollect}, "max_items": 2, "interval": "1m", "command": []string{"python3", "-I", "-B", "run.py"}}))
	dir := filepath.Join(root, "source")
	script, err := os.ReadFile("../plugins.example/scraper/run.py")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "run.py"), script, 0600); err != nil {
		t.Fatal(err)
	}
	config, _ := json.Marshal(map[string]any{"module_path": dir, "factory": "custom:Scraper", "kind": "media", "options": map[string]any{"label": "Configured title"}})
	if err = os.WriteFile(filepath.Join(dir, "source.json"), config, 0600); err != nil {
		t.Fatal(err)
	}
	module := `print("a dependency printed a log")
class Scraper:
    def __init__(self, label): self.label = label
    def scrape(self, max_items=50):
        return [{"title": self.label, "url": "https://images.example/media.png", "tags": ["first", "second"]}]
`
	if err = os.WriteFile(filepath.Join(dir, "custom.py"), []byte(module), 0600); err != nil {
		t.Fatal(err)
	}
	s := New(Config{PluginsDir: root, Limits: DefaultLimits()}, Options{})
	if _, err = s.Dispatch(context.Background(), "feeds.refresh", nil); err != nil {
		t.Fatal(err)
	}
	result := s.itemsReply()
	items := result["items"].([]map[string]any)
	if len(items) != 1 || items[0]["title"] != "Configured title" || items[0]["kind"] != "media" || items[0]["media_url"] != "https://images.example/media.png" {
		t.Fatal(result, s.status())
	}
	if len(items[0]["tags"].([]string)) != 2 {
		t.Fatal(items)
	}
}
