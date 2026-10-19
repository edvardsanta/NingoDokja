//go:build unix

package feeds

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// inputRunner records what each extractor was handed on stdin and answers with reply.
type inputRunner struct {
	mu     sync.Mutex
	inputs map[string][]byte
	reply  func(plugin Plugin) ([]byte, error)
}

func (r *inputRunner) Run(_ context.Context, plugin Plugin, _ []string, input []byte) ([]byte, error) {
	r.mu.Lock()
	if r.inputs == nil {
		r.inputs = map[string][]byte{}
	}
	r.inputs[plugin.ID] = input
	r.mu.Unlock()
	return r.reply(plugin)
}

func (r *inputRunner) ran(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.inputs[id]
	return ok
}

func documentsOutput(texts ...string) []byte {
	documents := make([]map[string]string, len(texts))
	for i, text := range texts {
		documents[i] = map[string]string{"title": fmt.Sprintf("Part %d", i+1), "text": text, "key": fmt.Sprint(i)}
	}
	body, _ := json.Marshal(map[string]any{"documents": documents})
	return body
}

func extractPayload(filename, content string) map[string]any {
	return map[string]any{"filename": filename, "content_b64": base64.StdEncoding.EncodeToString([]byte(content))}
}

func newExtractor(t *testing.T, root, id string, enabled bool, formats ...string) {
	t.Helper()
	newPlugin(t, root, id, v1(map[string]any{"id": id, "enabled": enabled, "formats": formats}))
}

func extractService(root string, runner Runner) *Service {
	return New(Config{PluginsDir: root, Limits: testLimits()}, Options{Runner: runner, Now: func() time.Time { return testNow }})
}

func reasonOf(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		t.Fatal("expected the reading to fail")
	}
	reason, _, ok := strings.Cut(err.Error(), ":")
	if !ok {
		t.Fatalf("an extract error must start with a reason: %v", err)
	}
	return reason
}

func TestExtractHandsTheFileToTheBoundPluginAndReturnsItsDocuments(t *testing.T) {
	root := t.TempDir()
	newExtractor(t, root, "reader", true, ".txt")
	runner := &inputRunner{reply: func(Plugin) ([]byte, error) { return documentsOutput("first", "second"), nil }}
	service := extractService(root, runner)

	result, err := service.Dispatch(context.Background(), "feeds.extract", extractPayload(`C:\docs\Notes.TXT`, "hello"))
	if err != nil {
		t.Fatal(err)
	}
	documents := result["documents"].([]Document)
	if result["plugin"] != "reader" || result["version"] != "0.1.0" || result["format"] != ".txt" ||
		result["count"] != 2 || documents[1].Text != "second" || documents[0].Title != "Part 1" {
		t.Fatalf("unexpected result %#v", result)
	}
	var request extractRequest
	if err := json.Unmarshal(runner.inputs["reader"], &request); err != nil {
		t.Fatal(err)
	}
	decoded, _ := base64.StdEncoding.DecodeString(request.ContentB64)
	if request.Operation != CapabilityDocumentExtract || request.Filename != "Notes.TXT" || request.Format != ".txt" ||
		string(decoded) != "hello" {
		t.Fatalf("the plugin must get the operation, the base name, the format and the bytes: %#v", request)
	}
}

func TestExtractRefusesBadRequestsWithoutRunningAPlugin(t *testing.T) {
	root := t.TempDir()
	newExtractor(t, root, "reader", true, ".txt")
	runner := &inputRunner{reply: func(Plugin) ([]byte, error) { return documentsOutput("x"), nil }}
	service := extractService(root, runner)

	cases := map[string]struct {
		payload map[string]any
		reason  string
	}{
		"no filename":      {map[string]any{"content_b64": "eA=="}, ReasonInvalid},
		"no extension":     {extractPayload("README", "x"), ReasonUnsupported},
		"no content":       {map[string]any{"filename": "a.txt"}, ReasonInvalid},
		"bad base64":       {map[string]any{"filename": "a.txt", "content_b64": "not base64!"}, ReasonInvalid},
		"empty file":       {map[string]any{"filename": "a.txt", "content_b64": ""}, ReasonInvalid},
		"too large":        {map[string]any{"filename": "a.txt", "content_b64": strings.Repeat("A", base64.StdEncoding.EncodedLen(MaxExtractInput)+4)}, ReasonInvalid},
		"no provider":      {extractPayload("a.pdf", "x"), ReasonUnavailable},
		"upper-case still": {extractPayload("a.PDF", "x"), ReasonUnavailable},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := service.Dispatch(context.Background(), "feeds.extract", tc.payload)
			if got := reasonOf(t, err); got != tc.reason {
				t.Fatalf("reason=%q want %q (%v)", got, tc.reason, err)
			}
		})
	}
	if runner.ran("reader") {
		t.Fatal("no plugin may run for a request that is refused")
	}
}

