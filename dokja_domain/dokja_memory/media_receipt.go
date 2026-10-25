package memory

import "context"

// MediaReceiptRecorder persists the current pool receipt through an orchestrator
// adapter. The existing storage format remains behind this transitional port.
type MediaReceiptRecorder interface {
	MarkSent(context.Context, string) error
}

type MediaDelivery struct {
	URL       string
	Requested bool
	Confirmed int
}

type MediaReceipt struct {
	Attempted bool
	Marked    bool
}

// MediaReceiptPolicy permits a receipt only after a known successful delivery.
// Failed, blocked and unknown outcomes do not contribute to Confirmed. One
// success suffices even if other destinations failed; this is a media-level
// receipt, not proof of delivery to every configured destination.
type MediaReceiptPolicy struct {
	Recorder MediaReceiptRecorder
	Enabled  bool
}

func (p MediaReceiptPolicy) Record(ctx context.Context, delivery MediaDelivery) (MediaReceipt, error) {
	if !p.Enabled || p.Recorder == nil || !delivery.Requested || delivery.URL == "" || delivery.Confirmed < 1 {
		return MediaReceipt{}, nil
	}
	err := p.Recorder.MarkSent(ctx, delivery.URL)
	return MediaReceipt{Attempted: true, Marked: err == nil}, err
}
