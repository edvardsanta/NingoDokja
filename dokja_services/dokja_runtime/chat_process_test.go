package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	domain "read_books/dokja_domain/dokja_runtime"
)

// These are controlled executables, not model mocks injected at the Run seam.
// They exercise process I/O, launch isolation, trace rejection and cancellation.
func TestChatCLIProcessIsolationAndCleanup(t *testing.T) {
	for _, provider := range []string{"codex", "claude_code"} {
		t.Run(provider, func(t *testing.T) {
			fixture := t.TempDir()
			for key, file := range map[string]string{"DOKJA_TEST_ARGS": "args", "DOKJA_TEST_CWD": "cwd", "DOKJA_TEST_INPUT": "input"} {
				t.Setenv(key, filepath.Join(fixture, file))
			}
			name := "codex"
			script := `#!/bin/sh
pwd > "$DOKJA_TEST_CWD"
printf '%s\n' "$@" > "$DOKJA_TEST_ARGS"
cat > "$DOKJA_TEST_INPUT"
`
			if provider == "codex" {
				script += `while [ "$#" -gt 0 ]; do
 if [ "$1" = '-o' ]; then shift; printf 'answer' > "$1"; fi
 shift
done
printf '%s\n' '{"type":"thread.started"}' '{"type":"turn.started"}' '{"type":"item.completed","item":{"type":"agent_message"}}' '{"type":"turn.completed"}'
`
			} else {
				name = "claude"
				script += `printf '%s\n' '{"result":"answer","is_error":false}'
`
			}
			if err := os.WriteFile(filepath.Join(fixture, name), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", fixture+string(os.PathListSeparator)+os.Getenv("PATH"))
			result, err := (ChatAdapters{}).Generate(t.Context(), domain.ChatBinding{Provider: provider, Model: "fixture/model"}, []domain.ChatMessage{{Role: "user", Content: "fixture question"}})
			if err != nil || result != "answer" {
				t.Fatal(result, err)
			}
			cwd, _ := os.ReadFile(filepath.Join(fixture, "cwd"))
			isolated := strings.TrimSpace(string(cwd))
			if isolated == fixture || !strings.Contains(isolated, "dokja-inference-") {
				t.Fatal("workspace not isolated")
			}
			if _, err := os.Stat(isolated); !os.IsNotExist(err) {
				t.Fatal("prompt directory retained")
			}
			args, _ := os.ReadFile(filepath.Join(fixture, "args"))
			flags := []string{"--model", "fixture/model", "--tools", "--strict-mcp-config", "--no-session-persistence", "disableAllHooks"}
			if provider == "codex" {
				flags = []string{"--json", "--ignore-rules", "--ignore-user-config", "--ephemeral", "features.shell_tool=false", "features.multi_agent=false", "features.plugins=false", "features.hooks=false", "features.memories=false", "features.skip_host_skill_discovery=true", `web_search="disabled"`}
			}
			for _, flag := range flags {
				if !strings.Contains(string(args), flag) {
					t.Fatal("missing isolation flag", flag)
				}
			}
			input, _ := os.ReadFile(filepath.Join(fixture, "input"))
			if !strings.Contains(string(input), `"role":"user"`) || !strings.Contains(string(input), "fixture question") {
				t.Fatal("message roles lost")
			}
		})
	}
}

func TestChatCLIProcessCancellationAndOutputLimit(t *testing.T) {
	for _, mode := range []string{"cancel", "overflow"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			script := "#!/bin/sh\ncat >/dev/null\n"
			if mode == "cancel" {
				script += "sleep 30\n"
			} else {
				script += "head -c 65537 /dev/zero\n"
			}
			if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			timeout := 2 * time.Second
			if mode == "cancel" {
				timeout = 100 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(t.Context(), timeout)
			defer cancel()
			start := time.Now()
			reply, err := (ChatAdapters{}).Generate(ctx, domain.ChatBinding{Provider: "claude_code", Model: "fixture"}, nil)
			if err == nil || reply != "" || time.Since(start) > 3*time.Second {
				t.Fatal("unbounded or accepted process output", reply, err)
			}
		})
	}
}

func TestChatEventTraceRequiresCompletedInferenceWithoutTools(t *testing.T) {
	for _, bad := range []string{
		"", "not json", `{"type":"turn.failed"}`, `{"type":"error"}`, `{"type":"new_event"}`,
		`{"type":"item.completed","item":{"type":"web_search"}}`,
		`{"type":"item.started","item":{"type":"mcp_tool_call"}}`,
		`{"type":"item.completed","item":{"type":"agent_message"}}`,
	} {
		if err := validateChatEvents([]byte(bad)); err == nil {
			t.Fatal("accepted incompatible trace", bad)
		}
	}
	if err := validateChatEvents([]byte("{\"type\":\"item.completed\",\"item\":{\"type\":\"reasoning\"}}\n{\"type\":\"turn.completed\"}\n")); err != nil {
		t.Fatal(err)
	}
}
