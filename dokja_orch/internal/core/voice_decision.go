package core

import "context"

type voiceDecisionKey struct{}

// WithVoiceDecision reports conversational eligibility before inference.
// Only the authenticated voice ingress installs this transport callback.
func WithVoiceDecision(ctx context.Context, decision func(bool)) context.Context {
	return context.WithValue(ctx, voiceDecisionKey{}, decision)
}
func ReportVoiceDecision(ctx context.Context, respond bool) {
	if report, ok := ctx.Value(voiceDecisionKey{}).(func(bool)); ok {
		report(respond)
	}
}
