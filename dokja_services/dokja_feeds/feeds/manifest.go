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

// Lifecycle (ADR 0002). A manifest without plugin_api is the original feed shape and goes
// through the legacy loader unchanged; plugin_api 1 selects the versioned loader.
const (
	pluginAPIVersion          = 1
	runtimeSubprocess         = "subprocess"
	CapabilitySourceCollect   = "source.collect.v1"
	CapabilityDocumentExtract = "document.extract.v1"
	maxFormats                = 20
)

// ErrIncompatible marks a manifest that names a lifecycle version, runtime or capability
// this host does not support. Its message starts with "incompatible:".
var ErrIncompatible = errors.New("incompatible")

var (
	idPattern      = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,39}$`)
	envPattern     = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
	versionPattern = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z.+-]{0,39}$`)
	formatPattern  = regexp.MustCompile(`^\.[a-z0-9]{1,10}$`)
	knownProvides  = map[string]bool{CapabilitySourceCollect: true, CapabilityDocumentExtract: true}
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

// manifestV1 is plugin.json with "plugin_api": 1. It is decoded as strictly as the legacy
// shape. Limits are required: a versioned plugin states its own bounds.
type manifestV1 struct {
	PluginAPI int      `json:"plugin_api"`
	ID        string   `json:"id"`
	Version   string   `json:"version"`
	Name      string   `json:"name"`
	Provides  []string `json:"provides"`
	Runtime   string   `json:"runtime"`
	Enabled   bool     `json:"enabled"`
	Command   []string `json:"command"`
	Env       []string `json:"env"`
	Interval  string   `json:"interval"`
	MaxItems  int      `json:"max_items"`
	Formats   []string `json:"formats"`
	Limits    struct {
		TimeoutMS   int `json:"timeout_ms"`
		OutputBytes int `json:"output_bytes"`
	} `json:"limits"`
}

// Plugin is a manifest that passed validation, with its values resolved. Command[0] is an
// absolute path. A legacy manifest has APIVersion 0 and provides source collection.
type Plugin struct {
	ID          string
	Name        string
	Dir         string
	Enabled     bool
	Command     []string
	Interval    time.Duration
	Timeout     time.Duration
	Env         []string
	MaxItems    int
	APIVersion  int
	Version     string
	Provides    []string
	Formats     []string
	OutputBytes int64
}

// Collects reports whether the plugin is a scheduled source of feed items.
func (p Plugin) Collects() bool {
	return p.provides(CapabilitySourceCollect)
}

func (p Plugin) provides(capability string) bool {
	for _, provided := range p.Provides {
		if provided == capability {
			return true
		}
	}
	return false
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
	version, versioned, err := lifecycleVersion(raw)
	if err != nil {
		return nil, err
	}
	if !versioned {
		return loadLegacy(dir, name, raw, limits)
	}
	if version != pluginAPIVersion {
		return nil, fmt.Errorf("%w: plugin_api %d is not supported; this host supports %d", ErrIncompatible, version, pluginAPIVersion)
	}
	return loadV1(dir, name, raw, limits)
}

// lifecycleVersion reads plugin_api without trusting the rest of the manifest. A manifest
// that is not even an object is left to the legacy loader, which explains why.
func lifecycleVersion(raw []byte) (int, bool, error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return 0, false, nil
	}
	value, present := probe["plugin_api"]
	if !present {
		return 0, false, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()
	var number any
	if err := decoder.Decode(&number); err == nil {
		if n, ok := number.(json.Number); ok {
			if parsed, err := n.Int64(); err == nil && parsed > 0 && parsed < 1000 {
				return int(parsed), true, nil
			}
		}
	}
	return 0, false, errors.New("plugin_api must be a positive whole number")
}

func decodeStrict(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("%s is not valid: %v", manifestName, err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%s must hold one JSON object", manifestName)
	}
	return nil
}

func checkID(id, name string) error {
	if !idPattern.MatchString(id) {
		return errors.New("id must be lowercase letters, digits, - or _ (at most 40)")
	}
	if id != name {
		return fmt.Errorf("id %q must match the directory name %q", id, name)
	}
	return nil
}

func checkMaxItems(value int) (int, error) {
	switch {
	case value == 0:
		return defaultMaxItems, nil
	case value < 0 || value > maxMaxItems:
		return 0, fmt.Errorf("max_items must be between 1 and %d", maxMaxItems)
	}
	return value, nil
}

func checkEnv(names []string) error {
	if len(names) > maxEnvNames {
		return fmt.Errorf("env lists at most %d variable names", maxEnvNames)
	}
	for _, variable := range names {
		if !envPattern.MatchString(variable) {
			return fmt.Errorf("env name %q must be capital letters, digits and _", variable)
		}
	}
	return nil
}

