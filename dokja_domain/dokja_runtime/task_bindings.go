package runtime

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var taskModelSize = regexp.MustCompile(`(?i):([0-9]+(?:\.[0-9]+)?)b(?:$|[-_])`)

// TaskBindings selects registered models for bounded tasks without changing the chat binding.
func (d *Domain) TaskBindings(profile string) ([]ChatBinding, error) {
	if profile != "lightweight" {
		return nil, fmt.Errorf("unsupported inference task profile")
	}
	if d.providerSettings == nil {
		return nil, fmt.Errorf("provider configuration is unavailable")
	}
	preferences, err := d.providerSettings.ReadProviders()
	if err != nil {
		return nil, err
	}
	bindings := []ChatBinding{}
	for _, spec := range providerSpecs {
		for _, registration := range preferences.Providers {
			if registration.ID != spec.id || !registration.Enabled || len(registration.Models) == 0 {
				continue
			}
			model := registration.Models[0]
			for _, candidate := range registration.Models[1:] {
				if lightweightModelRank(candidate) < lightweightModelRank(model) {
					model = candidate
				}
			}
			bindings = append(bindings, ChatBinding{Provider: registration.ID, Model: model})
		}
	}
	if len(bindings) == 0 {
		return nil, fmt.Errorf("no task models are registered in enabled providers")
	}
	return bindings, nil
}

func lightweightModelRank(model string) float64 {
	name := strings.ToLower(model)
	if strings.Contains(name, "haiku") || strings.Contains(name, "luna") {
		return 0
	}
	size := 1000.0
	if match := taskModelSize.FindStringSubmatch(name); len(match) == 2 {
		size, _ = strconv.ParseFloat(match[1], 64)
	}
	if strings.Contains(name, "qwen") {
		return size
	}
	return size + 1000
}