func TestExtractNeverRunsADisabledAmbiguousOrIncompatibleProvider(t *testing.T) {
	cases := map[string]struct {
		setup  func(t *testing.T, root string)
		reason string
		detail string
	}{
		"disabled": {func(t *testing.T, root string) { newExtractor(t, root, "off", false, ".pdf") }, ReasonUnavailable, ".pdf"},
		"ambiguous": {func(t *testing.T, root string) {
			newExtractor(t, root, "one", true, ".pdf")
			newExtractor(t, root, "two", true, ".pdf")
		}, ReasonAmbiguous, "one, two"},
		"lifecycle version 2": {func(t *testing.T, root string) {
			newPlugin(t, root, "future", v1(map[string]any{"id": "future", "enabled": true, "plugin_api": 2}))
		}, ReasonUnavailable, "future (incompatible:"},
		"operation version 2": {func(t *testing.T, root string) {
			newPlugin(t, root, "next", v1(map[string]any{"id": "next", "enabled": true, "provides": []string{"document.extract.v2"}}))
		}, ReasonUnavailable, "next (incompatible:"},
		"collector only": {func(t *testing.T, root string) { newPlugin(t, root, "news", map[string]any{"enabled": true}) }, ReasonUnavailable, ".pdf"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			tc.setup(t, root)
			ran := false
			runner := &inputRunner{reply: func(Plugin) ([]byte, error) { ran = true; return documentsOutput("x"), nil }}

			_, err := extractService(root, runner).Dispatch(context.Background(), "feeds.extract", extractPayload("a.pdf", "x"))
			if got := reasonOf(t, err); got != tc.reason || !strings.Contains(err.Error(), tc.detail) {
				t.Fatalf("got %v, want reason %q mentioning %q", err, tc.reason, tc.detail)
			}
			if ran {
				t.Fatal("nothing may run when the format has no single eligible provider")
			}
		})
	}
}

func TestExtractGivesEveryPluginFailureADefinedReason(t *testing.T) {
	cases := map[string]struct {
		output []byte
		err    error
		reason string
	}{
		"crash":               {nil, errors.New("exit status 3"), ReasonFailed},
		"hang":                {nil, fmt.Errorf("%w after 5s", ErrTimeout), ReasonTimeout},
		"too much output":     {nil, fmt.Errorf("%w: more than 4 KiB", ErrOutputTooLarge), ReasonFailed},
		"not json":            {[]byte("Traceback (most recent call last)"), nil, ReasonFailed},
		"two objects":         {[]byte(`{"documents":[]} {}`), nil, ReasonFailed},
		"no documents list":   {[]byte(`{"items":[]}`), nil, ReasonFailed},
		"unknown field":       {[]byte(`{"documents":[{"title":"a","text":"b","page":1}]}`), nil, ReasonFailed},
		"wrong type":          {[]byte(`{"documents":[{"title":"a","text":7}]}`), nil, ReasonFailed},
		"both answers":        {[]byte(`{"documents":[],"error":"x"}`), nil, ReasonFailed},
		"no text":             {documentsOutput("  ", "\x00"), nil, ReasonUnreadable},
		"declared unreadable": {[]byte(`{"error":"the PDF is password protected"}`), nil, ReasonUnreadable},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			newExtractor(t, root, "reader", true, ".pdf")
			runner := &inputRunner{reply: func(Plugin) ([]byte, error) { return tc.output, tc.err }}
			service := extractService(root, runner)

			_, err := service.Dispatch(context.Background(), "feeds.extract", extractPayload("a.pdf", "x"))
			if got := reasonOf(t, err); got != tc.reason {
				t.Fatalf("reason=%q want %q (%v)", got, tc.reason, err)
			}
			row := service.status()["extractors"].([]map[string]any)[0]
			if row["last_error"] == "" || row["last_run"] == "" || row["last_ok"] != "" {
				t.Fatalf("the failure must be the extractor's last result: %#v", row)
			}
		})
	}
}

