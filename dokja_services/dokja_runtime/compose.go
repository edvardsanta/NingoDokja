// Package runtime integrates with a fixed local Compose stack. It has no module
// dependency or ownership policy: that belongs to the Runtime domain.
package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"

	zmq "github.com/pebbe/zmq4"
	domain "read_books/dokja_domain/dokja_runtime"
)

type Runner func(context.Context, ...string) ([]byte, error)
type Compose struct {
	Run          Runner
	Inspect      Runner
	Poll         time.Duration
	Wait         time.Duration
	startCommand func(context.Context, domain.Module) error
	outputMu     sync.Mutex
	output       map[string]*tailBuffer
}

func New(root string) *Compose {
	c := &Compose{output: map[string]*tailBuffer{}}
	execute := func(ctx context.Context, stream io.Writer, args ...string) ([]byte, error) {
		if err := localEngine(os.Environ()); err != nil {
			return nil, err
		}
		cmd := exec.CommandContext(ctx, "docker", args...)
		cmd.Dir = root
		cmd.Env = engineEnvironment(os.Environ())
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.Cancel = func() error {
			if cmd.Process != nil {
				return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
			return nil
		}
		cmd.WaitDelay = time.Second
		out := &boundedBuffer{}
		cmd.Stdout = out
		cmd.Stderr = io.Discard
		if stream != nil {
			cmd.Stdout = stream
			cmd.Stderr = stream
		}
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("container command failed; check that the engine is running, images can build and local ports are available")
		}
		if out.overflow {
			return nil, fmt.Errorf("container status exceeds the supported size")
		}
		return out.Bytes(), nil
	}
	c.Run = func(ctx context.Context, args ...string) ([]byte, error) {
		return execute(ctx, nil, composeArgs(root, args)...)
	}
	c.Inspect = func(ctx context.Context, ids ...string) ([]byte, error) {
		return execute(ctx, nil, append([]string{"inspect"}, ids...)...)
	}
	c.Poll = time.Second
	c.Wait = 2 * time.Minute
	c.startCommand = func(ctx context.Context, m domain.Module) error {
		tail := &tailBuffer{}
		c.outputMu.Lock()
		c.output[m.Service] = tail
		c.outputMu.Unlock()
		_, err := execute(ctx, tail, composeArgs(root, []string{"up", "-d", "--no-deps", "--no-recreate", "--build", m.Service})...)
		return err
	}
	return c
}

// profiles are the opt-in Compose profiles of modules the controller may start. Podman Compose
// does not activate a profile for a service named on the command line, so they are always
// named; only the service passed to a command is started, so this starts nothing else.
var profiles = []string{"communications", "whatsapp"}

func composeArgs(root string, args []string) []string {
	all := []string{"compose", "-f", filepath.Join(root, "docker-compose.lite.yml")}
	for _, profile := range profiles {
		all = append(all, "--profile", profile)
	}
	return append(all, args...)
}

type boundedBuffer struct {
	bytes.Buffer
	overflow bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remaining := 2*1024*1024 - b.Len()
	if len(p) > remaining {
		b.overflow = true
		p = p[:max(remaining, 0)]
	}
	b.Buffer.Write(p)
	return n, nil
}

type inspected struct {
	ID     string `json:"Id"`
	Config struct{ Labels map[string]string }
	State  struct {
		Status   string
		Running  bool
		ExitCode int
		Health   struct{ Status string }
	}
}

var containerID = regexp.MustCompile(`^[a-f0-9]{12,64}$`)

func (c *Compose) Status(ctx context.Context) (map[string]domain.Container, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	// Docker excludes stopped containers by default. Podman Compose includes them
	// but rejects --all. Query IDs only and use the shared inspect schema.
	raw, err := c.Run(ctx, "ps", "--all", "--quiet")
	if err != nil {
		raw, err = c.Run(ctx, "ps", "--quiet")
	}
	if err != nil {
		return nil, err
	}
	ids := strings.Fields(string(raw))
	result := map[string]domain.Container{}
	if len(ids) == 0 {
		return result, nil
	}
	if len(ids) > 100 {
		return nil, fmt.Errorf("too many containers for the local runtime controller")
	}
	for _, id := range ids {
		if !containerID.MatchString(id) {
			return nil, fmt.Errorf("container provider returned an invalid identifier")
		}
	}
	if c.Inspect == nil {
		return nil, fmt.Errorf("container inspection is not configured")
	}
	raw, err = c.Inspect(ctx, ids...)
	if err != nil {
		return nil, err
	}
	var rows []inspected
	if json.Unmarshal(raw, &rows) != nil {
		return nil, fmt.Errorf("container provider returned unsupported inspection data")
	}
	if len(rows) != len(ids) {
		return nil, fmt.Errorf("container inspection was incomplete; refresh before changing modules")
	}
	for _, r := range rows {
		name := r.Config.Labels["com.docker.compose.service"]
		if name == "" {
			name = r.Config.Labels["io.podman.compose.service"]
		}
		if name == "" {
			return nil, fmt.Errorf("container is missing its Compose service label")
		}
		value := domain.Container{Running: r.State.Running, Completed: r.State.Status == "exited" && r.State.ExitCode == 0, Failed: r.State.Health.Status == "unhealthy" || r.State.Status == "dead" || (r.State.Status == "exited" && r.State.ExitCode != 0)}
		old, exists := result[name]
		if exists {
			value.Running = value.Running && old.Running
			value.Completed = value.Completed && old.Completed
			value.Failed = value.Failed || old.Failed
		}
		result[name] = value
	}
	return result, nil
}

