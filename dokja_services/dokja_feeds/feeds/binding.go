package feeds

import (
	"fmt"
	"sort"
	"strings"
)

// Binding chooses which plugin serves one capability key, such as document.extract.v1 for
// ".pdf" (ADR 0002). It is resolved when the host is composed; directory order and
// package versions never decide.
type Binding struct {
	Capability string
	Key        string
	Plugin     string
	Fallback   []string
}

// Selection is the resolved binding. Reason is empty when Primary is set; otherwise it
// starts with "unavailable:" or "ambiguous:" and nothing may be invoked for the key.
type Selection struct {
	Primary  *Plugin
	Fallback []*Plugin
	Reason   string
}

func (p Plugin) serves(capability, key string) bool {
	if !p.Enabled || !p.provides(capability) {
		return false
	}
	if capability != CapabilityDocumentExtract {
		return true
	}
	for _, format := range p.Formats {
		if format == key {
			return true
		}
	}
	return false
}

func Select(found []Found, binding Binding) Selection {
	byID := map[string]*Plugin{}
	var eligible []string
	for _, f := range found {
		if f.Plugin == nil {
			continue
		}
		byID[f.Plugin.ID] = f.Plugin
		if f.Plugin.serves(binding.Capability, binding.Key) {
			eligible = append(eligible, f.Plugin.ID)
		}
	}
	sort.Strings(eligible)
	pick := func(id string) (*Plugin, string) {
		plugin, ok := byID[id]
		if !ok || !plugin.serves(binding.Capability, binding.Key) {
			return nil, fmt.Sprintf("unavailable: %q is not an enabled provider of %s for %q", id, binding.Capability, binding.Key)
		}
		return plugin, ""
	}

	var selection Selection
	switch {
	case binding.Plugin != "":
		plugin, reason := pick(binding.Plugin)
		if reason != "" {
			return Selection{Reason: reason}
		}
		selection.Primary = plugin
	case len(eligible) == 0:
		return Selection{Reason: fmt.Sprintf("unavailable: no enabled provider of %s for %q", binding.Capability, binding.Key)}
	case len(eligible) > 1:
		return Selection{Reason: fmt.Sprintf("ambiguous: %s for %q is provided by %s; bind one explicitly",
			binding.Capability, binding.Key, strings.Join(eligible, ", "))}
	default:
		selection.Primary = byID[eligible[0]]
	}
	seen := map[string]bool{selection.Primary.ID: true}
	for _, id := range binding.Fallback {
		if seen[id] {
			return Selection{Reason: fmt.Sprintf("unavailable: fallback %q is listed twice or is the primary", id)}
		}
		seen[id] = true
		plugin, reason := pick(id)
		if reason != "" {
			return Selection{Reason: reason}
		}
		selection.Fallback = append(selection.Fallback, plugin)
	}
	return selection
}
