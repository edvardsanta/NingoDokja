package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	inference "read_books/dokja_domain/dokja_inference"
	"strings"
	"testing"
	"time"
)

func request() inference.Request {
	return inference.Request{Contract: inference.Version, TaskID: "t", Scope: "owner", Goal: "sum", Revision: 2, RemainingCalls: 3, Evidence: []inference.Evidence{{Ref: inference.Ref{ID: "rule", Version: "1", Source: "fixture:rule"}, Question: "sample"}}, Actions: []inference.Action{{ID: "fast", Description: "parse"}}}
}
func answer() string {
	return `{"contract":"inference.v1","revision":2,"status":"need_evidence","action":"","missing":"sample","target":"","probability":0,"evidence_ids":["rule"],"experience_ids":[],"summary":"Need observation"}`
}
func TestOllamaFallbackAndBudget(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/api/chat" {
			t.Error(r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["format"] == nil || body["stream"] != false {
			t.Error(body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"model": "fixture-model", "message": map[string]any{"content": answer()}, "prompt_eval_count": 11, "eval_count": 7})
	}))
	defer srv.Close()
	c := Chain{Providers: []Config{{Name: "unknown", Timeout: time.Second}, {Name: "ollama", Model: "fixture-model", Endpoint: srv.URL, Timeout: time.Second}}}
	got, err := c.Infer(context.Background(), request())
	if err != nil || len(got.Attempts) != 2 || got.Attempts[0].Outcome == "accepted" || got.Attempts[1].InputTokens == nil {
		t.Fatal(got, err)
	}
	r := request()
	r.RemainingCalls = 1
	_, err = c.Infer(context.Background(), r)
	if err == nil || calls != 1 {
		t.Fatal("fallback escaped budget", err, calls)
	}
}
func TestMalformedAndTimedOutProviders(t *testing.T) {
	for _, bad := range []string{`{}`, strings.Replace(answer(), `"revision":2`, `"revision":1`, 1), strings.Repeat("x", 300000)} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"message": map[string]string{"content": bad}})
		}))
		_, err := (Chain{Providers: []Config{{Name: "ollama", Model: "fixture", Endpoint: srv.URL, Timeout: time.Second}}}).Infer(context.Background(), request())
		srv.Close()
		if err == nil {
			t.Fatal("accepted invalid model output")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, err := (Chain{Providers: []Config{{Name: "ollama", Timeout: time.Second}}}).Infer(ctx, request())
	if err == nil || len(r.Attempts) != 0 {
		t.Fatal(r, err)
	}
}
func TestCLIIsolationArguments(t *testing.T) {
	for _, provider := range []string{"claude", "codex"} {
		t.Run(provider, func(t *testing.T) {
			dir := t.TempDir()
			argsPath := filepath.Join(dir, "args")
			cwdPath := filepath.Join(dir, "cwd")
			// Controlled executable fixtures observe actual adapter args without a model.
			script := "#!/bin/sh\npwd > \"$TEST_CWD\"\nprintf '%s\\n' \"$@\" > \"$TEST_ARGS\"\ncat >/dev/null\n"
			if provider == "claude" {
				envelope, _ := json.Marshal(map[string]any{"structured_output": json.RawMessage(answer()), "modelUsage": map[string]any{"fixture": map[string]any{}}})
				script += "printf '%s\\n' '" + string(envelope) + "'\n"
			} else {
				script += "while [ $# -gt 0 ]; do if [ \"$1\" = '-o' ]; then shift; printf '%s' '" + answer() + "' > \"$1\"; fi; shift; done\nprintf '%s\\n' '{\"type\":\"turn.completed\",\"usage\":{\"input_tokens\":1,\"output_tokens\":2}}'\n"
			}
			if err := os.WriteFile(filepath.Join(dir, provider), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("TEST_ARGS", argsPath)
			t.Setenv("TEST_CWD", cwdPath)
			result, err := (Chain{Providers: []Config{{Name: provider, Timeout: time.Second}}}).Infer(context.Background(), request())
			if err != nil {
				t.Fatal(result, err)
			}
			args, _ := os.ReadFile(argsPath)
			cwd, _ := os.ReadFile(cwdPath)
			if strings.TrimSpace(string(cwd)) == dir || !strings.Contains(string(cwd), "dokja-inference-") {
				t.Fatal("not isolated", string(cwd))
			}
			flags := []string{"--tools", "--strict-mcp-config", "disableAllHooks"}
			if provider == "codex" {
				flags = []string{"--ignore-user-config", "--ephemeral", "features.shell_tool=false", "features.multi_agent=false", "features.hooks=false", "features.plugins=false", `web_search="disabled"`}
			}
			for _, flag := range flags {
				if !strings.Contains(string(args), flag) {
					t.Fatal("missing isolation flag", flag)
				}
			}
			if _, err := os.Stat(strings.TrimSpace(string(cwd))); !os.IsNotExist(err) {
				t.Fatal("temporary inference directory retained")
			}
		})
	}
}

func TestCLIRejectsToolEventsAndCancels(t *testing.T) {
	for _, tc := range []struct {
		name, script string
		timeout      time.Duration
	}{
		{"tool", "printf '%s\\n' '{\"type\":\"item.completed\",\"item\":{\"type\":\"command_execution\"}}'\n", time.Second},
		{"timeout", "sleep 30\n", 50 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "codex"), []byte("#!/bin/sh\ncat >/dev/null\n"+tc.script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			start := time.Now()
			result, err := (Chain{Providers: []Config{{Name: "codex", Timeout: tc.timeout}}}).Infer(context.Background(), request())
			if err == nil || len(result.Attempts) != 1 || result.Attempts[0].Outcome == "accepted" {
				t.Fatal(result, err)
			}
			if time.Since(start) > 3*time.Second {
				t.Fatal("cancellation did not terminate provider promptly")
			}
			if tc.name == "timeout" && result.Attempts[0].Outcome != "timeout_or_cancelled" {
				t.Fatal(result)
			}
		})
	}
}
