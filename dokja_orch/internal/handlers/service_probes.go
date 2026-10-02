package handlers

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// HealthChecker is a service that answers a plain health request (book).
type HealthChecker interface {
	Health(ctx context.Context) error
}

// ServiceStatusReader is a service that answers a status request (knowledge, memory).
type ServiceStatusReader interface {
	Status(ctx context.Context) (map[string]any, error)
}

// ServiceProbe checks one service. A nil error means it answers; detail is a short note
// shown beside the ok ("" when there is nothing to add).
type ServiceProbe func(ctx context.Context) (detail string, err error)

// HealthProbe probes a service through its health request.
func HealthProbe(checker HealthChecker) ServiceProbe {
	return func(ctx context.Context) (string, error) {
		return "", checker.Health(ctx)
	}
}

// StatusProbe probes a service through its status request. A service that is up but
// cannot embed (Ollama down, or embeddings switched off) still answers ok, with a note.
func StatusProbe(reader ServiceStatusReader) ServiceProbe {
	return StatusProbeWith(reader, embedderNote)
}

// StatusProbeWith is StatusProbe with the note a service adds to its own ok.
func StatusProbeWith(reader ServiceStatusReader, note func(status map[string]any) string) ServiceProbe {
	return func(ctx context.Context) (string, error) {
		result, err := reader.Status(ctx)
		if err != nil {
			return "", err
		}
		return note(result), nil
	}
}

// feedsNote says what the feeds service cannot say by being up: that nothing is following
// anything yet, or that some of the plugins it follows are failing. Only counts are read.
func feedsNote(status map[string]any) string {
	enabled, _ := status["enabled"].(float64)
	failed, _ := status["failed"].(float64)
	switch {
	case enabled == 0:
		return "no plugins enabled"
	case failed > 0:
		return fmt.Sprintf("%d of %d plugins failing", int(failed), int(enabled))
	}
	return ""
}

// FeedsProbe probes the feeds service through its status request.
func FeedsProbe(reader ServiceStatusReader) ServiceProbe {
	return StatusProbeWith(reader, feedsNote)
}

// embedderNote reads the embedder fields the knowledge and memory services share. Only
// the fields are read; nothing a user wrote is in a status reply.
func embedderNote(status map[string]any) string {
	if on, ok := status["embeddings"].(bool); ok && !on {
		return "embeddings off"
	}
	if reachable, ok := status["embedder_reachable"].(bool); ok && !reachable {
		return "embedder unreachable"
	}
	return ""
}

// WithServiceProbes adds health probes for the services keyed by their switch name
// (see core.KnownServices). Services without a probe are reported as unchecked.
func (h *SystemDomainHandler) WithServiceProbes(probes map[string]ServiceProbe) *SystemDomainHandler {
	h.probes = probes
	return h
}

// probeServices runs every probe of an enabled service at once, so a few services that
// are down cost one requester timeout instead of one each. It fills services and reports
// whether any probe failed. A switched-off service is not probed.
func (h *SystemDomainHandler) probeServices(ctx context.Context, services map[string]any) (failed bool) {
	names := make([]string, 0, len(h.probes))
	for name, probe := range h.probes {
		if probe != nil && h.serviceEnabled(name) {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	entries := make([]map[string]any, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Add(1)
		go func() {
			defer wg.Done()
			detail, err := h.probes[name](ctx)
			if err != nil {
				entries[i] = map[string]any{"status": "error", "error": err.Error()}
				return
			}
			entries[i] = map[string]any{"status": "ok"}
			if detail != "" {
				entries[i]["detail"] = detail
			}
		}()
	}
	wg.Wait()

	for i, name := range names {
		services[name] = entries[i]
		if entries[i]["status"] == "error" {
			failed = true
		}
	}
	return failed
}
