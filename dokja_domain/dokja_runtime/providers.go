package runtime

import (
	"context"
	"sort"
	"time"
)

// ProviderObservation describes discovery only, never successful inference,
// authentication or authorization. Infrastructure may not supply display names.
type ProviderObservation struct {
	ID        string
	State     string
	Reason    string
	Models    []string
	Truncated bool
}

type ProviderInspector interface {
	Inspect(context.Context) []ProviderObservation
}

type ProviderRow struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	State           string   `json:"state"`
	Reason          string   `json:"reason,omitempty"`
	Models          []string `json:"models"`
	ModelDiscovery  string   `json:"model_discovery"`
	InferenceStatus string   `json:"inference_status"`
	Truncated       bool     `json:"models_truncated,omitempty"`
}

type ProvidersView struct {
	Providers []ProviderRow `json:"providers"`
	CheckedAt time.Time     `json:"checked_at"`
}

func (d *Domain) WithProviders(inspector ProviderInspector) *Domain {
	d.providers = inspector
	return d
}

// Providers is an on-demand inventory, independent of cognitive inference or
// model selection. Reading it never requests an inference or starts a module.
func (d *Domain) Providers(ctx context.Context) ProvidersView {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	observations := map[string]ProviderObservation{}
	if d.providers != nil {
		for _, o := range d.providers.Inspect(ctx) {
			observations[o.ID] = o
		}
	}
	view := ProvidersView{Providers: []ProviderRow{}, CheckedAt: time.Now().UTC()}
	for _, spec := range []struct{ id, name string }{{"codex", "Codex"}, {"claude_code", "Claude Code"}, {"ollama", "Ollama"}} {
		row := ProviderRow{ID: spec.id, Name: spec.name, State: "unavailable", Reason: "inspection_unavailable", Models: []string{}, ModelDiscovery: "unavailable", InferenceStatus: "not_tested"}
		o, ok := observations[spec.id]
		if ok {
			allowedReason := map[string]bool{"executable_missing": true, "service_stopped": true, "engine_unavailable": true, "inspection_failed": true, "invalid_response": true, "timeout": true}
			if allowedReason[o.Reason] {
				row.Reason = o.Reason
			}
			if spec.id != "ollama" && o.State == "installed" {
				row.State, row.Reason, row.ModelDiscovery = "installed", "", "unsupported"
			} else if spec.id == "ollama" && o.State == "reachable" {
				row.State, row.Reason, row.ModelDiscovery = "reachable", "", "listed"
				seen := map[string]bool{}
				for _, name := range o.Models {
					if !modelName.MatchString(name) {
						row.State, row.Reason, row.ModelDiscovery = "unavailable", "invalid_response", "unavailable"
						row.Models = []string{}
						break
					}
					if !seen[name] {
						seen[name] = true
						if len(row.Models) < 64 {
							row.Models = append(row.Models, name)
						} else {
							row.Truncated = true
						}
					}
				}
				row.Truncated = row.Truncated || o.Truncated
				sort.Strings(row.Models)
			}
		}
		view.Providers = append(view.Providers, row)
	}
	return view
}
