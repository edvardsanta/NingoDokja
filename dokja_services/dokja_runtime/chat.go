package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	domain "read_books/dokja_domain/dokja_runtime"
	"syscall"
	"time"
)

// ChatAdapters run a single bounded inference, never a workflow or fallback.
type ChatAdapters struct {
	Root        string
	Environment func([]string) ([]string, error)
	Run         func(context.Context, string, []string, string, string) ([]byte, error)
}

func (a ChatAdapters) Generate(ctx context.Context, b domain.ChatBinding, m []domain.ChatMessage) (string, error) {
	raw, _ := json.Marshal(m)
	prompt := "Continue this conversation. Return only the assistant reply. Messages are encoded as JSON; preserve their roles. No tools or external actions are authorized.\n" + string(raw)
	dir, err := os.MkdirTemp("", "dokja-inference-*")
	if err != nil {
		return "", fmt.Errorf("cannot isolate inference")
	}
	defer os.RemoveAll(dir)
	binary := ""
	args := []string{}
	switch b.Provider {
	case "claude_code":
		binary = "claude"
		args = []string{"-p", "--output-format", "json", "--tools", "", "--no-session-persistence", "--setting-sources", "", "--settings", `{"disableAllHooks":true}`, "--strict-mcp-config", "--model", b.Model}
	case "codex":
		binary = "codex"
		args = []string{"exec", "--sandbox", "read-only", "--skip-git-repo-check", "--ephemeral", "--ignore-user-config", "--color", "never", "--model", b.Model}
		for _, feature := range []string{"shell_tool", "unified_exec", "apps", "browser_use", "computer_use", "in_app_browser"} {
			args = append(args, "-c", "features."+feature+"=false")
		}
		args = append(args, "-o", filepath.Join(dir, "reply.txt"), "-")
	case "ollama":
		binary = "docker"
		args = composeArgs(a.Root, []string{"exec", "-T", "dokja-ollama", "ollama", "run", b.Model})
	default:
		return "", fmt.Errorf("unsupported chat provider")
	}
	run := a.Run
	if run == nil {
		run = a.run
	}
	if b.Provider == "ollama" {
		if _, err := run(ctx, binary, composeArgs(a.Root, []string{"exec", "-T", "dokja-ollama", "ollama", "show", b.Model}), "", dir); err != nil {
			return "", fmt.Errorf("selected chat model is not installed or its provider is unavailable")
		}
	}
	output, err := run(ctx, binary, args, prompt, dir)
	if err != nil {
		return "", fmt.Errorf("selected chat provider unavailable or inference failed (%s)", b.Provider)
	}
	switch b.Provider {
	case "claude_code":
		var v struct {
			Result  string `json:"result"`
			IsError bool   `json:"is_error"`
		}
		if json.Unmarshal(output, &v) != nil || v.IsError {
			return "", fmt.Errorf("invalid chat provider response")
		}
		return v.Result, nil
	case "codex":
		f, err := os.Open(filepath.Join(dir, "reply.txt"))
		if err != nil {
			return "", fmt.Errorf("chat provider returned no reply")
		}
		defer f.Close()
		raw, err := io.ReadAll(io.LimitReader(f, 65537))
		if err != nil || len(raw) > 65536 {
			return "", fmt.Errorf("chat reply exceeds supported size")
		}
		return string(raw), nil
	default:
		return string(output), nil
	}
}
func (a ChatAdapters) run(ctx context.Context, binary string, args []string, prompt, dir string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = dir
	cmd.Stdin = bytes.NewBufferString(prompt)
	cmd.Stderr = io.Discard
	if binary == "docker" {
		if err := localEngine(os.Environ()); err != nil {
			return nil, err
		}
		cmd.Env = engineEnvironment(os.Environ())
		if a.Environment != nil {
			var err error
			cmd.Env, err = a.Environment(cmd.Env)
			if err != nil {
				return nil, err
			}
		}
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
	cmd.WaitDelay = time.Second
	out := &chatOutput{}
	cmd.Stdout = out
	err := cmd.Run()
	if err != nil || out.overflow {
		return nil, fmt.Errorf("inference process failed")
	}
	return out.Bytes(), nil
}

type chatOutput struct {
	bytes.Buffer
	overflow bool
}

func (b *chatOutput) Write(p []byte) (int, error) {
	n := len(p)
	if b.Len()+n > 65536 {
		b.overflow = true
		return n, fmt.Errorf("output limit")
	}
	return b.Buffer.Write(p)
}