func title(name, id string) string {
	if cleaned := cleanText(name, maxSourceRunes); cleaned != "" {
		return cleaned
	}
	return id
}

func loadLegacy(dir, name string, raw []byte, limits Limits) (*Plugin, error) {
	var m manifest
	if err := decodeStrict(raw, &m); err != nil {
		return nil, err
	}
	if err := checkID(m.ID, name); err != nil {
		return nil, err
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
	maxItems, err := checkMaxItems(m.MaxItems)
	if err != nil {
		return nil, err
	}
	if err := checkEnv(m.Env); err != nil {
		return nil, err
	}
	program, err := resolveCommand(dir, m.Command)
	if err != nil {
		return nil, err
	}
	return &Plugin{
		ID:       m.ID,
		Name:     title(m.Name, m.ID),
		Dir:      dir,
		Enabled:  m.Enabled,
		Command:  append([]string{program}, m.Command[1:]...),
		Interval: interval,
		Timeout:  timeout,
		Env:      m.Env,
		MaxItems: maxItems,
		Provides: []string{CapabilitySourceCollect},
	}, nil
}

func loadV1(dir, name string, raw []byte, limits Limits) (*Plugin, error) {
	var m manifestV1
	if err := decodeStrict(raw, &m); err != nil {
		return nil, err
	}
	if err := checkID(m.ID, name); err != nil {
		return nil, err
	}
	if !versionPattern.MatchString(m.Version) {
		return nil, errors.New("version is required: letters, digits, ., + or - (at most 40)")
	}
	if m.Runtime != runtimeSubprocess {
		return nil, fmt.Errorf("%w: runtime %q is not supported; this host runs %q", ErrIncompatible, cleanText(m.Runtime, 32), runtimeSubprocess)
	}
	if len(m.Provides) == 0 {
		return nil, errors.New("provides must name at least one capability")
	}
	seen := map[string]bool{}
	for _, capability := range m.Provides {
		if !knownProvides[capability] {
			return nil, fmt.Errorf("%w: capability %q is not supported by this host", ErrIncompatible, cleanText(capability, 64))
		}
		if seen[capability] {
			return nil, fmt.Errorf("provides lists %q twice", capability)
		}
		seen[capability] = true
	}
	plugin := &Plugin{
		ID: m.ID, Name: title(m.Name, m.ID), Dir: dir, Enabled: m.Enabled, Env: m.Env,
		APIVersion: m.PluginAPI, Version: m.Version, Provides: append([]string(nil), m.Provides...),
	}
	if seen[CapabilitySourceCollect] {
		interval, err := boundedDuration("interval", m.Interval, defaultInterval, limits.MinInterval, limits.MaxInterval)
		if err != nil {
			return nil, err
		}
		if plugin.MaxItems, err = checkMaxItems(m.MaxItems); err != nil {
			return nil, err
		}
		plugin.Interval = interval
	} else if m.Interval != "" || m.MaxItems != 0 {
		return nil, fmt.Errorf("interval and max_items apply only to %s", CapabilitySourceCollect)
	}
	if seen[CapabilityDocumentExtract] {
		formats, err := checkFormats(m.Formats)
		if err != nil {
			return nil, err
		}
		plugin.Formats = formats
	} else if len(m.Formats) > 0 {
		return nil, fmt.Errorf("formats apply only to %s", CapabilityDocumentExtract)
	}
	timeout := time.Duration(m.Limits.TimeoutMS) * time.Millisecond
	if timeout < limits.MinTimeout || timeout > limits.MaxTimeout {
		return nil, fmt.Errorf("limits.timeout_ms must be between %d and %d", limits.MinTimeout.Milliseconds(), limits.MaxTimeout.Milliseconds())
	}
	if m.Limits.OutputBytes <= 0 || m.Limits.OutputBytes > maxOutput {
		return nil, fmt.Errorf("limits.output_bytes must be between 1 and %d", maxOutput)
	}
	plugin.Timeout, plugin.OutputBytes = timeout, int64(m.Limits.OutputBytes)
	if err := checkEnv(m.Env); err != nil {
		return nil, err
	}
	program, err := resolveCommand(dir, m.Command)
	if err != nil {
		return nil, err
	}
	plugin.Command = append([]string{program}, m.Command[1:]...)
	return plugin, nil
}

func checkFormats(formats []string) ([]string, error) {
	if len(formats) == 0 || len(formats) > maxFormats {
		return nil, fmt.Errorf("formats must list between 1 and %d file extensions", maxFormats)
	}
	seen := map[string]bool{}
	for _, format := range formats {
		if !formatPattern.MatchString(format) || seen[format] {
			return nil, fmt.Errorf("format %q must be a unique lowercase extension such as .pdf", cleanText(format, 16))
		}
		seen[format] = true
	}
	return append([]string(nil), formats...), nil
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
