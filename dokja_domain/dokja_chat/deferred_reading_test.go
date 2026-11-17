package chat

import "testing"

func TestDeferredReadingReasonOnlyAcceptsTransientFailures(t *testing.T) {
	for _, transient := range []string{"timeout", "blocked", "unavailable"} {
		if !DeferredReadingReason(transient) {
			t.Fatalf("%s should defer", transient)
		}
	}
	for _, permanent := range []string{"", "unreadable", "pdf_unreadable", "too_large", "reader_disabled"} {
		if DeferredReadingReason(permanent) {
			t.Fatalf("%s must not defer", permanent)
		}
	}
}
