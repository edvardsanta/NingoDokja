package runtime

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const MaxProviderModels = 16
const MaxProviderRevision uint64 = 9007199254740991

var ErrProviderConflict = errors.New("provider configuration changed; reload before saving")
var providerSpecs = []struct{ id, name string }{{"codex", "Codex"}, {"claude_code", "Claude Code"}, {"ollama", "Ollama"}}

// Registrations declare operator intent. They establish neither model
// availability nor successful inference nor a capability binding.
type ProviderRegistration struct {
	ID      string   `json:"id"`
	Enabled bool     `json:"enabled"`
	Models  []string `json:"models"`
}
type ProviderPreferences struct {
	Revision  uint64
	Providers []ProviderRegistration
}
type ProviderSaved struct {
	Revision uint64 `json:"configuration_revision"`
	Provider string `json:"provider"`
}
type ProviderSettingsStore interface {
	ReadProviders() (ProviderPreferences, error)
	WriteProvider(uint64, ProviderRegistration) (uint64, error)
}

func (d *Domain) WithProviderSettings(store ProviderSettingsStore) *Domain {
	d.providerSettings = store
	return d
}

func DefaultProviderValues() map[string]string {
	values := map[string]string{}
	for _, p := range providerSpecs {
		values[p.id+".enabled"] = "true"
		values[p.id+".models"] = ""
	}
	return values
}
func NormalizeProviderRegistration(p ProviderRegistration) (ProviderRegistration, error) {
	known := false
	for _, s := range providerSpecs {
		if s.id == p.ID {
			known = true
		}
	}
	if !known {
		return ProviderRegistration{}, fmt.Errorf("unsupported provider")
	}
	if len(p.Models) > MaxProviderModels {
		return ProviderRegistration{}, fmt.Errorf("at most %d declared models", MaxProviderModels)
	}
	names := []string{}
	seen := map[string]bool{}
	for _, raw := range p.Models {
		name := strings.TrimSpace(raw)
		if !modelName.MatchString(name) {
			return ProviderRegistration{}, fmt.Errorf("invalid declared model name")
		}
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	sort.Strings(names)
	p.Models = names
	return p, nil
}
func DecodeProviderPreferences(revision uint64, values map[string]string) (ProviderPreferences, error) {
	result := ProviderPreferences{Revision: revision, Providers: []ProviderRegistration{}}
	if revision == 0 || revision > MaxProviderRevision || len(values) != len(providerSpecs)*2 {
		return result, fmt.Errorf("invalid provider configuration")
	}
	for _, s := range providerSpecs {
		enabled, ok := values[s.id+".enabled"]
		models, present := values[s.id+".models"]
		if !ok || !present || (enabled != "true" && enabled != "false") {
			return ProviderPreferences{}, fmt.Errorf("invalid provider configuration")
		}
		names := []string{}
		if models != "" {
			names = strings.Split(models, ",")
		}
		row, err := NormalizeProviderRegistration(ProviderRegistration{ID: s.id, Enabled: enabled == "true", Models: names})
		if err != nil {
			return ProviderPreferences{}, fmt.Errorf("invalid provider configuration")
		}
		result.Providers = append(result.Providers, row)
	}
	return result, nil
}
func ProviderRegistrationValues(p ProviderRegistration) (map[string]string, error) {
	p, err := NormalizeProviderRegistration(p)
	if err != nil {
		return nil, err
	}
	return map[string]string{p.ID + ".enabled": strconv.FormatBool(p.Enabled), p.ID + ".models": strings.Join(p.Models, ",")}, nil
}
func (d *Domain) ConfigureProvider(revision uint64, p ProviderRegistration) (ProviderSaved, error) {
	if d.providerSettings == nil {
		return ProviderSaved{}, fmt.Errorf("provider configuration is unavailable")
	}
	if revision == 0 || revision >= MaxProviderRevision {
		return ProviderSaved{}, fmt.Errorf("invalid provider configuration revision")
	}
	p, err := NormalizeProviderRegistration(p)
	if err != nil {
		return ProviderSaved{}, err
	}
	current, err := d.providerSettings.ReadProviders()
	if err != nil {
		return ProviderSaved{}, err
	}
	if current.Revision != revision {
		return ProviderSaved{}, ErrProviderConflict
	}
	next, err := d.providerSettings.WriteProvider(revision, p)
	if err != nil {
		return ProviderSaved{}, err
	}
	return ProviderSaved{Revision: next, Provider: p.ID}, nil
}