func (c *Compose) Start(ctx context.Context, m domain.Module) error {
	if !valid(m) {
		return fmt.Errorf("unknown service")
	}
	if c.startCommand != nil {
		return c.startCommand(ctx, m)
	}
	_, err := c.Run(ctx, "up", "-d", "--no-deps", "--no-recreate", "--build", m.Service)
	return err
}
func (c *Compose) Stop(ctx context.Context, m domain.Module) error {
	if !valid(m) {
		return fmt.Errorf("unknown service")
	}
	_, err := c.Run(ctx, "stop", "--timeout", "20", m.Service)
	return err
}
func valid(m domain.Module) bool {
	known, ok := domain.Lookup(m.Name)
	return ok && known.Service == m.Service && known.Port == m.Port
}

func (c *Compose) Ready(parent context.Context, m domain.Module) error {
	if !valid(m) {
		return fmt.Errorf("unknown service")
	}
	wait := c.Wait
	if m.Name == "embedding_init" {
		wait = 8 * time.Minute
	}
	ctx, cancel := context.WithTimeout(parent, wait)
	defer cancel()
	poll := c.Poll
	if poll <= 0 {
		poll = time.Second
	}
	for {
		containers, err := c.Status(ctx)
		if err != nil {
			return err
		}
		state := containers[m.Service]
		if state.Failed {
			return fmt.Errorf("%s exited or became unhealthy; inspect its container logs and retry", m.Name)
		}
		if m.Name == "embedding_init" {
			if state.Completed {
				return nil
			}
		} else if state.Running {
			if m.Name == "embeddings" {
				probe, cancel := context.WithTimeout(ctx, 5*time.Second)
				_, err := c.Run(probe, "exec", "-T", m.Service, "ollama", "list")
				cancel()
				if err == nil {
					return nil
				}
			} else if m.Name == "whatsapp" {
				probeContext, cancel := context.WithTimeout(ctx, 5*time.Second)
				_, err := c.Run(probeContext, "exec", "-T", m.Service, "node", "--input-type=module", "-e", whatsappHealth)
				cancel()
				if err == nil {
					return nil
				}
			} else if m.Name == "communications" {
				// Its port is not published, so readiness is read inside the container.
				probeContext, cancel := context.WithTimeout(ctx, 5*time.Second)
				_, err := c.Run(probeContext, "exec", "-T", m.Service, "python", "-c", communicationsHealth)
				cancel()
				if err == nil {
					return nil
				}
			} else if probe(ctx, m) == nil {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s did not become ready before the deadline; inspect its container logs and retry", m.Name)
		case <-time.After(poll):
		}
	}
}

func probe(parent context.Context, m domain.Module) error {
	if m.Name == "book" || m.Name == "chat_ai" || m.Name == "voice" {
		ctx, cancel := context.WithTimeout(parent, 2*time.Second)
		defer cancel()
		endpoint := url.URL{Scheme: "http", Host: "127.0.0.1:" + strconv.Itoa(m.Port), Path: "/health"}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
		client := http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return fmt.Errorf("unhealthy")
		}
		return nil
	}
	socket, err := zmq.NewSocket(zmq.REQ)
	if err != nil {
		return err
	}
	defer socket.Close()
	timeout := 2 * time.Second
	if m.Name == "orchestrator" {
		timeout = 30 * time.Second
	}
	if deadline, ok := parent.Deadline(); ok {
		timeout = min(timeout, time.Until(deadline))
	}
	if timeout <= 0 {
		return context.DeadlineExceeded
	}
	socket.SetLinger(0)
	socket.SetSndtimeo(timeout)
	socket.SetRcvtimeo(timeout)
	socket.SetImmediate(true)
	if err := socket.Connect("tcp://127.0.0.1:" + strconv.Itoa(m.Port)); err != nil {
		return err
	}
	action := m.Name + ".status"
	if m.Name == "orchestrator" {
		action = "ningo.status"
	}
	body, _ := json.Marshal(map[string]any{"event_id": fmt.Sprintf("runtime:%d", time.Now().UnixNano()), "timestamp": time.Now().UTC(), "source": "desktop", "type": action, "user": map[string]string{"id": "runtime", "name": "runtime"}, "channel": map[string]string{"id": "runtime"}, "payload": map[string]any{}, "context": map[string]string{"interface": "runtime"}})
	if _, err := socket.SendBytes(body, 0); err != nil {
		return err
	}
	raw, err := socket.RecvBytes(0)
	if err != nil {
		return err
	}
	var answer map[string]any
	if json.Unmarshal(raw, &answer) != nil {
		return fmt.Errorf("invalid readiness reply")
	}
	// Service REP handlers and the main orchestrator use the same status wrapper.
	if answer["status"] != "ok" {
		return fmt.Errorf("module did not acknowledge readiness")
	}
	return nil
}

