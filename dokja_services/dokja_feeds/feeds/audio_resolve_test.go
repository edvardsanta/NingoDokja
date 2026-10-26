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
// entries, one of them without an id, and a link with the video itself.
const fakeYtDlp = `import json, os
class YoutubeDL:
    def __init__(self, options): self.options = options
    def __enter__(self): return self
    def __exit__(self, *exc): return False
    def extract_info(self, target, download=True):
        print("yt-dlp chatter on stdout")
        with open(os.path.join(os.path.dirname(__file__), "asked.json"), "w") as out:
            json.dump({"target": target, "download": download, "options": self.options}, out)
        if target.startswith("ytsearch"):
            return {"entries": [
                {"id": "abc", "ie_key": "Youtube", "url": "https://video.example/watch?v=abc", "title": "First song", "uploader": "Band", "duration": 201.0},
                {"title": "no id"},
                {"id": "def", "ie_key": "Youtube", "url": "https://video.example/watch?v=def", "title": "Live set", "live_status": "is_live"},
            ]}
        if target.endswith("broken"):
            raise RuntimeError("secret path /srv/private/cache")
        return {"id": "xyz", "extractor_key": "Generic", "webpage_url": target, "title": "A file", "duration": 60}
`

// The example plugin runs through the host as it ships, except that its script directory is on
// the path, so the fake yt_dlp above is what it imports.
func audioResolveHost(t *testing.T) (*Service, string) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is not installed")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "audio-resolve")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	script, err := os.ReadFile("../plugins.example/audio-resolve/run.py")
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	shipped, err := os.ReadFile("../plugins.example/audio-resolve/plugin.json")
	if err != nil || json.Unmarshal(shipped, &manifest) != nil {
		t.Fatal(err)
	}
	manifest["enabled"] = true
	manifest["command"] = []string{"python3", "-B", "run.py"}
	body, _ := json.Marshal(manifest)
	writeFile(t, filepath.Join(dir, "plugin.json"), string(body), 0o644)
	writeFile(t, filepath.Join(dir, "run.py"), string(script), 0o644)
	writeFile(t, filepath.Join(dir, "yt_dlp.py"), fakeYtDlp, 0o644)
	return New(Config{PluginsDir: root, Limits: DefaultLimits()}, Options{}), dir
}

func resolveAudio(s *Service, input map[string]any) (map[string]any, error) {
	return s.Dispatch(context.Background(), "feeds.invoke", map[string]any{"capability": CapabilityAudioResolve, "action": "resolve", "input": input})
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

func TestAudioResolveSearchesWithoutDownloadingAndKeepsOnlyPlayableTracks(t *testing.T) {
	s, dir := audioResolveHost(t)
	result, err := resolveAudio(s, map[string]any{"query": "  first   song ", "limit": 9})
	if err != nil {
		t.Fatal(err)
	}
	asked := askedOf(t, dir)
	options := asked["options"].(map[string]any)
	if asked["target"] != "ytsearch5:first song" || asked["download"] != false || options["skip_download"] != true || options["noplaylist"] != true {
		t.Fatal(asked)
	}
	tracks := result["tracks"].([]any)
	if len(tracks) != 2 {
		t.Fatal(result)
	}
	first := tracks[0].(map[string]any)
	if first["id"] != "abc" || first["source"] != "Youtube" || first["url"] != "https://video.example/watch?v=abc" || first["title"] != "First song" || first["duration_s"] != float64(201) || first["live"] != false {
		t.Fatal(first)
	}
	if live := tracks[1].(map[string]any); live["live"] != true || live["duration_s"] != nil {
		t.Fatal(live)
	}
}

func TestAudioResolveReadsALinkAsOneTrack(t *testing.T) {
	s, dir := audioResolveHost(t)
	result, err := resolveAudio(s, map[string]any{"url": "https://files.example/song.mp3"})
	if err != nil {
		t.Fatal(err)
	}
	if askedOf(t, dir)["target"] != "https://files.example/song.mp3" {
		t.Fatal(askedOf(t, dir))
	}
	tracks := result["tracks"].([]any)
	if len(tracks) != 1 || tracks[0].(map[string]any)["source"] != "Generic" {
		t.Fatal(result)
	}
}

func TestAudioResolveRefusesBadRequestsAndHidesItsOwnFailures(t *testing.T) {
	s, _ := audioResolveHost(t)
	for name, input := range map[string]map[string]any{
		"nothing":     {},
		"both":        {"query": "song", "url": "https://files.example/a.mp3"},
		"not a page":  {"url": "file:///etc/passwd"},
		"credentials": {"url": "https://user:pass@files.example/a.mp3"},
		"long query":  {"query": strings.Repeat("a", 201)},
		"bad limit":   {"query": "song", "limit": "many"},
	} {
		if _, err := resolveAudio(s, input); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
	_, err := resolveAudio(s, map[string]any{"url": "https://files.example/broken"})
	if err == nil || strings.Contains(err.Error(), "/srv/private") || !strings.Contains(err.Error(), "audio resolve failed") {
		t.Fatal(err)
	}
}
