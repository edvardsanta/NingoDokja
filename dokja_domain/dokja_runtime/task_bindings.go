package runtime

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var taskModelSize = regexp.MustCompile(`(?i):([0-9]+(?:\.[0-9]+)?)b(?:$|[-_])`)

// TaskBindings selects registered models for bounded tasks without changing the chat binding.
type TaskBinding struct {
	ChatBinding
	RetryAfterMS  int64  `json:"retry_after_ms,omitempty"`
	FailureReason string `json:"failure_reason,omitempty"`
	Configured    bool   `json:"configured,omitempty"`
}

func (d *Domain) TaskBindings(profile string) ([]TaskBinding, error) {
	if !validTaskProfile(profile) {
		return nil, fmt.Errorf("unsupported inference task profile")
	}
	if d.providerSettings == nil {
		return nil, fmt.Errorf("provider configuration is unavailable")
	}
	preferences, err := d.providerSettings.ReadProviders()
	if err != nil {
		return nil, err
	}
	bindings := []TaskBinding{}
	profiles, err := d.TaskProfiles()
	if err != nil {
		return nil, err
	}
	if priorities := profiles.Profiles[profile]; len(priorities) > 0 {
		for _, choice := range priorities {
			// Removed or disabled registrations are never silently re-enabled by a profile.
			if d.validateChat(choice) != nil {
				continue
			}
			retry, reason := d.taskCircuit.status(choice.Provider, time.Now())
			bindings = append(bindings, TaskBinding{Configured: true, ChatBinding: ChatBinding{Provider: choice.Provider, Model: choice.Model}, RetryAfterMS: retry, FailureReason: reason})
		}
		if len(bindings) == 0 {
			return nil, fmt.Errorf("no configured task models are enabled")
		}
		return bindings, nil
	}
	for _, spec := range providerSpecs {
		for _, registration := range preferences.Providers {
			if registration.ID != spec.id || !registration.Enabled || len(registration.Models) == 0 {
				continue
			}
			model := registration.Models[0]
			for _, candidate := range registration.Models[1:] {
				if (profile == "lightweight" && lightweightModelRank(candidate) < lightweightModelRank(model)) || (profile == "analysis" && lightweightModelRank(candidate) > lightweightModelRank(model)) {
					model = candidate
				}
			}
			retry, reason := d.taskCircuit.status(registration.ID, time.Now())
			bindings = append(bindings, TaskBinding{ChatBinding: ChatBinding{Provider: registration.ID, Model: model}, RetryAfterMS: retry, FailureReason: reason})
		}
	}
	if len(bindings) == 0 {
		return nil, fmt.Errorf("no task models are registered in enabled providers")
	}
	if chat, err := d.ChatBinding(); err == nil {
		bindings = preferChatProvider(bindings, chat, profile, preferences.Providers)
	}
	return bindings, nil
}

// preferChatProvider puts the provider the owner chose for chat first when no priority list says
// otherwise, instead of the fixed provider order. Analysis tasks also use the chat model itself
// when the provider has it registered; lightweight tasks keep their small model.
func preferChatProvider(bindings []TaskBinding, chat ChatBinding, profile string, registrations []ProviderRegistration) []TaskBinding {
	for i, binding := range bindings {
		if binding.Provider != chat.Provider {
			continue
		}
		if profile == "analysis" {
			for _, registration := range registrations {
				if registration.ID != chat.Provider {
					continue
				}
				for _, model := range registration.Models {
					if SameModel(model, chat.Model) {
						binding.Model = model
					}
				}
			}
		}
		ordered := make([]TaskBinding, 0, len(bindings))
		ordered = append(ordered, binding)
		ordered = append(ordered, bindings[:i]...)
		return append(ordered, bindings[i+1:]...)
	}
	return bindings
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