func TestExtractCleansDocumentsAndRecordsSuccess(t *testing.T) {
	root := t.TempDir()
	newExtractor(t, root, "reader", true, ".txt")
	output := []byte(`{"documents":[{"title":"  A\n title ","text":"keep\nlines\u0000 here","source_ref":"chapter 1","key":"c1"},{"title":"blank","text":" "}]}`)
	service := extractService(root, &inputRunner{reply: func(Plugin) ([]byte, error) { return output, nil }})

	result, err := service.Dispatch(context.Background(), "feeds.extract", extractPayload("a.txt", "x"))
	if err != nil {
		t.Fatal(err)
	}
	documents := result["documents"].([]Document)
	if len(documents) != 1 || documents[0].Title != "A title" || documents[0].Text != "keep\nlines here" ||
		documents[0].SourceRef != "chapter 1" || documents[0].Key != "c1" {
		t.Fatalf("documents must be bounded and cleaned, keeping line breaks in the text: %#v", documents)
	}
	row := service.status()["extractors"].([]map[string]any)[0]
	if row["last_ok"] == "" || row["last_error"] != "" {
		t.Fatalf("a good reading must be the extractor's last result: %#v", row)
	}
}

func TestReplacingAnExtractorNeedsNoCodeChange(t *testing.T) {
	root := t.TempDir()
	newExtractor(t, root, "old-reader", true, ".pdf")
	runner := &inputRunner{reply: func(plugin Plugin) ([]byte, error) { return documentsOutput("by " + plugin.ID), nil }}

	read := func() string {
		result, err := extractService(root, runner).Dispatch(context.Background(), "feeds.extract", extractPayload("a.pdf", "x"))
		if err != nil {
			t.Fatal(err)
		}
		return result["plugin"].(string)
	}
	if got := read(); got != "old-reader" {
		t.Fatalf("got %s", got)
	}
	// The owner disables the old plugin and adds a new one: only manifests change.
	newExtractor(t, root, "old-reader", false, ".pdf")
	newExtractor(t, root, "new-reader", true, ".pdf")
	if got := read(); got != "new-reader" {
		t.Fatalf("the single enabled provider must be chosen after a replacement, got %s", got)
	}
}

