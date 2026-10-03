package feeds

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const quietScript = "#!/bin/sh\necho '{\"items\":[]}'\n"

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

// newPlugin writes plugins/<id>/plugin.json (a valid manifest, changed by overrides; a nil
// value removes a key) and an executable run.sh, and returns the plugin directory.
func newPlugin(t *testing.T, root, id string, overrides map[string]any) string {
	t.Helper()
	manifest := map[string]any{"id": id, "category": "feed", "command": []string{"./run.sh"}}
	for key, value := range overrides {
		if value == nil {
			delete(manifest, key)
		} else {
			manifest[key] = value
		}
	}
	body, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, id)
	writeFile(t, filepath.Join(dir, "plugin.json"), string(body), 0o644)
	writeFile(t, filepath.Join(dir, "run.sh"), quietScript, 0o755)
	return dir
}

func discoverOne(t *testing.T, root string) Found {
	t.Helper()
	found, err := Discover(root, DefaultLimits())
	if err != nil || len(found) != 1 {
		t.Fatalf("found=%v err=%v", found, err)
	}
	return found[0]
}

func TestAMinimalManifestGetsSafeDefaults(t *testing.T) {
	root := t.TempDir()
	dir := newPlugin(t, root, "example", nil)

	found := discoverOne(t, root)
	if found.Err != nil {
		t.Fatal(found.Err)
	}
	plugin := found.Plugin
	if plugin.Enabled {
		t.Error("a plugin must be disabled unless its manifest says otherwise")
	}
	if plugin.Interval != 15*time.Minute || plugin.Timeout != 30*time.Second || plugin.MaxItems != 50 {
		t.Errorf("defaults: interval=%v timeout=%v max=%d", plugin.Interval, plugin.Timeout, plugin.MaxItems)
	}
	if plugin.Name != "example" || plugin.ID != "example" {
		t.Errorf("name=%q id=%q", plugin.Name, plugin.ID)
	}
	resolved, _ := filepath.EvalSymlinks(filepath.Join(dir, "run.sh"))
	if len(plugin.Command) != 1 || plugin.Command[0] != resolved {
		t.Errorf("command = %v, want [%s]", plugin.Command, resolved)
	}
}

func TestAManifestCanEnableAndTuneAPlugin(t *testing.T) {
	root := t.TempDir()
	newPlugin(t, root, "example", map[string]any{
		"enabled": true, "name": "  A   source ", "interval": "5m", "timeout": "10s",
		"max_items": 7, "env": []string{"EXAMPLE_TOKEN"}, "command": []string{"./run.sh", "--flag", "value"},
	})
	plugin := discoverOne(t, root).Plugin
	if !plugin.Enabled || plugin.Name != "A source" || plugin.Interval != 5*time.Minute ||
		plugin.Timeout != 10*time.Second || plugin.MaxItems != 7 || len(plugin.Env) != 1 {
		t.Fatalf("%+v", plugin)
	}
	if got := plugin.Command[1:]; len(got) != 2 || got[0] != "--flag" || got[1] != "value" {
		t.Fatalf("arguments = %v", got)
	}
}

func TestAProgramOnPathIsResolved(t *testing.T) {
	root := t.TempDir()
	newPlugin(t, root, "example", map[string]any{"command": []string{"sh", "-c", "true"}})
	plugin := discoverOne(t, root).Plugin
	if !filepath.IsAbs(plugin.Command[0]) || filepath.Base(plugin.Command[0]) != "sh" {
		t.Fatalf("command = %v", plugin.Command)
	}
}

func TestAnUnusableManifestIsReportedWithItsReason(t *testing.T) {
	cases := map[string]struct {
		overrides map[string]any
		want      string
	}{
		"an id with capitals":           {map[string]any{"id": "Example"}, "id must be"},
		"an id that is not the folder":  {map[string]any{"id": "other"}, "must match the directory name"},
		"no category":                   {map[string]any{"category": nil}, "category must be"},
		"another category":              {map[string]any{"category": "weather"}, "category must be"},
		"an interval that is too short": {map[string]any{"interval": "10s"}, "interval must be between"},
		"an interval that is too long":  {map[string]any{"interval": "72h"}, "interval must be between"},
		"an interval that is not time":  {map[string]any{"interval": "often"}, "interval must be a duration"},
		"a timeout that is too long":    {map[string]any{"timeout": "1h"}, "timeout must be between"},
		"too many items":                {map[string]any{"max_items": 201}, "max_items must be"},
		"negative items":                {map[string]any{"max_items": -1}, "max_items must be"},
		"a lowercase variable":          {map[string]any{"env": []string{"token"}}, "env name"},
		"a variable with a dash":        {map[string]any{"env": []string{"A-B"}}, "env name"},
		"no command":                    {map[string]any{"command": nil}, "command is required"},
		"an empty command":              {map[string]any{"command": []string{" "}}, "command is required"},
		"an absolute command":           {map[string]any{"command": []string{"/bin/sh"}}, "inside the plugin directory"},
		"a command that leaves":         {map[string]any{"command": []string{"../outside.sh"}}, "does not exist"},
		"a missing file":                {map[string]any{"command": []string{"./missing.sh"}}, "does not exist"},
		"a program that is not there":   {map[string]any{"command": []string{"no-such-program-xyz"}}, "is not installed"},
		"a NUL in an argument":          {map[string]any{"command": []string{"./run.sh", "a\x00b"}}, "NUL"},
		"too many parts":                {map[string]any{"command": append([]string{"./run.sh"}, make([]string, 20)...)}, "at most"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			newPlugin(t, root, "example", c.overrides)
			found := discoverOne(t, root)
			if found.Plugin != nil || found.Err == nil || !strings.Contains(found.Err.Error(), c.want) {
				t.Fatalf("plugin=%v err=%v, want an error containing %q", found.Plugin, found.Err, c.want)
			}
		})
	}
}

