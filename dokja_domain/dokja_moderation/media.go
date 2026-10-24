package moderation

import "context"

// MediaScreener supplies a provider observation. Only an explicit boolean safe
// value can establish admissibility for a destination that requires screening.
type MediaScreener interface {
	Screen(ctx context.Context, url, caption string) (map[string]any, error)
}

type MediaDecision struct {
	Safe   bool
	Reason string
}

// MediaPolicy owns the fail-closed interpretation of media observations. It
// does not select recipients or authorize a screening bypass.
type MediaPolicy struct {
	Screener MediaScreener
	Enabled  bool
}

func (p MediaPolicy) Screen(ctx context.Context, url, caption string) MediaDecision {
	if p.Screener == nil {
		return MediaDecision{Reason: "no screener configured"}
	}
	if !p.Enabled {
		return MediaDecision{Reason: "meme service is disabled, so the image cannot be screened"}
	}
	result, err := p.Screener.Screen(ctx, url, caption)
	if err != nil {
		return MediaDecision{Reason: "screen failed: " + err.Error()}
	}
	if safe, _ := result["safe"].(bool); safe {
		return MediaDecision{Safe: true}
	}
	reason, _ := result["reason"].(string)
	if reason == "" {
		reason = "flagged by the nsfw screen"
	}
	return MediaDecision{Reason: reason}
}

// AssessMediaLabel interprets the cached observation attached to a collected
// item. Preserve its reason verbatim; a missing or malformed label is unsafe.
func AssessMediaLabel(label map[string]any) MediaDecision {
	safe, ok := label["safe"].(bool)
	if !ok {
		return MediaDecision{Reason: "no nsfw label"}
	}
	reason, _ := label["reason"].(string)
	return MediaDecision{Safe: safe, Reason: reason}
}
