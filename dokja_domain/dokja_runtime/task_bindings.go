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
