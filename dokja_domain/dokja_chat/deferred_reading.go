package chat

// DeferredReadingReason reports whether a failed reading reason is transient:
// a later attempt may succeed, so the conversation can defer the reading and
// come back with the result. Permanent refusals and invalid readings are not:
// retrying them unchanged wastes a follow-up the application promised.
func DeferredReadingReason(reason string) bool {
	switch reason {
	case "timeout", "blocked", "unavailable":
		return true
	}
	return false
}
