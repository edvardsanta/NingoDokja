package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	domain "read_books/dokja_domain/dokja_runtime"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

// ChatAdapters run a single bounded inference, never a workflow or fallback.
type ChatAdapters struct {
	Root        string
	Environment func([]string) ([]string, error)
	Run         func(context.Context, string, []string, string, string) ([]byte, error)
}

func (a ChatAdapters) Generate(ctx context.Context, b domain.ChatBinding, m []domain.ChatMessage) (string, error) {
	return a.generate(ctx, b, m, nil)
}

// GenerateStream is Generate with a preview of the text so far, for the providers whose output can be
// read as it comes (Ollama's text, Codex's agent messages). Claude Code gives none yet. The reply is
// still taken and checked from the whole output, exactly as Generate does.
func (a ChatAdapters) GenerateStream(ctx context.Context, b domain.ChatBinding, m []domain.ChatMessage, onPreview func(string)) (string, error) {
	return a.generate(ctx, b, m, onPreview)
}

func (a ChatAdapters) generate(ctx context.Context, b domain.ChatBinding, m []domain.ChatMessage, onPreview func(string)) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
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
		args = []string{"exec", "--sandbox", "read-only", "--skip-git-repo-check", "--ephemeral", "--ignore-user-config", "--ignore-rules", "--json", "--color", "never", "--model", b.Model}
		for _, feature := range []string{"shell_tool", "unified_exec", "apps", "browser_use", "computer_use", "in_app_browser", "multi_agent", "hooks", "plugins", "skill_search", "memories", "image_generation", "view_image", "sleep_tool", "workspace_dependencies"} {
			args = append(args, "-c", "features."+feature+"=false")
		}
		args = append(args, "-c", `web_search="disabled"`, "-c", "features.skip_host_skill_discovery=true", "-c", "suppress_unstable_features_warning=true", "-o", filepath.Join(dir, "reply.txt"), "-")
	case "ollama":
		binary = "docker"
		args = composeArgs(a.Root, []string{"exec", "-T", "dokja-ollama", "ollama", "run", b.Model})
	default:
		return "", fmt.Errorf("unsupported chat provider")
	}
	run := a.Run
	if run == nil {
		var observe func([]byte)
		if onPreview != nil && (b.Provider == "ollama" || b.Provider == "codex") {
			observe = (&chatPreview{provider: b.Provider, emit: onPreview}).observe
		}
		run = func(ctx context.Context, binary string, args []string, prompt, dir string) ([]byte, error) {
			return a.runObserved(ctx, binary, args, prompt, dir, observe)
		}
	}
	if b.Provider == "ollama" {
		check := a.Run
		if check == nil {
			check = a.run
		}
		if _, err := check(ctx, binary, composeArgs(a.Root, []string{"exec", "-T", "dokja-ollama", "ollama", "show", b.Model}), "", dir); err != nil {
			return "", fmt.Errorf("selected chat model is not installed or its provider is unavailable")
		}
	}
	output, err := run(ctx, binary, args, prompt, dir)
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil {
		var failure *domain.ChatProviderFailure
		if errors.As(err, &failure) {
			return "", failure
		}
		return "", &domain.ChatProviderFailure{Code: domain.ChatUnavailable}
	}
	switch b.Provider {
	case "claude_code":
		var v struct {
			Result  string `json:"result"`
			IsError bool   `json:"is_error"`
		}
		if json.Unmarshal(output, &v) != nil {
			return "", fmt.Errorf("invalid chat provider response")
		}
		if v.IsError {
			return "", classifyChatFailure(output, nil)
		}
		return v.Result, nil
	case "codex":
		if err := validateChatEvents(output); err != nil {
			return "", err
		}
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

// validateChatEvents accepts inference messages only. Disabling tools at launch
// is the primary control; the trace gate refuses tool/failed/unknown events even
// when the CLI has left a seemingly valid final reply file behind.
func validateChatEvents(output []byte) error {
	completed := false
	for _, line := range bytes.Split(output, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var event struct {
			Type string `json:"type"`
			Item struct {
				Type string `json:"type"`
			} `json:"item"`
		}
		if json.Unmarshal(line, &event) != nil {
			return fmt.Errorf("invalid chat provider event")
		}
		switch event.Type {
		case "thread.started", "turn.started":
		case "item.started", "item.updated", "item.completed":
			if event.Item.Type != "agent_message" && event.Item.Type != "reasoning" {
				return fmt.Errorf("chat provider attempted a tool or unsupported event")
			}
		case "turn.completed":
			completed = true
		default:
			return fmt.Errorf("chat provider failed or returned an unsupported event")
		}
	}
	if !completed {
		return fmt.Errorf("chat provider returned no completed turn")
	}
	return nil
}

func (a ChatAdapters) run(ctx context.Context, binary string, args []string, prompt, dir string) ([]byte, error) {
	return a.runObserved(ctx, binary, args, prompt, dir, nil)
}

// runObserved runs one provider process; observe, when set, sees its output as it is written.
func (a ChatAdapters) runObserved(ctx context.Context, binary string, args []string, prompt, dir string, observe func([]byte)) ([]byte, error) {
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = dir
	cmd.Stdin = bytes.NewBufferString(prompt)
	stderr := &chatOutput{}
	cmd.Stderr = stderr
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
	out := &chatOutput{observe: observe}
	cmd.Stdout = out
	err := cmd.Run()
	if out.overflow || stderr.overflow {
		return nil, &domain.ChatProviderFailure{Code: domain.ChatOutputLimit}
	}
	if err != nil {
		return nil, classifyChatFailure(out.Bytes(), stderr.Bytes())
	}
	return out.Bytes(), nil
}

// Keep the buffer named: embedding bytes.Buffer promotes ReadFrom, allowing
// io.Copy in os/exec to bypass this writer's output limit.
type chatOutput struct {
	buffer   bytes.Buffer
	overflow bool
	observe  func([]byte)
}

func (b *chatOutput) Bytes() []byte { return b.buffer.Bytes() }

func (b *chatOutput) Write(p []byte) (int, error) {
	n := len(p)
	if b.buffer.Len()+n > 65536 {
		b.overflow = true
		return 0, fmt.Errorf("output limit")
	}
	n, err := b.buffer.Write(p)
	if err == nil && b.observe != nil {
		b.observe(p[:n])
	}
	return n, err
}

// chatPreview turns what a provider prints into the text it has written so far. It decides nothing:
// a preview is shown and thrown away, and the reply is parsed and checked from the whole output once
// the process ends. Output it cannot read gives no preview rather than a wrong one.
type chatPreview struct {
	provider string
	emit     func(string)
	raw      []byte
	last     string
}

func (p *chatPreview) observe(chunk []byte) {
	p.raw = append(p.raw, chunk...)
	switch p.provider {
	case "ollama":
		p.show(validPrefix(p.raw))
	case "codex":
		// whole lines only; the last one may still be on its way
		end := bytes.LastIndexByte(p.raw, '\n')
		if end < 0 {
			return
		}
		for _, line := range bytes.Split(p.raw[:end], []byte("\n")) {
			var event struct {
				Type string `json:"type"`
				Item struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"item"`
			}
			if json.Unmarshal(line, &event) != nil {
				continue
			}
			if (event.Type == "item.updated" || event.Type == "item.completed") && event.Item.Type == "agent_message" && event.Item.Text != "" {
				p.show(event.Item.Text)
			}
		}
		p.raw = append([]byte(nil), p.raw[end+1:]...)
	}
}

func (p *chatPreview) show(text string) {
	if strings.TrimSpace(text) == "" || text == p.last {
		return
	}
	p.last = text
	p.emit(text)
}

// validPrefix is the longest start of raw that is whole UTF-8: a character cut between two writes
// waits for its other bytes.
func validPrefix(raw []byte) string {
	for end := len(raw); end > 0 && end > len(raw)-utf8.UTFMax; end-- {
		if utf8.Valid(raw[:end]) {
			return string(raw[:end])
		}
	}
	if utf8.Valid(raw) {
		return string(raw)
	}
	return ""
}