// Podman's Docker-compatible CLI ignores DOCKER_HOST, while the application may
// use that local socket. Preserve an explicit Podman target and bridge the local
// socket only when no CONTAINER_HOST was provided. Docker ignores the extra key.
func engineEnvironment(env []string) []string {
	var dockerHost, containerHost string
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if key == "DOCKER_HOST" {
			dockerHost = value
		}
		if key == "CONTAINER_HOST" {
			containerHost = value
		}
	}
	if containerHost == "" && strings.HasPrefix(dockerHost, "unix://") {
		return append(append([]string(nil), env...), "CONTAINER_HOST="+dockerHost)
	}
	return env
}

const communicationsHealth = `import urllib.request; urllib.request.urlopen('http://127.0.0.1:8084/health', timeout=2)`

const whatsappHealth = `const r=await fetch('http://localhost:8094/status',{redirect:'error',signal:AbortSignal.timeout(2000)}); if(!r.ok)process.exit(1);const b=await r.json();if(typeof b.state!=='string')process.exit(1);`

func localEngine(env []string) error {
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || value == "" {
			continue
		}
		if (key == "DOCKER_HOST" || key == "CONTAINER_HOST") && !strings.HasPrefix(value, "unix://") {
			return fmt.Errorf("local runtime control requires a local Unix container-engine socket")
		}
		if key == "DOCKER_CONTEXT" && value != "default" {
			return fmt.Errorf("custom container contexts require explicit local Unix socket configuration")
		}
	}
	return nil
}

// Keep the newest build output, including stderr, without blocking a long build.
const maxLogBytes = 24 * 1024

type tailBuffer struct {
	mu   sync.Mutex
	data []byte
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	if len(p) >= maxLogBytes {
		b.data = append(b.data[:0], p[len(p)-maxLogBytes:]...)
	} else {
		if len(b.data)+len(p) > maxLogBytes {
			b.data = b.data[len(b.data)+len(p)-maxLogBytes:]
		}
		b.data = append(b.data, p...)
	}
	return n, nil
}
func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.data)
}
func cleanLogs(raw string) string {
	// Display plain text only; remove ANSI escapes and control characters.
	raw = ansiEscape.ReplaceAllString(strings.ToValidUTF8(raw, "�"), "")
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, raw)
}

var ansiEscape = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)

func (c *Compose) Logs(parent context.Context, modules []domain.Module) (string, error) {
	args := []string{"logs", "--no-color", "--timestamps", "--tail", "40"}
	var builds strings.Builder
	for _, m := range modules {
		if !valid(m) {
			return "", fmt.Errorf("unknown service")
		}
		args = append(args, m.Service)
		c.outputMu.Lock()
		tail := c.output[m.Service]
		c.outputMu.Unlock()
		if tail != nil {
			builds.WriteString("[build " + m.Name + "]\n" + tail.String() + "\n")
		}
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	raw, err := c.Run(ctx, args...)
	if err != nil && builds.Len() == 0 {
		return "", err
	}
	combined := &tailBuffer{}
	combined.Write([]byte(builds.String()))
	combined.Write(raw)
	if err != nil {
		combined.Write([]byte("\nContainer logs are not available yet.\n"))
	}
	return cleanLogs(combined.String()), nil
}
