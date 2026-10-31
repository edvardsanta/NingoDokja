package core

import "context"

type replyPreviewKey struct{}

// WithReplyPreview asks the workflow that handles the event to show its reply while it is written:
// preview receives the text so far. A preview is never the reply, which the workflow still returns
// and handles as it always does. Only the ingress that can deliver previews to the requester sets it.
func WithReplyPreview(ctx context.Context, preview func(string)) context.Context {
	return context.WithValue(ctx, replyPreviewKey{}, preview)
}

// ReplyPreview is the preview the requester asked for, or nil.
func ReplyPreview(ctx context.Context) func(string) {
	preview, _ := ctx.Value(replyPreviewKey{}).(func(string))
	return preview
}

// SendState is where an accepted send stands. It is reported by the orchestrator, which owns the order,
// and is independent of any client or provider: queued waits behind earlier turns, dispatched has gone
// on to be answered. Neither proves a reply or an effect.
type SendState string

const (
	SendQueued     SendState = "queued"
	SendDispatched SendState = "dispatched"
)

type sendProgressKey struct{}

// WithSendProgress asks the workflow to report where the send stands. Only the ingress that can
// deliver it to the requester sets it.
func WithSendProgress(ctx context.Context, progress func(SendState)) context.Context {
	return context.WithValue(ctx, sendProgressKey{}, progress)
}

// SendProgress is the report the requester asked for, or nil.
func SendProgress(ctx context.Context) func(SendState) {
	progress, _ := ctx.Value(sendProgressKey{}).(func(SendState))
	return progress
}

type voiceSpeechKey struct{}

// WithVoiceSpeech carries complete, usable speech segments from the dedicated
// voice generator to the live voice request. It is separate from chat previews.
func WithVoiceSpeech(ctx context.Context, speech func(string)) context.Context {
	return context.WithValue(ctx, voiceSpeechKey{}, speech)
}

func VoiceSpeech(ctx context.Context) func(string) {
	speech, _ := ctx.Value(voiceSpeechKey{}).(func(string))
	return speech
}
