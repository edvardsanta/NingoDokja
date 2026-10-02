package feeds

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	manifestName     = "plugin.json"
	maxManifestBytes = 16 << 10
	categoryFeed     = "feed"
	defaultInterval  = 15 * time.Minute
	defaultTimeout   = 30 * time.Second
	defaultMaxItems  = 50
	maxMaxItems      = 200
	maxCommandParts  = 16
	maxCommandPart   = 1024
	maxEnvNames      = 20
)

var (
	idPattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,39}$`)
	envPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
)

// manifest is plugin.json. It is read without running the plugin, so a plugin that is
// broken, disabled or unknown can still be listed and explained.
type manifest struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Category string   `json:"category"`
	Enabled  bool     `json:"enabled"`
	Command  []string `json:"command"`
	Interval string   `json:"interval"`
	Timeout  string   `json:"timeout"`
	Env      []string `json:"env"`
	MaxItems int      `json:"max_items"`
}

// Plugin is a manifest that passed validation, with its values resolved. Command[0] is an
// absolute path.
type Plugin struct {
	ID       string
	Name     string
	Dir      string
	Enabled  bool
	Command  []string
	Interval time.Duration
	Timeout  time.Duration
	Env      []string
	MaxItems int
}

// Found is one directory of the plugins directory: a valid plugin, or the reason it is not.
type Found struct {
	Dir    string
	Plugin *Plugin
	Err    error
}

// Discover reads every plugin directory under root. Hidden directories and names starting
// with an underscore are skipped, as are files. A plugin is disabled unless its manifest
// says "enabled": true, so adding a directory never starts anything by itself.
func Discover(root string, limits Limits) ([]Found, error) {
	if root == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read plugins directory: %w", err)
	}
	var found []Found
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			continue
		}
		path := filepath.Join(root, name)
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			continue
		}
		plugin, err := loadPlugin(path, name, limits)
		found = append(found, Found{Dir: name, Plugin: plugin, Err: err})
	}
	return found, nil
}

func loadPlugin(dir, name string, limits Limits) (*Plugin, error) {
	raw, err := readManifest(filepath.Join(dir, manifestName))
	if err != nil {
		return nil, err
	}
	var m manifest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&m); err != nil {
		return nil, fmt.Errorf("%s is not valid: %v", manifestName, err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%s must hold one JSON object", manifestName)
	}

	if !idPattern.MatchString(m.ID) {
		return nil, errors.New("id must be lowercase letters, digits, - or _ (at most 40)")
	}
	if m.ID != name {
		return nil, fmt.Errorf("id %q must match the directory name %q", m.ID, name)
	}
	if m.Category != categoryFeed {
		return nil, fmt.Errorf("category must be %q", categoryFeed)
	}
	interval, err := boundedDuration("interval", m.Interval, defaultInterval, limits.MinInterval, limits.MaxInterval)
	if err != nil {
		return nil, err
	}
	timeout, err := boundedDuration("timeout", m.Timeout, defaultTimeout, limits.MinTimeout, limits.MaxTimeout)
	if err != nil {
		return nil, err
	}
	maxItems := m.MaxItems
	switch {
	case maxItems == 0:
		maxItems = defaultMaxItems
	case maxItems < 0 || maxItems > maxMaxItems:
		return nil, fmt.Errorf("max_items must be between 1 and %d", maxMaxItems)
	}
	if len(m.Env) > maxEnvNames {
		return nil, fmt.Errorf("env lists at most %d variable names", maxEnvNames)
	}
	for _, variable := range m.Env {
		if !envPattern.MatchString(variable) {
			return nil, fmt.Errorf("env name %q must be capital letters, digits and _", variable)
		}
	}
	program, err := resolveCommand(dir, m.Command)
	if err != nil {
		return nil, err
	}

	title := cleanText(m.Name, maxSourceRunes)
	if title == "" {
		title = m.ID
	}
	command := append([]string{program}, m.Command[1:]...)
	return &Plugin{
		ID:       m.ID,
		Name:     title,
		Dir:      dir,
		Enabled:  m.Enabled,
		Command:  command,
		Interval: interval,
		Timeout:  timeout,
		Env:      m.Env,
		MaxItems: maxItems,
	}, nil
}

func readManifest(path string) ([]byte, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("no %s", manifestName)
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read %s", manifestName)
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maxManifestBytes+1))
	if err != nil {
		return nil, fmt.Errorf("cannot read %s", manifestName)
	}
	if len(raw) > maxManifestBytes {
		return nil, fmt.Errorf("%s is larger than %d KiB", manifestName, maxManifestBytes>>10)
	}
	return raw, nil
}

func boundedDuration(field, raw string, fallback, min, max time.Duration) (time.Duration, error) {
	if strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be a duration such as 15m or 30s", field)
	}
	if value < min || value > max {
		return 0, fmt.Errorf("%s must be between %s and %s", field, min, max)
	}
	return value, nil
}

// resolveCommand turns command[0] into an absolute path. It is either a program found on
// PATH (a shell, an interpreter) or a file inside the plugin directory; anything else is
// refused, so a manifest cannot point at an arbitrary file of the host.
func resolveCommand(dir string, command []string) (string, error) {
	if len(command) == 0 || strings.TrimSpace(command[0]) == "" {
		return "", errors.New("command is required")
	}
	if len(command) > maxCommandParts {
		return "", fmt.Errorf("command has at most %d parts", maxCommandParts)
	}
	for _, part := range command {
		if len(part) > maxCommandPart || strings.ContainsRune(part, 0) {
			return "", errors.New("command holds a part that is too long or contains a NUL byte")
		}
	}

	program := command[0]
	if !strings.Contains(program, "/") {
		path, err := exec.LookPath(program)
		if err != nil {
			return "", fmt.Errorf("program %q is not installed where the service runs", program)
		}
		return filepath.Abs(path)
	}
	if filepath.IsAbs(program) {
		return "", errors.New("command must be a program on PATH or a path inside the plugin directory")
	}
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", errors.New("cannot read the plugin directory")
	}
	full, err := filepath.EvalSymlinks(filepath.Join(root, program))
	if err != nil {
		return "", fmt.Errorf("command %q does not exist", program)
	}
	relative, err := filepath.Rel(root, full)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("command must stay inside the plugin directory")
	}
	info, err := os.Stat(full)
	if err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("command %q is not a file", program)
	}
	if info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("command %q is not executable", program)
	}
	return full, nil
}
