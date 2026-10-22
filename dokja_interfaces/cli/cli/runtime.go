package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// The local runtime controller starts and stops the lite stack's containers. It is a host
// daemon on a private Unix socket, shared by every local interface (desktop, cli and tui), so
// any of them can start what another stopped.

// RuntimeRoot finds the checkout that holds the lite stack, walking up from start.
func RuntimeRoot(start string) (string, error) {
	dir, err := filepath.EvalSymlinks(start)
	if err != nil {
		return "", err
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		if info, err := os.Stat(filepath.Join(dir, "docker-compose.lite.yml")); err == nil && info.Mode().IsRegular() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("no docker-compose.lite.yml above this directory; run from the checkout or pass --runtime-root")
		}
		dir = parent
	}
}

// containerMarkers are the files Docker and Podman leave inside a container.
var containerMarkers = []string{"/.dockerenv", "/run/.containerenv"}

// LocalRuntimeClient prepares module control for an interface started from start (a directory of
// the checkout, or the --runtime-root given). Inside a container it refuses: the controller drives
// the host's container engine and belongs on the host.
func LocalRuntimeClient(start, source string, getenv func(string) string) (*RuntimeClient, error) {
	for _, marker := range containerMarkers {
		if _, err := os.Stat(marker); err == nil {
			return nil, errors.New("this interface runs in a container; run it on the host to start and stop modules")
		}
	}
	if start == "" {
		var err error
		if start, err = os.Getwd(); err != nil {
			return nil, err
		}
	}
	root, err := RuntimeRoot(start)
	if err != nil {
		return nil, err
	}
	return NewRuntimeClient(root, source, getenv)
}

// RuntimeProjectID names one checkout, as the daemon and the desktop name it.
func RuntimeProjectID(root string) string {
	sum := sha256.Sum256([]byte(root))
	return hex.EncodeToString(sum[:])[:16]
}

