package runtime

import (
	"context"
	"os/exec"
	"regexp"
	"strings"

	domain "read_books/dokja_domain/dokja_runtime"
)

// ProviderDiscovery inspects host CLIs and the selected lite stack's Ollama.
// It executes no prompts, loads no model, and has no startup/fallback operation.
type ProviderDiscovery struct {
	Compose  *Compose
	LookPath func(string) (string, error)
}

func (p ProviderDiscovery) Inspect(ctx context.Context, enabled map[string]bool) []domain.ProviderObservation {
	lookup := p.LookPath
	if lookup == nil {
		lookup = exec.LookPath
	}
	rows := []domain.ProviderObservation{}
	for _, entry := range []struct{ id, binary string }{{"codex", "codex"}, {"claude_code", "claude"}} {
		if !enabled[entry.id] {
			continue
		}
		row := domain.ProviderObservation{ID: entry.id, State: "unavailable", Reason: "executable_missing"}
		if _, err := lookup(entry.binary); err == nil {
			row.State, row.Reason = "installed", ""
		}
		rows = append(rows, row)
	}
	if !enabled["ollama"] {
		return rows
	}
	ollama := domain.ProviderObservation{ID: "ollama", State: "unavailable", Reason: "engine_unavailable"}
	if p.Compose == nil {
		return append(rows, ollama)
	}
	if ctx.Err() != nil {
		ollama.Reason = "timeout"
		return append(rows, ollama)
	}
	containers, err := p.Compose.Status(ctx)
	if err != nil {
		if ctx.Err() != nil {
			ollama.Reason = "timeout"
		}
		return append(rows, ollama)
	}
	if !containers["dokja-ollama"].Running {
		ollama.Reason = "service_stopped"
		return append(rows, ollama)
	}
	raw, err := p.Compose.Run(ctx, "exec", "-T", "dokja-ollama", "ollama", "list")
	if err != nil {
		ollama.Reason = "inspection_failed"
		if ctx.Err() != nil {
			ollama.Reason = "timeout"
		}
		return append(rows, ollama)
	}
	names, truncated, ok := parseModelList(raw)
	if !ok {
		ollama.Reason = "invalid_response"
		return append(rows, ollama)
	}
	ollama.State, ollama.Reason, ollama.Models, ollama.Truncated = "reachable", "", names, truncated
	return append(rows, ollama)
}

// The installed CLI's human-readable table is version-sensitive. Reject an
// unknown header/row instead of inventing model names from diagnostics.
var listedModelID = regexp.MustCompile(`^[0-9a-f]{12,64}$`)

func parseModelList(raw []byte) ([]string, bool, bool) {
	if len(raw) > 64*1024 {
		return nil, false, false
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) == 0 || strings.Join(strings.Fields(lines[0]), " ") != "NAME ID SIZE MODIFIED" {
		return nil, false, false
	}
	names := []string{}
	truncated := false
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 || !listedModelID.MatchString(fields[1]) {
			return nil, false, false
		}
		if len(names) < 64 {
			names = append(names, fields[0])
		} else {
			truncated = true
		}
	}
	return names, truncated, true
}
