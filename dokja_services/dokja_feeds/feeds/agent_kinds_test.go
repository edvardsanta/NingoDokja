//go:build unix

package feeds

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The fake CLIs answer from $HOME/<name>.reply and record their arguments and working
// directory in $HOME/<name>.args; HOME is the only variable of the manifest they get here.
const fakeClaude = `#!/bin/sh
cat > /dev/null
{ pwd; for arg in "$@"; do printf '%s\n' "$arg"; done; } > "$HOME/claude.args"
cat "$HOME/claude.reply"
`

const fakeCodex = `#!/bin/sh
cat > /dev/null
{ pwd; for arg in "$@"; do printf '%s\n' "$arg"; done; } > "$HOME/codex.args"
out=""
while [ $# -gt 0 ]; do [ "$1" = "-o" ] && out="$2"; shift; done
echo "model: gpt-6.1-sol"
[ -f "$HOME/codex.reply" ] || exit 1
cp "$HOME/codex.reply" "$out"
`

type agentKinds struct {
	service *Service
	home    string
}

func newAgentKinds(t *testing.T, clis map[string]string, env map[string]string) agentKinds {
	t.Helper()
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not installed")
	}
	root, home, bin, interpreter := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	// Only python3 and the tools the fakes use join them on PATH, so a real claude or codex
	// is never found.
	for _, tool := range []string{"python3", "cat", "cp"} {
		found := python
		if tool != "python3" {
			if found, err = exec.LookPath(tool); err != nil {
				t.Skipf("%s is not installed", tool)
			}
		}
		if err := os.Symlink(found, filepath.Join(interpreter, tool)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.CopyFS(filepath.Join(root, "agent-kinds"), os.DirFS("../plugins.example/agent-kinds")); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, "agent-kinds", "plugin.json")
	raw, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, manifest, strings.Replace(string(raw), `"enabled": false`, `"enabled": true`, 1), 0o644)
	for name, script := range clis {
		writeFile(t, filepath.Join(bin, name), script, 0o755)
	}
	vars := map[string]string{"PATH": bin + ":" + interpreter, "HOME": home}
	for name, value := range env {
		vars[name] = value
	}
	service := New(Config{PluginsDir: root, Limits: DefaultLimits()}, Options{
		Lookup: func(name string) (string, bool) { value, ok := vars[name]; return value, ok },
	})
	return agentKinds{service: service, home: home}
}

func (a agentKinds) reply(t *testing.T, cli, body string) {
	t.Helper()
	writeFile(t, filepath.Join(a.home, cli+".reply"), body, 0o644)
}

func (a agentKinds) args(t *testing.T, cli string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(a.home, cli+".args"))
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}

func claudeReply(classifications string) string {
	return `{"is_error":false,"subtype":"success","modelUsage":{"claude-haiku-5-5":{}},"structured_output":{"classifications":` + classifications + `}}`
}

const newsAndNote = `[{"form":"article","content_kind":"news","uncertain_form":false,"uncertain_content_kind":false,
  "topics":["city budget"],"entities":[{"name":"Example City","type":"place"}],"date_anchor":"2026-03-14"},
 {"form":"note","content_kind":"essay","uncertain_form":false,"uncertain_content_kind":false,"topics":[],"entities":[],"date_anchor":null}]`

func classifyTwo(t *testing.T, a agentKinds) (map[string]any, error) {
	t.Helper()
	return a.service.Dispatch(context.Background(), "feeds.classify", classifyPayload("The council approved the budget.", "buy milk"))
}

