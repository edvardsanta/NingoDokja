package clients

import (
	"context"
	"fmt"
	"sort"
)

// FeedsAudioSources reaches the music sources the owner installed as Feeds plugins: it lists the
// enabled providers of one capability, by id, and invokes one of them by name (several enabled
// providers of a capability are ambiguous to Feeds unless one is named).
type FeedsAudioSources struct {
	feeds      *FeedsServiceClient
	capability string
}

func NewFeedsAudioSources(feeds *FeedsServiceClient, capability string) *FeedsAudioSources {
	return &FeedsAudioSources{feeds: feeds, capability: capability}
}

func (s *FeedsAudioSources) AudioProviders(ctx context.Context) ([]string, error) {
	status, err := s.feeds.Status(ctx)
	if err != nil {
		return nil, err
	}
	rows, _ := status["providers"].([]any)
	var ids []string
	for _, row := range rows {
		provider, _ := row.(map[string]any)
		id, _ := provider["id"].(string)
		if enabled, _ := provider["enabled"].(bool); !enabled || id == "" {
			continue
		}
		provides, _ := provider["provides"].([]any)
		for _, capability := range provides {
			if capability == s.capability {
				ids = append(ids, id)
				break
			}
		}
	}
	sort.Strings(ids)
	return ids, nil
}

func (s *FeedsAudioSources) InvokeAudio(ctx context.Context, plugin, action string, input map[string]any) (map[string]any, error) {
	if s.feeds == nil {
		return nil, fmt.Errorf("feeds service requester factory is not configured")
	}
	return s.feeds.send(ctx, s.feeds.extractFactory, "feeds.invoke", map[string]any{
		"capability": s.capability, "action": action, "input": input, "plugin": plugin,
	})
}