func TestExtractWaitsForAFreeSlotOnlyUntilTheDeadline(t *testing.T) {
	root := t.TempDir()
	newExtractor(t, root, "reader", true, ".txt")
	release := make(chan struct{})
	started := make(chan struct{}, maxExtractions)
	runner := &inputRunner{reply: func(Plugin) ([]byte, error) {
		started <- struct{}{}
		<-release
		return documentsOutput("x"), nil
	}}
	service := extractService(root, runner)
	defer close(release)

	for range maxExtractions {
		go service.Dispatch(context.Background(), "feeds.extract", extractPayload("a.txt", "x"))
	}
	for range maxExtractions {
		<-started
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := service.Dispatch(ctx, "feeds.extract", extractPayload("a.txt", "x"))
	if reasonOf(t, err) != ReasonTimeout {
		t.Fatalf("a reading past the concurrency limit must give up at the deadline: %v", err)
	}
	if _, err := service.Dispatch(context.Background(), "feeds.status", nil); err != nil {
		t.Fatalf("status must answer while readings run: %v", err)
	}
}

func TestExtractStopsAHungPluginAtTheCallersDeadline(t *testing.T) {
	root := t.TempDir()
	dir := newPlugin(t, root, "slow", v1(map[string]any{"id": "slow", "enabled": true, "formats": []string{".txt"},
		"limits": map[string]any{"timeout_ms": 60000, "output_bytes": 4096}}))
	writeFile(t, filepath.Join(dir, "run.sh"), "#!/bin/sh\nexec sleep 30\n", 0o755)
	service := New(Config{PluginsDir: root, Limits: testLimits()}, Options{Lookup: func(string) (string, bool) { return "", false }})

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	began := time.Now()
	_, err := service.Dispatch(ctx, "feeds.extract", extractPayload("a.txt", "x"))
	if reasonOf(t, err) != ReasonTimeout || time.Since(began) > 5*time.Second {
		t.Fatalf("the plugin must be stopped at the caller's deadline, not its own: %v after %s", err, time.Since(began))
	}
}

func TestACollectorReadsNoInputAndAnExtractorReadsItsRequest(t *testing.T) {
	plugin := scriptPlugin(t, `if IFS= read -r line; then printf '%s' "$line"; else printf none; fi`, 5*time.Second)

	out, err := ExecRunner{}.Run(context.Background(), plugin, []string{testPath}, nil)
	if err != nil || string(out) != "none" {
		t.Fatalf("a scheduled run must have no input: %q %v", out, err)
	}
	out, err = ExecRunner{}.Run(context.Background(), plugin, []string{testPath}, []byte("request\n"))
	if err != nil || string(out) != "request" {
		t.Fatalf("an extractor must read its request on stdin: %q %v", out, err)
	}
}

// The example adapter serves Knowledge's own extractors through this host. It needs python3,
// and the Knowledge sources next to this module.
func TestTheKnowledgeExtractorsExampleServesKnowledgeFormats(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is not installed")
	}
	knowledge, err := filepath.Abs("../../dokja_knowledge")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(knowledge, "extractors", "__init__.py")); err != nil {
		t.Skip("the Knowledge sources are not next to this module")
	}
	root := t.TempDir()
	if err := os.CopyFS(filepath.Join(root, "knowledge-extractors"), os.DirFS("../plugins.example/knowledge-extractors")); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, "knowledge-extractors", "plugin.json")
	raw, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, manifest, strings.Replace(string(raw), `"enabled": false`, `"enabled": true`, 1), 0o644)
	env := map[string]string{"PATH": os.Getenv("PATH"), "KNOWLEDGE_EXTRACTORS_DIR": knowledge}
	service := New(Config{PluginsDir: root, Limits: DefaultLimits()}, Options{
		Lookup: func(name string) (string, bool) { value, ok := env[name]; return value, ok },
	})

	result, err := service.Dispatch(context.Background(), "feeds.extract",
		extractPayload("page.html", "<html><head><title>On Reading</title></head><body><p>Reading is a faculty.</p></body></html>"))
	if err != nil {
		t.Fatal(err)
	}
	documents := result["documents"].([]Document)
	if result["plugin"] != "knowledge-extractors" || len(documents) != 1 || documents[0].Title != "On Reading" ||
		!strings.Contains(documents[0].Text, "Reading is a faculty.") {
		t.Fatalf("unexpected reading %#v", result)
	}

	_, err = service.Dispatch(context.Background(), "feeds.extract", extractPayload("broken.epub", "not a zip"))
	if reasonOf(t, err) != ReasonUnreadable || !strings.Contains(err.Error(), "not a valid archive") {
		t.Fatalf("Knowledge's own extract error must come back as unreadable: %v", err)
	}
}

// The owner's register(registry) Knowledge plugins are served through the same adapter once
// their extensions are listed in the manifest.
func TestTheKnowledgeExtractorsExampleServesTheOwnersKnowledgePlugins(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is not installed")
	}
	knowledge, err := filepath.Abs("../../dokja_knowledge")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(knowledge, "plugins.py")); err != nil {
		t.Skip("the Knowledge sources are not next to this module")
	}
	root, owned := t.TempDir(), t.TempDir()
	if err := os.CopyFS(filepath.Join(root, "knowledge-extractors"), os.DirFS("../plugins.example/knowledge-extractors")); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, "knowledge-extractors", "plugin.json")
	raw, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(raw), `"enabled": false`, `"enabled": true`, 1)
	edited = strings.Replace(edited, `"formats": [`, `"formats": [".foo", `, 1)
	writeFile(t, manifest, edited, 0o644)
	writeFile(t, filepath.Join(owned, "foo.py"), `from extractors import Extracted


def register(registry):
    registry.add_extractor(".foo", lambda data, name: [Extracted(title="Foo", text=data.decode().upper())])
`, 0o644)
	env := map[string]string{"PATH": os.Getenv("PATH"), "KNOWLEDGE_EXTRACTORS_DIR": knowledge, "KNOWLEDGE_PLUGINS_DIR": owned}
	service := New(Config{PluginsDir: root, Limits: DefaultLimits()}, Options{
		Lookup: func(name string) (string, bool) { value, ok := env[name]; return value, ok },
	})

	result, err := service.Dispatch(context.Background(), "feeds.extract", extractPayload("notes.foo", "owner format"))
	if err != nil {
		t.Fatal(err)
	}
	if documents := result["documents"].([]Document); len(documents) != 1 || documents[0].Text != "OWNER FORMAT" {
		t.Fatalf("the owner's plugin must read its format: %#v", result)
	}
}
