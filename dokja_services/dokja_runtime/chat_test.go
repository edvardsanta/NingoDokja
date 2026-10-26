package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	domain "read_books/dokja_domain/dokja_runtime"
	store "read_books/dokja_store"
	"reflect"
	"strings"
	"testing"
)

func TestChatBindingSurvivesReopenAndRejectsStaleWrites(t *testing.T) {
	s := databaseFixture(t)
	b, err := s.ReadChat()
	if err != nil || b.Revision != 1 || b.Provider != "" {
		t.Fatal(b, err)
	}
	before, _, _ := s.DB.ProviderConfiguration()
	b.Provider = "codex"
	b.Model = "fixture/model"
	saved, err := s.WriteChat(b)
	if err != nil || saved.Revision != 2 {
		t.Fatal(saved, err)
	}
	if _, err = s.WriteChat(b); err == nil {
		t.Fatal("stale write accepted")
	}
	path := s.DB.Path()
	s.Close()
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s.DB = db
	got, err := s.ReadChat()
	if err != nil || got != saved {
		t.Fatal(got, err)
	}
	after, _, _ := s.DB.ProviderConfiguration()
	if before.Revision != after.Revision {
		t.Fatal("registration changed")
	}
}
func TestChatAdaptersBoundAndIsolateEachSelectedProvider(t *testing.T) {
	for _, id := range []string{"codex", "claude_code", "ollama"} {
		t.Run(id, func(t *testing.T) {
			calls := 0
			var isolated string
			a := ChatAdapters{Root: t.TempDir(), Run: func(ctx context.Context, binary string, args []string, input, dir string) ([]byte, error) {
				calls++
				isolated = dir
				if entries, _ := os.ReadDir(dir); len(entries) != 0 {
					t.Fatal("workspace exposed")
				}
				if id == "ollama" && strings.Contains(strings.Join(args, " "), " show ") {
					return []byte("installed"), nil
				}
				start := strings.Index(input, "\n[")
				var transcript []domain.ChatMessage
				if start < 0 || json.Unmarshal([]byte(input[start+1:]), &transcript) != nil {
					t.Fatal("application transcript was not forwarded as valid JSON")
				}
				expected := []domain.ChatMessage{
					{Role: "system", Content: "context"}, {Role: "user", Content: "question"},
					{Role: "assistant", Content: "A billboard on the avenue."}, {Role: "user", Content: "followup"},
				}
				if !reflect.DeepEqual(transcript, expected) || !strings.Contains(input[:start], "same application assistant") {
					t.Fatal("history lost")
				}
				command := strings.Join(args, " ")
				if !strings.Contains(command, "fixture/model") {
					t.Fatal("model ignored")
				}
				if id == "codex" {
					if !strings.Contains(command, "features.shell_tool=false") || !strings.Contains(command, "--ignore-user-config") {
						t.Fatal("isolation flags missing")
					}
					return []byte("{\"type\":\"turn.completed\"}\n"), os.WriteFile(filepath.Join(dir, "reply.txt"), []byte("answer"), 0600)
				}
				if id == "claude_code" {
					if !strings.Contains(command, "--strict-mcp-config") || !strings.Contains(command, "--tools ") {
						t.Fatal("tools allowed")
					}
					return []byte(`{"result":"answer"}`), nil
				}
				return []byte("answer"), nil
			}}
			reply, err := a.Generate(t.Context(), domain.ChatBinding{Provider: id, Model: "fixture/model"}, []domain.ChatMessage{
				{Role: "system", Content: "context"}, {Role: "user", Content: "question"},
				{Role: "assistant", Content: "A billboard on the avenue."}, {Role: "user", Content: "followup"},
			})
			if err != nil || reply != "answer" || calls != map[string]int{"codex": 1, "claude_code": 1, "ollama": 2}[id] {
				t.Fatal(reply, err, calls)
			}
			if _, err := os.Stat(isolated); !os.IsNotExist(err) {
				t.Fatal("prompt directory retained")
			}
		})
	}
}
func TestChatAdapterFailureDoesNotLeakOutputOrFallback(t *testing.T) {
	calls := 0
	a := ChatAdapters{Run: func(context.Context, string, []string, string, string) ([]byte, error) {
		calls++
		return []byte("private-output"), errors.New("private-error")
	}}
	_, err := a.Generate(t.Context(), domain.ChatBinding{Provider: "claude_code", Model: "fixture"}, nil)
	if err == nil || strings.Contains(err.Error(), "private") || calls != 1 {
		t.Fatal(err, calls)
	}
}

func TestMissingLocalModelDoesNotPullOrGenerate(t *testing.T) {
	calls := 0
	a := ChatAdapters{Root: t.TempDir(), Run: func(_ context.Context, _ string, args []string, _ string, _ string) ([]byte, error) {
		calls++
		if !strings.Contains(strings.Join(args, " "), " show fixture") {
			t.Fatal(args)
		}
		return nil, errors.New("missing")
	}}
	if _, err := a.Generate(t.Context(), domain.ChatBinding{Provider: "ollama", Model: "fixture"}, nil); err == nil || calls != 1 {
		t.Fatal(err, calls)
	}
}

func TestChatAdaptersRejectCodexToolEventsEvenWithReplyFile(t *testing.T) {
	a := ChatAdapters{Run: func(_ context.Context, _ string, _ []string, _ string, dir string) ([]byte, error) {
		if err := os.WriteFile(filepath.Join(dir, "reply.txt"), []byte("answer"), 0600); err != nil {
			t.Fatal(err)
		}
		return []byte("{\"type\":\"item.completed\",\"item\":{\"type\":\"command_execution\"}}\n{\"type\":\"turn.completed\"}\n"), nil
	}}
	if reply, err := a.Generate(t.Context(), domain.ChatBinding{Provider: "codex", Model: "fixture"}, nil); err == nil || reply != "" {
		t.Fatal("accepted tool output", reply, err)
	}
}
func TestChatAdaptersRejectLateReplies(t *testing.T) {
	for _, provider := range []string{"codex", "claude_code", "ollama"} {
		t.Run(provider, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			a := ChatAdapters{Root: t.TempDir(), Run: func(_ context.Context, _ string, args []string, _ string, dir string) ([]byte, error) {
				calls++
				if provider == "ollama" && strings.Contains(strings.Join(args, " "), " show ") {
					return []byte("installed"), nil
				}
				cancel()
				os.WriteFile(filepath.Join(dir, "reply.txt"), []byte("late"), 0600)
				if provider == "claude_code" {
					return []byte(`{"result":"late"}`), nil
				}
				return []byte("late"), nil
			}}
			if reply, err := a.Generate(ctx, domain.ChatBinding{Provider: provider, Model: "fixture"}, nil); err == nil || reply != "" {
				t.Fatal("late success accepted", reply, err, calls)
			}
		})
	}
}
