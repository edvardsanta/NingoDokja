package handlers

import (
	"context"
	"errors"
	"testing"
)

// receiptOrderMarker verifies that receipt writes follow actual transport success.
type receiptOrderMarker struct {
	deliverer *fakeDiscordDeliverer
	urls      []string
	err       error
}

func (m *receiptOrderMarker) MarkSent(_ context.Context, url string) error {
	for _, delivery := range m.deliverer.deliveries {
		if delivery.attachmentURL == url {
			m.urls = append(m.urls, url)
			return m.err
		}
	}
	return errors.New("receipt preceded delivery")
}

func TestScheduledMediaReceiptsRequireAConfirmedDestination(t *testing.T) {
	for _, tc := range []struct {
		name       string
		safe       bool
		safeOnly   string
		channels   string
		fail       string
		failure    error
		wantMarked bool
	}{
		{"confirmed", true, "", "one", "", nil, true},
		{"blocked", false, "one", "one", "", nil, false},
		{"failed", true, "", "one", "one", errors.New("delivery refused"), false},
		{"unknown", true, "", "one", "one", context.DeadlineExceeded, false},
		{"partial success", true, "", "one,two", "one", errors.New("delivery refused"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fetcher := &fakeMemeFetcher{result: map[string]any{"memes": []any{map[string]any{
				"url": "https://media.example/a.png", "title": "media", "nsfw": map[string]any{"safe": tc.safe},
			}}}}
			deliverer := &channelFailingDeliverer{failChannel: tc.fail, err: tc.failure}
			marker := &receiptOrderMarker{deliverer: &deliverer.fakeDiscordDeliverer}
			handler := NewSystemDomainHandler(nil, nil, fetcher, deliverer, tc.channels).
				WithSafeOnlyChannels(tc.safeOnly).WithMemeTools(nil, marker)
			event, step := scheduledMemeEvent()
			_, err := handler.Handle(context.Background(), event, step)
			if (tc.failure != nil) != (err != nil) || (len(marker.urls) == 1) != tc.wantMarked {
				t.Fatalf("err=%v receipts=%v confirmed=%v", err, marker.urls, deliverer.deliveries)
			}
		})
	}
}

func TestManualPartialDeliveryKeepsItsConfirmedReceipt(t *testing.T) {
	deliverer := &channelFailingDeliverer{failChannel: "failed", err: context.DeadlineExceeded}
	marker := &receiptOrderMarker{deliverer: &deliverer.fakeDiscordDeliverer}
	handler := NewSystemDomainHandler(nil, nil, nil, deliverer, "confirmed,failed").WithMemeTools(nil, marker)
	event, step := discordSendEvent(map[string]any{
		"all": true, "attachment_url": "https://media.example/a.png", "mark_sent": true, "skip_hashtag": true,
	})
	_, err := handler.Handle(context.Background(), event, step)
	if !errors.Is(err, context.DeadlineExceeded) || len(marker.urls) != 1 || deliverer.attempts["confirmed"] != 1 || deliverer.attempts["failed"] != 1 {
		t.Fatalf("partial outcome lost: err=%v receipts=%v attempts=%v", err, marker.urls, deliverer.attempts)
	}
}

func TestScheduledReceiptFailureDoesNotImmediatelyResend(t *testing.T) {
	fetcher := &fakeMemeFetcher{result: map[string]any{"memes": []any{map[string]any{
		"url": "https://media.example/a.png", "title": "media",
	}}}}
	deliverer := &fakeDiscordDeliverer{}
	marker := &receiptOrderMarker{deliverer: deliverer, err: errors.New("storage unavailable")}
	handler := NewSystemDomainHandler(nil, nil, fetcher, deliverer, "one").WithMemeTools(nil, marker)
	event, step := scheduledMemeEvent()
	result, err := handler.Handle(context.Background(), event, step)
	if err != nil || result["delivered_count"] != 1 || len(deliverer.deliveries) != 1 || len(marker.urls) != 1 {
		t.Fatalf("receipt failure repeated delivery: result=%v err=%v deliveries=%v receipts=%v", result, err, deliverer.deliveries, marker.urls)
	}
}
