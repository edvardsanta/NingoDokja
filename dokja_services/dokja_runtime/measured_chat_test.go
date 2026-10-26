package runtime

import (
	"context"
	"os"
	"path/filepath"
	domain "read_books/dokja_domain/dokja_runtime"
	"strings"
	"testing"
)

func TestMeasuredAdaptersReportUsageAndReuseIsolation(t *testing.T) {
	for _, id := range []string{"codex", "claude_code"} {
		t.Run(id, func(t *testing.T) {
			calls := 0
			a := ChatAdapters{Run: func(ctx context.Context, binary string, args []string, input, dir string) ([]byte, error) {
				calls++
				command := strings.Join(args, " ")
				if id == "codex" {
					if !strings.Contains(command, "features.shell_tool=false") {
						t.Error("tools enabled")
					}
					return []byte("{\"type\":\"turn.completed\",\"usage\":{\"input_tokens\":20,\"cached_input_tokens\":5,\"output_tokens\":3}}\n"), os.WriteFile(filepath.Join(dir, "reply.txt"), []byte("answer"), 0600)
				}
				if !strings.Contains(command, "--tools ") {
					t.Error("tools enabled")
				}
				return []byte(`{"result":"answer","usage":{"input_tokens":7,"cache_read_input_tokens":30,"cache_creation_input_tokens":10,"output_tokens":4}}`), nil
			}}
			result, err := a.GenerateMeasured(t.Context(), domain.ChatBinding{Provider: id, Model: "fixture"}, []domain.ChatMessage{{Role: "user", Content: "q"}})
			if err != nil || result.Reply != "answer" || !result.Usage.Complete() || calls != 1 {
				t.Fatal(result, err, calls)
			}
			want := int64(20)
			if id == "claude_code" {
				want = 47
			}
			if *result.Usage.InputTokens != want {
				t.Fatal("cache total incorrect", result.Usage)
			}
		})
	}
}
func TestMeasuredUsageMissingFieldsDoNotBecomeZero(t *testing.T) {
	for _, tc := range []struct{ provider, raw string }{
		{"codex", `{"type":"turn.completed"}`},
		{"codex", `{"type":"turn.completed","usage":{"input_tokens":10}}`},
		{"codex", "{\"type\":\"turn.completed\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}\n{\"type\":\"turn.completed\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}"},
		{"claude_code", `{"usage":{"input_tokens":1,"output_tokens":1}}`},
		{"claude_code", `{"usage":{"input_tokens":9223372036854775807,"cache_read_input_tokens":1,"cache_creation_input_tokens":0,"output_tokens":1}}`},
		{"claude_code", `{"usage":{"input_tokens":-1,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":1}}`},
	} {
		if u := reportedTokenUsage(tc.provider, []byte(tc.raw)); u.Complete() {
			t.Fatalf("invalid/missing usage accepted: %+v", u)
		}
	}
}
func TestMeasuredUnsupportedProviderNeverLaunches(t *testing.T) {
	calls := 0
	a := ChatAdapters{Run: func(context.Context, string, []string, string, string) ([]byte, error) { calls++; return nil, nil }}
	if _, err := a.GenerateMeasured(t.Context(), domain.ChatBinding{Provider: "ollama", Model: "fixture"}, nil); err == nil || calls != 0 {
		t.Fatal("uncounted provider launched", err, calls)
	}
}
