package runtime

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	domain "read_books/dokja_domain/dokja_runtime"
)

// EnvFile keeps the stack's settings in the owner's .env, the file Compose reads, so a stack
// started by hand sees the same values. Only the settings' own lines change: every other line,
// comment and credential is left byte for byte, and only setting variables are ever parsed.
type EnvFile struct {
	Path string
}

var (
	envLine       = regexp.MustCompile(`^(\s*(?:export\s+)?)([A-Z][A-Z0-9_]*)\s*=(.*)$`)
	webhookChan   = regexp.MustCompile(`^[0-9]{1,20}$`)
	unsafeInValue = regexp.MustCompile("[\\s$#'\"`\\\\]")
)

func allowed() map[string]bool {
	names := map[string]bool{}
	for _, name := range domain.SettingsEnv() {
		names[name] = true
	}
	return names
}

func unquote(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
		return value[1 : len(value)-1]
	}
	return value
}

// Read returns the settings' values and the channel ids that deliver through a webhook. The
// webhook addresses are credentials: they are dropped as soon as their line is split.
func (e EnvFile) Read() (map[string]string, []string, error) {
	values := map[string]string{}
	webhooks := []string{}
	raw, err := os.ReadFile(e.Path)
	if os.IsNotExist(err) {
		return values, webhooks, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("cannot read the stack's .env")
	}
	names := allowed()
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		match := envLine.FindStringSubmatch(scanner.Text())
		if match == nil || !names[match[2]] {
			continue
		}
		if match[2] == domain.WebhooksEnv {
			webhooks = webhooks[:0]
			for _, pair := range strings.Split(unquote(match[3]), ",") {
				id, _, _ := strings.Cut(strings.TrimSpace(pair), "=")
				if webhookChan.MatchString(id) {
					webhooks = append(webhooks, id)
				}
			}
			continue
		}
		values[match[2]] = unquote(match[3])
	}
	return values, webhooks, nil
}

// Write sets the given variables, replacing their lines where they are and adding the others at
// the end. The file is replaced atomically and keeps its permissions.
func (e EnvFile) Write(values map[string]string) error {
	names := allowed()
	for name, value := range values {
		if !names[name] || name == domain.WebhooksEnv {
			return fmt.Errorf("refusing to write %s", name)
		}
		if unsafeInValue.MatchString(value) {
			return fmt.Errorf("refusing an unsafe value for %s", name)
		}
	}
	mode := os.FileMode(0o600)
	raw, err := os.ReadFile(e.Path)
	switch {
	case err == nil:
		if info, statErr := os.Stat(e.Path); statErr == nil {
			mode = info.Mode().Perm()
		}
	case !os.IsNotExist(err):
		return fmt.Errorf("cannot read the stack's .env")
	}
	written := map[string]bool{}
	var out bytes.Buffer
	lines := strings.SplitAfter(string(raw), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		body := strings.TrimRight(line, "\r\n")
		match := envLine.FindStringSubmatch(body)
		if match != nil {
			if value, ok := values[match[2]]; ok {
				out.WriteString(match[1] + match[2] + "=" + value + line[len(body):])
				written[match[2]] = true
				continue
			}
		}
		out.WriteString(line)
	}
	if out.Len() > 0 && !bytes.HasSuffix(out.Bytes(), []byte("\n")) {
		out.WriteString("\n")
	}
	for _, name := range domain.SettingsEnv() {
		if value, ok := values[name]; ok && !written[name] {
			out.WriteString(name + "=" + value + "\n")
		}
	}
	temp, err := os.CreateTemp(filepath.Dir(e.Path), ".env.settings-*")
	if err != nil {
		return fmt.Errorf("cannot write next to the stack's .env")
	}
	defer os.Remove(temp.Name())
	if _, err := temp.Write(out.Bytes()); err != nil {
		temp.Close()
		return fmt.Errorf("cannot write the stack's .env")
	}
	if err := temp.Chmod(mode); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), e.Path)
}
