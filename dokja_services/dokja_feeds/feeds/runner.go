//go:build unix

package feeds

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

var (
	ErrTimeout        = errors.New("timed out")
	ErrOutputTooLarge = errors.New("output is too large")
)

const (
	killGrace     = 2 * time.Second
	stderrKept    = 2048
	stderrLogRune = 500
)

// Runner runs a plugin once and returns what it printed. It is an interface so the service
// can be tested without starting processes.
type Runner interface {
	Run(ctx context.Context, plugin Plugin, env []string) ([]byte, error)
}

// ExecRunner runs a plugin as a child process of its own. A plugin is code the owner wrote
// and may be slow, wrong or hung, so the run is bounded in time and in output, gets the
// environment it declared and nothing else, has no input, and is killed with everything it
// started. What it writes to stderr goes to the log and never into an error that is shown.
type ExecRunner struct {
	MaxOutput int64
	Logger    *log.Logger
}

func (r ExecRunner) Run(parent context.Context, plugin Plugin, env []string) ([]byte, error) {
	limit := r.MaxOutput
	if limit <= 0 {
		limit = maxOutput
	}
	ctx, cancel := context.WithTimeout(parent, plugin.Timeout)
	defer cancel()

	stdout := &capped{limit: limit, onFull: cancel}
	stderr := &tail{limit: stderrKept}
	cmd := exec.CommandContext(ctx, plugin.Command[0], plugin.Command[1:]...)
	cmd.Dir = plugin.Dir
	cmd.Env = env
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	// The child leads its own process group, so one signal reaches what it started too.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = killGrace

	err := cmd.Run()
	if errors.Is(err, exec.ErrWaitDelay) && cmd.Process != nil {
		// The child is gone but something it started still holds its pipes.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	if text := stderr.text(); text != "" && r.Logger != nil {
		r.Logger.Printf("plugin=%s stderr: %s", plugin.ID, text)
	}

	switch {
	case stdout.isFull():
		return nil, fmt.Errorf("%w: more than %d KiB", ErrOutputTooLarge, limit>>10)
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return nil, fmt.Errorf("%w after %s", ErrTimeout, plugin.Timeout)
	case parent.Err() != nil:
		return nil, parent.Err()
	case err != nil && !errors.Is(err, exec.ErrWaitDelay):
		return nil, errors.New(cleanText(err.Error(), 200))
	}
	return stdout.bytes(), nil
}

// capped keeps the first limit bytes written to it. Past the limit it still reads, so the
// child is never stuck on a full pipe, and calls onFull once so the run can be stopped.
type capped struct {
	mu     sync.Mutex
	buffer bytes.Buffer
	limit  int64
	full   bool
	onFull func()
}

func (c *capped) Write(data []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	room := c.limit - int64(c.buffer.Len())
	if int64(len(data)) <= room {
		return c.buffer.Write(data)
	}
	if room > 0 {
		c.buffer.Write(data[:room])
	}
	if !c.full {
		c.full = true
		if c.onFull != nil {
			c.onFull()
		}
	}
	return len(data), nil
}

func (c *capped) isFull() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.full
}

func (c *capped) bytes() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return bytes.Clone(c.buffer.Bytes())
}

// tail keeps the last limit bytes written to it.
type tail struct {
	mu     sync.Mutex
	buffer []byte
	limit  int
}

func (t *tail) Write(data []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buffer = append(t.buffer, data...)
	if len(t.buffer) > t.limit {
		t.buffer = t.buffer[len(t.buffer)-t.limit:]
	}
	return len(data), nil
}

// text is one clean line fit for a log.
func (t *tail) text() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return cleanText(string(t.buffer), stderrLogRune)
}