func TestAManifestThatIsNotAnObjectOfKnownFieldsIsRefused(t *testing.T) {
	cases := map[string]struct{ body, want string }{
		"not JSON":             {"{", "not valid"},
		"an unknown field":     {`{"id":"example","category":"feed","command":["sh"],"url":"x"}`, "unknown field"},
		"two objects":          {`{"id":"example"} {"id":"example"}`, "one JSON object"},
		"a list":               {`[]`, "not valid"},
		"a wrong type":         {`{"id":"example","enabled":"yes"}`, "not valid"},
		"a manifest too large": {`{"id":"` + strings.Repeat("a", maxManifestBytes) + `"}`, "larger than"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, filepath.Join(root, "example", "plugin.json"), c.body, 0o644)
			found := discoverOne(t, root)
			if found.Plugin != nil || found.Err == nil || !strings.Contains(found.Err.Error(), c.want) {
				t.Fatalf("plugin=%v err=%v, want an error containing %q", found.Plugin, found.Err, c.want)
			}
		})
	}

	t.Run("a directory without a manifest", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, "example", "run.sh"), quietScript, 0o755)
		found := discoverOne(t, root)
		if found.Err == nil || !strings.Contains(found.Err.Error(), "no plugin.json") {
			t.Fatalf("err=%v", found.Err)
		}
	})
}

func TestACommandThatIsNotARunnableFileInsideTheDirectoryIsRefused(t *testing.T) {
	t.Run("not executable", func(t *testing.T) {
		root := t.TempDir()
		dir := newPlugin(t, root, "example", nil)
		if err := os.Chmod(filepath.Join(dir, "run.sh"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := discoverOne(t, root).Err; err == nil || !strings.Contains(err.Error(), "not executable") {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("a directory", func(t *testing.T) {
		root := t.TempDir()
		dir := newPlugin(t, root, "example", map[string]any{"command": []string{"./tools"}})
		if err := os.Mkdir(filepath.Join(dir, "tools"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := discoverOne(t, root).Err; err == nil || !strings.Contains(err.Error(), "not a file") {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("a link that points outside the directory", func(t *testing.T) {
		root := t.TempDir()
		dir := newPlugin(t, root, "example", nil)
		outside := filepath.Join(t.TempDir(), "outside.sh")
		writeFile(t, outside, quietScript, 0o755)
		if err := os.Remove(filepath.Join(dir, "run.sh")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(dir, "run.sh")); err != nil {
			t.Fatal(err)
		}
		if err := discoverOne(t, root).Err; err == nil || !strings.Contains(err.Error(), "stay inside") {
			t.Fatalf("err=%v", err)
		}
	})
}

func TestDiscoverReadsDirectoriesInOrderAndSkipsTheRest(t *testing.T) {
	root := t.TempDir()
	newPlugin(t, root, "beta", nil)
	newPlugin(t, root, "alpha", nil)
	newPlugin(t, root, ".hidden", map[string]any{"id": "hidden"})
	newPlugin(t, root, "_disabled-by-name", map[string]any{"id": "x"})
	writeFile(t, filepath.Join(root, "README.txt"), "not a plugin", 0o644)

	found, err := Discover(root, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range found {
		names = append(names, f.Dir)
	}
	if strings.Join(names, ",") != "alpha,beta" {
		t.Fatalf("directories = %v", names)
	}
}

func TestDiscoverWithoutADirectoryOrWithAMissingOne(t *testing.T) {
	if found, err := Discover("", DefaultLimits()); err != nil || found != nil {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if _, err := Discover(filepath.Join(t.TempDir(), "missing"), DefaultLimits()); err == nil {
		t.Fatal("a missing plugins directory was not reported")
	}
}

func TestTheLimitsCanBeWidenedForTests(t *testing.T) {
	root := t.TempDir()
	newPlugin(t, root, "example", map[string]any{"interval": "50ms"})
	limits := DefaultLimits()
	limits.MinInterval = 10 * time.Millisecond
	found, err := Discover(root, limits)
	if err != nil || found[0].Err != nil || found[0].Plugin.Interval != 50*time.Millisecond {
		t.Fatalf("found=%+v err=%v", found, err)
	}
}
