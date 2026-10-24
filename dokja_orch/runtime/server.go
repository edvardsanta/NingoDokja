// Package runtime is the bootstrap orchestrator for local container lifecycle.
// It routes a restricted envelope to the Runtime domain, independently of the
// main conversational orchestrator whose container it may need to start.
package runtime

import (
	"encoding/json"
	"math"
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

// controllers are the local interfaces that may manage modules. All of them reach the
// daemon through the same private socket, so each gets the same control.
var controllers = map[string]bool{"desktop": true, "cli": true, "tui": true}

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
		if decoder.Decode(&e) != nil || !controllers[e.Source] {
			w.WriteHeader(http.StatusBadRequest)
			reply("error", nil, "invalid event envelope")
			return
		}
		switch e.Type {
		case "runtime.modules":
			reply("ok", d.List(r.Context()), "")
		case "runtime.logs":
			name, _ := e.Payload["module"].(string)
			result, err := d.Tail(r.Context(), name)
			if err != nil {
				reply("error", nil, err.Error())
				return
			}
			reply("ok", result, "")
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
		case "runtime.chat":
			result, err := d.ChatBinding()
			if err != nil {
				reply("error", nil, err.Error())
				return
			}
			reply("ok", result, "")
		case "runtime.chat.configure":
			rev, ok := e.Payload["configuration_revision"].(float64)
			p, pok := e.Payload["provider"].(string)
			m, mok := e.Payload["model"].(string)
			if !ok || rev < 1 || rev >= float64(domain.MaxProviderRevision) || math.Trunc(rev) != rev || !pok || !mok {
				reply("error", nil, "invalid chat configuration request")
				return
			}
			result, err := d.ConfigureChat(domain.ChatBinding{Revision: uint64(rev), Provider: p, Model: m})
			if err != nil {
				reply("error", nil, err.Error())
				return
			}
			reply("ok", result, "")
		case "runtime.providers":
			view := d.Providers(r.Context())
			if view.Error != "" {
				reply("error", nil, view.Error)
				return
			}
			reply("ok", view, "")
		case "runtime.providers.configure":
			revision, valid := e.Payload["configuration_revision"].(float64)
			id, idOK := e.Payload["provider"].(string)
			enabled, enabledOK := e.Payload["enabled"].(bool)
			raw, modelsOK := e.Payload["models"].([]any)
			if !valid || revision < 1 || revision >= float64(domain.MaxProviderRevision) || math.Trunc(revision) != revision || !idOK || !enabledOK || !modelsOK || len(raw) > domain.MaxProviderModels {
				reply("error", nil, "invalid provider configuration request")
				return
			}
			models := []string{}
			for _, v := range raw {
				name, ok := v.(string)
				if !ok {
					reply("error", nil, "declared models must be names")
					return
				}
				models = append(models, name)
			}
			result, err := d.ConfigureProvider(uint64(revision), domain.ProviderRegistration{ID: id, Enabled: enabled, Models: models})
			if err != nil {
				reply("error", nil, err.Error())
				return
			}
			reply("ok", result, "")
		case "runtime.models.status", "runtime.models.cancel", "runtime.models.engine.start":
			var result domain.ModelsStatus
			var err error
			switch e.Type {
			case "runtime.models.status":
				result, err = d.ModelsStatus()
			case "runtime.models.cancel":
				result, err = d.CancelPull()
			default:
				result, err = d.StartEngine()
			}
			if err != nil {
				reply("error", nil, err.Error())
				return
			}
			reply("ok", result, "")
		case "runtime.models.pull", "runtime.models.remove":
			model, ok := e.Payload["model"].(string)
			if !ok {
				reply("error", nil, "model is required")
				return
			}
			var result domain.ModelsStatus
			var err error
			if e.Type == "runtime.models.pull" {
				result, err = d.PullModel(r.Context(), model)
			} else {
				result, err = d.RemoveModel(r.Context(), model)
			}
			if err != nil {
				reply("error", nil, err.Error())
				return
			}
			reply("ok", result, "")
		case "runtime.settings":
			reply("ok", d.Settings(), "")
		case "runtime.settings.set":
			raw, ok := e.Payload["values"].(map[string]any)
			if !ok || len(raw) == 0 {
				reply("error", nil, "values are required")
				return
			}
			values := map[string]string{}
			for id, value := range raw {
				text, ok := value.(string)
				if !ok {
					reply("error", nil, "every value must be text")
					return
				}
				values[id] = text
			}
			result, err := d.SetSettings(values)
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