func contains(list []string, values ...string) bool {
	for i := 0; i+len(values) <= len(list); i++ {
		match := true
		for j, value := range values {
			if list[i+j] != value {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func TestAgentKindsAsksClaudeWithoutToolsSettingsOrHooks(t *testing.T) {
	a := newAgentKinds(t, map[string]string{"claude": fakeClaude, "codex": fakeCodex}, nil)
	a.reply(t, "claude", claudeReply(newsAndNote))

	result, err := classifyTwo(t, a)
	if err != nil {
		t.Fatal(err)
	}
	proposals := result["classifications"].([]Proposal)
	if result["version"] != "0.1.0+claude.haiku-5-5" || proposals[0].ContentKind != "news" ||
		proposals[0].DateAnchor != "2026-03-14" || proposals[0].Entities[0].Name != "Example City" ||
		proposals[1].Form != "note" || proposals[1].ContentKind != "" || !proposals[1].UncertainContentKind {
		t.Fatalf("unexpected result %#v", result)
	}
	args := a.args(t, "claude")
	for _, flags := range [][]string{{"--tools", ""}, {"--setting-sources", ""}, {"--settings", `{"disableAllHooks": true}`},
		{"--strict-mcp-config"}, {"--no-session-persistence"}, {"-p"}} {
		if !contains(args, flags...) {
			t.Errorf("claude must run with %q: %q", flags, args)
		}
	}
	if entries, err := os.ReadDir(args[0]); err == nil && len(entries) > 0 || strings.Contains(args[0], "agent-kinds") {
		t.Errorf("claude must run from an empty directory, not %s", args[0])
	}
	if a.args(t, "codex") != nil {
		t.Error("codex is only the fallback")
	}
}

func TestAgentKindsFallsBackToCodexOnlyWhenClaudeFails(t *testing.T) {
	cases := map[string]map[string]string{
		"not installed": {"codex": fakeCodex},
		"error":         {"claude": "#!/bin/sh\necho '{\"is_error\":true,\"subtype\":\"error_during_execution\"}'\n", "codex": fakeCodex},
		"not json":      {"claude": "#!/bin/sh\necho 'Not logged in'\nexit 1\n", "codex": fakeCodex},
		"wrong count":   {"claude": "#!/bin/sh\necho '" + claudeReply(`[{}]`) + "'\n", "codex": fakeCodex},
	}
	for name, clis := range cases {
		a := newAgentKinds(t, clis, nil)
		a.reply(t, "codex", `{"classifications":`+newsAndNote+`}`)
		result, err := classifyTwo(t, a)
		if err != nil || result["version"] != "0.1.0+codex.gpt-6.1-sol" {
			t.Errorf("%s: codex must answer: %v %v", name, result, err)
			continue
		}
		args := a.args(t, "codex")
		for _, flags := range [][]string{{"--sandbox", "read-only"}, {"--ignore-user-config"}, {"--ephemeral"},
			{"-c", "features.shell_tool=false"}, {"-c", "features.unified_exec=false"}} {
			if !contains(args, flags...) {
				t.Errorf("%s: codex must run with %q: %q", name, flags, args)
			}
		}
	}
}

func TestAgentKindsKeepsAnUnsureAnswerInsteadOfAskingAnotherModel(t *testing.T) {
	a := newAgentKinds(t, map[string]string{"claude": fakeClaude, "codex": fakeCodex}, nil)
	a.reply(t, "claude", claudeReply(`[{"form":null,"content_kind":null,"uncertain_form":true,"uncertain_content_kind":true},
		{"form":"book","content_kind":"research","uncertain_form":false,"uncertain_content_kind":true}]`))
	a.reply(t, "codex", `{"classifications":`+newsAndNote+`}`)

	result, err := classifyTwo(t, a)
	if err != nil {
		t.Fatal(err)
	}
	proposals := result["classifications"].([]Proposal)
	if !proposals[0].UncertainForm || !proposals[0].UncertainContentKind || proposals[1].ContentKind != "" ||
		!proposals[1].UncertainContentKind || a.args(t, "codex") != nil {
		t.Fatalf("an unsure axis is a question for the user, not a reason to ask codex: %#v", result)
	}
}

func TestAgentKindsUsesOllamaLastAndSaysWhenNothingAnswered(t *testing.T) {
	var asked map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&asked)
		content, _ := json.Marshal(map[string]any{"classifications": json.RawMessage(newsAndNote)})
		_ = json.NewEncoder(w).Encode(map[string]any{"message": map[string]any{"content": string(content)}})
	}))
	defer server.Close()

	a := newAgentKinds(t, nil, map[string]string{"AGENT_KINDS_OLLAMA_URL": server.URL, "AGENT_KINDS_OLLAMA_MODEL": "Qwen3:8B"})
	result, err := classifyTwo(t, a)
	if err != nil || result["version"] != "0.1.0+ollama.qwen3-8b" || asked["model"] != "Qwen3:8B" || asked["format"] == nil {
		t.Fatalf("ollama answers when both CLIs are missing: %v %v %v", result, err, asked)
	}

	none := newAgentKinds(t, nil, nil)
	_, err = classifyTwo(t, none)
	if reason := reasonOf(t, err); reason != ReasonUnreadable || !strings.Contains(err.Error(), "claude is not installed") {
		t.Fatalf("with no provider the reading says why: %v", err)
	}
}
