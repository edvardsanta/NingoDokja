//go:build unix

package feeds

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// invoke is the bounded on-demand protocol for mechanism plugins. Domains keep
// workflow policy; the host only selects a declared provider and runs it.
func (s *Service) invoke(ctx context.Context, payload map[string]any) (map[string]any, error) {
	capability, _ := payload["capability"].(string)
	switch capability {
	case CapabilityDocumentProcess, CapabilityMediaLibrary, CapabilityMediaPerceive, CapabilityAudioResolve:
	default:
		return nil, reasonError(ReasonUnsupported, "capability cannot be invoked")
	}
	action, _ := payload["action"].(string)
	if !idPattern.MatchString(action) {
		return nil, reasonError(ReasonInvalid, "action must be a bounded identifier")
	}
	input, err := json.Marshal(map[string]any{"operation": capability, "action": action, "payload": payload["input"]})
	if err != nil || len(input) > 28<<20 {
		return nil, reasonError(ReasonInvalid, "input is invalid or too large")
	}
	var found []Found
	byID := map[string]*entry{}
	for _, e := range s.entries {
		if e.plugin != nil {
			found = append(found, Found{Dir: e.dir, Plugin: e.plugin})
			byID[e.plugin.ID] = e
		}
	}
	id, _ := payload["plugin"].(string)
	selection := Select(found, Binding{Capability: capability, Key: action, Plugin: id})
	if selection.Primary == nil {
		return nil, fmt.Errorf("%s", selection.Reason)
	}
	e := byID[selection.Primary.ID]
	select {
	case s.extractions <- struct{}{}:
		defer func() { <-s.extractions }()
	case <-ctx.Done():
		return nil, reasonError(ReasonTimeout, "no plugin slot before deadline")
	}
	raw, err := s.runner.Run(ctx, *e.plugin, s.environment(*e.plugin), input)
	var output struct {
		Result map[string]any `json:"result"`
		Error  string         `json:"error"`
	}
	if err != nil {
		err = runFailure(err)
	} else if json.Unmarshal(raw, &output) != nil || output.Result == nil && output.Error == "" {
		err = reasonError(ReasonFailed, "plugin must return a result object or error")
	} else if output.Error != "" {
		err = reasonError(ReasonFailed, "%s", cleanText(output.Error, maxReasonRunes))
	}
	s.recordExtraction(e, err)
	return output.Result, err
}

// refreshSources runs collectors on demand through the same path as their timers.
// It never invokes an extractor or changes a plugin's enabled setting.
func (s *Service) refreshSources(ctx context.Context, payload map[string]any) (map[string]any, error) {
	id, _ := payload["plugin"].(string)
	var selected []*entry
	for _, e := range s.entries {
		if e.plugin != nil && e.plugin.Enabled && e.plugin.Collects() && (id == "" || e.plugin.ID == id) {
			selected = append(selected, e)
		}
	}
	if id != "" && len(selected) == 0 {
		return nil, reasonError(ReasonUnavailable, "no enabled collector %q", cleanText(id, 40))
	}
	var wg sync.WaitGroup
	for _, e := range selected {
		wg.Add(1)
		go func() { defer wg.Done(); s.refresh(ctx, e) }()
	}
	wg.Wait()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	failed := 0
	s.mu.RLock()
	for _, e := range selected {
		if e.lastErr != "" {
			failed++
		}
	}
	s.mu.RUnlock()
	return map[string]any{"status": "refreshed", "collectors": len(selected), "failed": failed}, nil
}