// RuntimeStateDir is where the daemon of one checkout keeps its socket and ownership journal.
func RuntimeStateDir(root string, getenv func(string) string) (string, error) {
	base := getenv("XDG_STATE_HOME")
	if !filepath.IsAbs(base) {
		home := getenv("HOME")
		if !filepath.IsAbs(home) {
			return "", errors.New("cannot find the state directory: set XDG_STATE_HOME or HOME")
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "dokja", "runtime-"+RuntimeProjectID(root)), nil
}

// RuntimeModule is one row of the controller's snapshot. A name this build does not know is
// kept as it came: a newer daemon may manage more modules.
type RuntimeModule struct {
	Name         string   `json:"name"`
	State        string   `json:"state"`
	Error        string   `json:"error,omitempty"`
	Dependencies []string `json:"dependencies"`
}

type RuntimeSnapshot struct {
	Modules []RuntimeModule `json:"modules"`
	Busy    bool            `json:"busy"`
	Error   string          `json:"error,omitempty"`
}

// Stopped reports whether a module's control starts it (rather than stopping it).
func (m RuntimeModule) Stopped() bool { return m.State == "stopped" || m.State == "failed" }

type RuntimeClient struct {
	Root     string
	StateDir string
	// Binary is the daemon to launch when none is running; empty means none can be launched.
	Binary string
	Source string
}

// NewRuntimeClient prepares a client for the checkout at root. The daemon binary is
// DOKJA_RUNTIME_BIN, then dokja-runtime on PATH, then the one the desktop build leaves.
func NewRuntimeClient(root, source string, getenv func(string) string) (*RuntimeClient, error) {
	state, err := RuntimeStateDir(root, getenv)
	if err != nil {
		return nil, err
	}
	return &RuntimeClient{Root: root, StateDir: state, Binary: runtimeBinary(root, getenv), Source: source}, nil
}

func runtimeBinary(root string, getenv func(string) string) string {
	if path := getenv("DOKJA_RUNTIME_BIN"); filepath.IsAbs(path) {
		return path
	}
	if path, err := exec.LookPath("dokja-runtime"); err == nil {
		return path
	}
	built := filepath.Join(root, "dokja_interfaces", "desktop", "dist", "runtime", "dokja-runtime")
	if _, err := os.Stat(built); err == nil {
		return built
	}
	return ""
}

func (c *RuntimeClient) socket() string { return filepath.Join(c.StateDir, "control.sock") }

func (c *RuntimeClient) connected() bool {
	conn, err := net.DialTimeout("unix", c.socket(), 500*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// ensure starts the daemon when none answers. It outlives this process, like the desktop's.
func (c *RuntimeClient) ensure(ctx context.Context) error {
	if c.connected() {
		return nil
	}
	if c.Binary == "" {
		return errors.New("the local runtime controller is not running and no dokja-runtime binary was found; build it with `go build -o ~/.local/bin/dokja-runtime ./cmd/runtime` in dokja_orch/runtime, or set DOKJA_RUNTIME_BIN")
	}
	if err := os.MkdirAll(c.StateDir, 0o700); err != nil {
		return err
	}
	cmd := exec.Command(c.Binary, "--root", c.Root, "--state-dir", c.StateDir)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start the local runtime controller: %w", err)
	}
	go cmd.Wait()
	for range 60 {
		if c.connected() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	return errors.New("the local runtime controller did not start; check its binary and the permissions of " + c.StateDir)
}

// Request sends one runtime.* request and returns the controller's snapshot.
func (c *RuntimeClient) Request(ctx context.Context, eventType string, payload map[string]any) (RuntimeSnapshot, error) {
	if eventType != "runtime.modules" && eventType != "runtime.set" {
		return RuntimeSnapshot{}, fmt.Errorf("unsupported runtime action %q", eventType)
	}
	var snapshot *RuntimeSnapshot
	if err := c.call(ctx, eventType, payload, &snapshot); err != nil {
		return RuntimeSnapshot{}, err
	}
	if snapshot == nil {
		return RuntimeSnapshot{}, errors.New("the local runtime controller returned no modules")
	}
	return *snapshot, nil
}

// Settings reads the stack's settings the interfaces may change.
func (c *RuntimeClient) Settings(ctx context.Context) (SettingsView, error) {
	return c.settingsCall(ctx, "runtime.settings", nil)
}

// SetSettings changes settings by id; the controller recreates the modules that read them.
func (c *RuntimeClient) SetSettings(ctx context.Context, values map[string]string) (SettingsView, error) {
	payload := map[string]any{}
	for id, value := range values {
		payload[id] = value
	}
	return c.settingsCall(ctx, "runtime.settings.set", map[string]any{"values": payload})
}

func (c *RuntimeClient) settingsCall(ctx context.Context, eventType string, payload map[string]any) (SettingsView, error) {
	var view *SettingsView
	if err := c.call(ctx, eventType, payload, &view); err != nil {
		return SettingsView{}, err
	}
	if view == nil {
		return SettingsView{}, errors.New("the local runtime controller returned no settings")
	}
	return *view, nil
}

// call sends one request to the controller and decodes its result into out.
func (c *RuntimeClient) call(ctx context.Context, eventType string, payload map[string]any, out any) error {
	if err := c.ensure(ctx); err != nil {
		return err
	}
	if payload == nil {
		payload = map[string]any{}
	}
	event := BuildEvent(EmitOptions{
		Type: eventType, UserID: c.Source + "-user", UserName: c.Source, ChannelID: c.Source,
		Payload: payload, Context: map[string]any{"interface": c.Source}, Source: c.Source,
	})
	body, err := json.Marshal(event)
	if err != nil {
		return err
	}
	client := http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var dialer net.Dialer
		return dialer.DialContext(ctx, "unix", c.socket())
	}}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://runtime/request", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("the local runtime controller is unavailable: %w", err)
	}
	defer resp.Body.Close()
	var reply struct {
		Version   int    `json:"version"`
		ProjectID string `json:"project_id"`
		Status    string `json:"status"`
		Error     string `json:"error"`
		Result    struct {
			Result json.RawMessage `json:"result"`
		} `json:"result"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 65536)).Decode(&reply); err != nil || reply.Version != 1 || reply.ProjectID != RuntimeProjectID(c.Root) {
		return errors.New("the local runtime controller returned an incompatible response")
	}
	if reply.Status != "ok" {
		return errors.New(reply.Error)
	}
	if len(reply.Result.Result) == 0 {
		return nil
	}
	if err := json.Unmarshal(reply.Result.Result, out); err != nil {
		return errors.New("the local runtime controller returned an incompatible response")
	}
	return nil
}

// SettingRow is one setting as the controller describes it. Affects names the modules recreated
// when it changes; Note names a consequence to mention (sessions, reindex).
type SettingRow struct {
	ID      string   `json:"id"`
	Group   string   `json:"group"`
	Env     string   `json:"env"`
	Kind    string   `json:"kind"`
	Options []string `json:"options,omitempty"`
	Value   string   `json:"value"`
	Affects []string `json:"affects"`
	Note    string   `json:"note,omitempty"`
}

type SettingsView struct {
	Settings []SettingRow `json:"settings"`
	// WebhookChannels are the channel ids that deliver through a webhook rather than the bot.
	WebhookChannels []string `json:"webhook_channels"`
	Busy            bool     `json:"busy"`
	Error           string   `json:"error,omitempty"`
}

func (v SettingsView) Lookup(id string) (SettingRow, bool) {
	for _, row := range v.Settings {
		if row.ID == id {
			return row, true
		}
	}
	return SettingRow{}, false
}
