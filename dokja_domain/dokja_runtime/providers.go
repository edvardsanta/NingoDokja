package runtime

import (
	"context"
	"regexp"
	"sort"
	"time"
)

var modelSize = regexp.MustCompile(`^[0-9]{1,6}(\.[0-9]{1,2})? (B|KB|MB|GB|TB)$`)

// ProviderObservation describes discovery only, never successful inference,
// authentication or authorization. Infrastructure may not supply display names.
type ProviderObservation struct {
	ID     string
	State  string
	Reason string
	Models []string
	// Sizes are the engine's own figures, such as "4.7 GB", by model name.
	Sizes     map[string]string
	Truncated bool
}

type ProviderInspector interface {
	Inspect(context.Context, map[string]bool) []ProviderObservation
}

type ProviderRow struct {
	Enabled         bool     `json:"enabled"`
	DeclaredModels  []string `json:"declared_models"`
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	State           string   `json:"state"`
	Reason          string   `json:"reason,omitempty"`
	Models          []string `json:"models"`
	ModelDiscovery  string   `json:"model_discovery"`
	InferenceStatus string   `json:"inference_status"`
	Truncated       bool     `json:"models_truncated,omitempty"`
	// Only on the local engine: each listed model's size, and the model memory and knowledge embed
	// with, which is no chat model.
	ModelSizes     map[string]string `json:"model_sizes,omitempty"`
	EmbeddingModel string            `json:"embedding_model,omitempty"`
}

type ProvidersView struct {
	Revision  uint64        `json:"configuration_revision,omitempty"`
	Error     string        `json:"error,omitempty"`
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
	preferences, _ := DecodeProviderPreferences(1, DefaultProviderValues())
	revision := uint64(0)
	if d.providerSettings != nil {
		var err error
		preferences, err = d.providerSettings.ReadProviders()
		if err != nil {
			return ProvidersView{Providers: []ProviderRow{}, Error: "cannot read provider configuration", CheckedAt: time.Now().UTC()}
		}
		revision = preferences.Revision
	}
	configured := map[string]ProviderRegistration{}
	enabled := map[string]bool{}
	for _, p := range preferences.Providers {
		configured[p.ID] = p
		enabled[p.ID] = p.Enabled
	}
	observations := map[string]ProviderObservation{}
	if d.providers != nil {
		for _, o := range d.providers.Inspect(ctx, enabled) {
			observations[o.ID] = o
		}
	}
	view := ProvidersView{Revision: revision, Providers: []ProviderRow{}, CheckedAt: time.Now().UTC()}
	for _, spec := range providerSpecs {
		row := ProviderRow{Enabled: configured[spec.id].Enabled, DeclaredModels: configured[spec.id].Models, ID: spec.id, Name: spec.name, State: "unavailable", Reason: "inspection_unavailable", Models: []string{}, ModelDiscovery: "unavailable", InferenceStatus: "not_tested"}
		o, ok := observations[spec.id]
		if !row.Enabled {
			row.Reason = "disabled"
			ok = false
		}
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
							if size := o.Sizes[name]; modelSize.MatchString(size) {
								if row.ModelSizes == nil {
									row.ModelSizes = map[string]string{}
								}
								row.ModelSizes[name] = size
							}
						} else {
							row.Truncated = true
						}
					}
				}
				row.Truncated = row.Truncated || o.Truncated
				sort.Strings(row.Models)
			}
		}
		// Named as the engine lists it, so a screen compares names as they are.
		if spec.id == "ollama" && d.models != nil {
			row.EmbeddingModel = d.models.engine.EmbeddingModel()
			for _, name := range row.Models {
				if SameModel(name, row.EmbeddingModel) {
					row.EmbeddingModel = name
				}
			}
		}
		view.Providers = append(view.Providers, row)
	}
	return view
}
