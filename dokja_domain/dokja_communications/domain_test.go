package communications

import (
	"context"
	"testing"
)

type fakeService struct {
	calls   int
	action  string
	payload map[string]any
}

func (f *fakeService) Dispatch(_ context.Context, action string, payload map[string]any) (map[string]any, error) {
	f.calls++
	f.action = action
	f.payload = payload
	return map[string]any{"ok": true}, nil
}
func TestHistoryOnlyForConfiguredChannels(t *testing.T) {
	f := &fakeService{}
	d := New(f, "123,456,123,invalid")
	for _, payload := range []map[string]any{{"channel_id": "789"}, {"channel_id": "123", "before": "../token"}} {
		if _, err := d.Handle(context.Background(), ActionHistory, payload); err == nil {
			t.Fatal("accepted invalid history")
		}
	}
	if f.calls != 0 {
		t.Fatal("invalid read reached service")
	}
	_, err := d.Handle(context.Background(), ActionHistory, map[string]any{"channel_id": "123", "before": "50", "limit": 999, "token": "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if f.payload["limit"] != 30 || f.payload["before"] != "50" || len(f.payload) != 3 {
		t.Fatalf("unexpected request: %v", f.payload)
	}
}
func TestEmptyConfigurationAndUnknownActions(t *testing.T) {
	f := &fakeService{}
	d := New(f, "")
	answer, err := d.Handle(context.Background(), ActionChannels, nil)
	if err != nil || len(answer["channels"].([]any)) != 0 || f.calls != 0 {
		t.Fatal("empty configuration must not query service")
	}
	if _, err = d.Handle(context.Background(), "unknown", nil); err == nil {
		t.Fatal("unknown action accepted")
	}
}
