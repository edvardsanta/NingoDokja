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
