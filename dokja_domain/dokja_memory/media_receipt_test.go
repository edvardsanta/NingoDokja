package memory

import (
	"context"
	"errors"
	"testing"
)

type mediaReceiptStub struct {
	urls []string
	err  error
}

func (r *mediaReceiptStub) MarkSent(_ context.Context, url string) error {
	r.urls = append(r.urls, url)
	return r.err
}

func TestMediaReceiptsRequireConfirmedRequestedDelivery(t *testing.T) {
	for _, tc := range []struct {
		name      string
		requested bool
		confirmed int
		url       string
		enabled   bool
		want      bool
	}{
		{"confirmed", true, 1, "https://media.example/a.png", true, true},
		{"several confirmed", true, 2, "https://media.example/a.png", true, true},
		{"blocked failed or unknown", true, 0, "https://media.example/a.png", true, false},
		{"invalid count", true, -1, "https://media.example/a.png", true, false},
		{"not requested", false, 1, "https://media.example/a.png", true, false},
		{"text only", true, 1, "", true, false},
		{"disabled", true, 1, "https://media.example/a.png", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &mediaReceiptStub{}
			result, err := (MediaReceiptPolicy{Recorder: store, Enabled: tc.enabled}).Record(context.Background(), MediaDelivery{
				URL: tc.url, Requested: tc.requested, Confirmed: tc.confirmed,
			})
			if err != nil || result.Attempted != tc.want || result.Marked != tc.want || (len(store.urls) == 1) != tc.want {
				t.Fatalf("receipt=%+v err=%v writes=%v", result, err, store.urls)
			}
		})
	}
}

func TestReceiptFailureDoesNotInventConfirmationOrRetryStorage(t *testing.T) {
	store := &mediaReceiptStub{err: errors.New("receipt unavailable")}
	result, err := (MediaReceiptPolicy{Recorder: store, Enabled: true}).Record(context.Background(), MediaDelivery{
		URL: "https://media.example/a.png", Requested: true, Confirmed: 1,
	})
	if !errors.Is(err, store.err) || !result.Attempted || result.Marked || len(store.urls) != 1 {
		t.Fatalf("receipt=%+v err=%v writes=%v", result, err, store.urls)
	}
	result, err = (MediaReceiptPolicy{Enabled: true}).Record(context.Background(), MediaDelivery{
		URL: "https://media.example/a.png", Requested: true, Confirmed: 1,
	})
	if err != nil || result.Attempted || result.Marked {
		t.Fatalf("missing recorder invented a receipt: %+v %v", result, err)
	}
}
