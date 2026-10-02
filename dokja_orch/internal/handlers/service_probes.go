package handlers

import (
	"context"
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
	return func(ctx context.Context) (string, error) {
		result, err := reader.Status(ctx)
		if err != nil {
			return "", err
		}
		return embedderNote(result), nil
	}
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
