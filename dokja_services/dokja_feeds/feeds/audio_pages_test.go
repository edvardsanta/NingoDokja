//go:build unix

package feeds

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeYtDlp stands in for yt_dlp: it records what it was asked and answers a search with two
// entries, one of them without an id, a broken page with yt-dlp's own error, a crash with
// another error, and any other page with the page itself.
var fakeYtDlp = map[string]string{
	"yt_dlp/utils.py": "class DownloadError(Exception):\n    pass\n",
	"yt_dlp/__init__.py": `import json, os
from .utils import DownloadError
class YoutubeDL:
    def __init__(self, options): self.options = options
    def __enter__(self): return self
    def __exit__(self, *exc): return False
    def extract_info(self, target, download=True):
        print("yt-dlp chatter on stdout")
        with open(os.path.join(os.path.dirname(os.path.dirname(__file__)), "asked.json"), "w") as out:
            json.dump({"target": target, "download": download, "options": self.options}, out)
        if target.startswith("findsearch"):
            return {"entries": [
                {"id": "abc", "ie_key": "Pages", "url": "https://video.example/watch?v=abc", "title": "First song", "uploader": "Band", "duration": 201.0},
                {"title": "no id"},
                {"id": "def", "ie_key": "Pages", "url": "https://video.example/watch?v=def", "title": "Live set", "live_status": "is_live"},
            ]}
        if target.endswith("unreadable"):
            raise DownloadError("Unsupported URL: " + target)
        if target.endswith("crash"):
            raise RuntimeError("secret path /srv/private/cache")
        return {"id": "xyz", "extractor_key": "Generic", "webpage_url": target, "title": "A file", "duration": 60}
`,
}

// The example plugin runs through the host as it ships, except that its script directory is on
// the path, so the fake yt_dlp above is what it imports.
func audioPagesHost(t *testing.T, settings string) (*Service, string) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is not installed")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "audio-pages")
	script, err := os.ReadFile("../plugins.example/audio-pages/run.py")
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	shipped, err := os.ReadFile("../plugins.example/audio-pages/plugin.json")
	if err != nil || json.Unmarshal(shipped, &manifest) != nil {
		t.Fatal(err)
	}
	manifest["enabled"] = true
	manifest["command"] = []string{"python3", "-B", "run.py"}
	body, _ := json.Marshal(manifest)
	if err := os.MkdirAll(filepath.Join(dir, "yt_dlp"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "plugin.json"), string(body), 0o644)
	writeFile(t, filepath.Join(dir, "run.py"), string(script), 0o644)
	for name, source := range fakeYtDlp {
		writeFile(t, filepath.Join(dir, name), source, 0o644)
	}
	if settings != "" {
		writeFile(t, filepath.Join(dir, "settings.json"), settings, 0o644)
	}
	return New(Config{PluginsDir: root, Limits: DefaultLimits()}, Options{}), dir
}

func invokeAudio(s *Service, action string, input map[string]any) (map[string]any, error) {
	return s.Dispatch(context.Background(), "feeds.invoke", map[string]any{"capability": CapabilityAudioResolve, "action": action, "input": input})
}

func askedOf(t *testing.T, dir string) map[string]any {
	t.Helper()
	var asked map[string]any
	raw, err := os.ReadFile(filepath.Join(dir, "asked.json"))
	if err != nil || json.Unmarshal(raw, &asked) != nil {
		t.Fatal(err)
	}
	return asked
}

func TestAudioPagesSearchesWithTheOwnersPrefixWithoutDownloading(t *testing.T) {
	s, dir := audioPagesHost(t, `{"search_prefix": "findsearch"}`)
	result, err := invokeAudio(s, "search", map[string]any{"query": "  first   song ", "limit": 9})
	if err != nil || result["handled"] != true {
		t.Fatal(result, err)
	}
	asked := askedOf(t, dir)
	options := asked["options"].(map[string]any)
	if asked["target"] != "findsearch5:first song" || asked["download"] != false || options["skip_download"] != true || options["noplaylist"] != true {
		t.Fatal(asked)
	}
	tracks := result["tracks"].([]any)
	if len(tracks) != 2 {
		t.Fatal(result)
	}
	first := tracks[0].(map[string]any)
	if first["id"] != "abc" || first["url"] != "https://video.example/watch?v=abc" || first["duration_s"] != float64(201) || first["live"] != false || first["playable"] != true {
		t.Fatal(first)
	}
	if live := tracks[1].(map[string]any); live["live"] != true || live["duration_s"] != nil {
		t.Fatal(live)
	}
}

func TestAudioPagesStepsAsideForWhatItDoesNotServe(t *testing.T) {
	// no search prefix: this source does not search
	s, dir := audioPagesHost(t, "")
	if result, err := invokeAudio(s, "search", map[string]any{"query": "song"}); err != nil || result["handled"] != false {
		t.Fatal(result, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "asked.json")); err == nil {
		t.Fatal("yt-dlp must not run for a request this source does not take")
	}
	// hosts set: a page elsewhere is someone else's
	s, _ = audioPagesHost(t, `{"hosts": ["files.example"]}`)
	if result, err := invokeAudio(s, "read", map[string]any{"url": "https://other.example/a"}); err != nil || result["handled"] != false {
		t.Fatal(result, err)
	}
	result, err := invokeAudio(s, "read", map[string]any{"url": "https://files.example/song.mp3"})
	tracks, _ := result["tracks"].([]any)
	if err != nil || len(tracks) != 1 || tracks[0].(map[string]any)["source"] != "Generic" {
		t.Fatal(result, err)
	}
	status := s.status()
	if status["provider_failed"] != 0 {
		t.Fatal("stepping aside must not mark the source failed:", status)
	}
}

func TestAudioPagesRefusesAnUnreadablePageAndHidesItsOwnFailures(t *testing.T) {
	s, _ := audioPagesHost(t, "")
	result, err := invokeAudio(s, "read", map[string]any{"url": "https://files.example/unreadable"})
	if err != nil || result["handled"] != true || result["refused"] == "" {
		t.Fatal(result, err)
	}
	for name, request := range map[string][2]any{
		"not a page":  {"read", map[string]any{"url": "file:///etc/passwd"}},
		"credentials": {"read", map[string]any{"url": "https://user:pass@files.example/a.mp3"}},
		"long query":  {"search", map[string]any{"query": strings.Repeat("a", 201)}},
		"no query":    {"search", map[string]any{}},
		"other verb":  {"resolve", map[string]any{"url": "https://files.example/a.mp3"}},
	} {
		if _, err := invokeAudio(s, request[0].(string), request[1].(map[string]any)); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
	_, err = invokeAudio(s, "read", map[string]any{"url": "https://files.example/crash"})
	if err == nil || strings.Contains(err.Error(), "/srv/private") || !strings.Contains(err.Error(), "audio pages failed") {
		t.Fatal(err)
	}
}
