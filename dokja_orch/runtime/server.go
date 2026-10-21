// Package runtime is the bootstrap orchestrator for local container lifecycle.
// It routes a restricted envelope to the Runtime domain, independently of the
// main conversational orchestrator whose container it may need to start.
package runtime

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	domain "read_books/dokja_domain/dokja_runtime"
)

type Event struct {
	EventID   string         `json:"event_id"`
	Timestamp time.Time      `json:"timestamp"`
	Source    string         `json:"source"`
	Type      string         `json:"type"`
	User      map[string]any `json:"user"`
	Channel   map[string]any `json:"channel"`
	Payload   map[string]any `json:"payload"`
	Context   map[string]any `json:"context"`
}

func Handler(d *domain.Domain, project string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		reply := func(status string, result any, message string) {
			json.NewEncoder(w).Encode(map[string]any{"version": 1, "project_id": project, "status": status, "result": map[string]any{"domain": "runtime", "result": result}, "error": message})
		}
		if r.Method != http.MethodPost || r.URL.Path != "/request" || r.Header.Get("Origin") != "" || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			w.WriteHeader(http.StatusBadRequest)
			reply("error", nil, "invalid local runtime request")
			return
		}
		var e Event
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
		if decoder.Decode(&e) != nil || e.Source != "desktop" {
			w.WriteHeader(http.StatusBadRequest)
			reply("error", nil, "invalid event envelope")
			return
		}
		switch e.Type {
		case "runtime.modules":
			reply("ok", d.List(r.Context()), "")
		case "runtime.set":
			name, ok := e.Payload["module"].(string)
			enabled, flag := e.Payload["enabled"].(bool)
			if !ok || !flag {
				reply("error", nil, "module and enabled are required")
				return
			}
			result, err := d.Set(name, enabled)
			if err != nil {
				reply("error", nil, err.Error())
				return
			}
			reply("ok", result, "")
		default:
			reply("error", nil, "unsupported local runtime event")
		}
	})
}
