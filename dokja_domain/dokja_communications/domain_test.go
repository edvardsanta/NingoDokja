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

type fakeConversations struct {
	listed  []Conversation
	listErr error
	page    map[string]any
	asked   []string
}

func (f *fakeConversations) List(context.Context) ([]Conversation, error) { return f.listed, f.listErr }
func (f *fakeConversations) History(_ context.Context, alias, before string) (map[string]any, error) {
	f.asked = append(f.asked, alias+"|"+before)
	return f.page, nil
}

func TestConversationsJoinTheChannelsWithoutChangingTheDiscordOnes(t *testing.T) {
	f := &fakeService{}
	c := &fakeConversations{listed: []Conversation{{Alias: "friend", Kind: "direct", RetentionDays: 30}}}
	d := New(f, "123").WithConversations(c)

	answer, err := d.Handle(context.Background(), ActionChannels, nil)
	if err != nil {
		t.Fatal(err)
	}

	channels := answer["channels"].([]any)
	if len(channels) != 1 {
		t.Fatalf("fake discord service gives no channels, want only the conversation, got %#v", channels)
	}
	entry := channels[0].(map[string]any)
	if entry["id"] != "whatsapp:friend" || entry["name"] != "friend" || entry["retention_days"] != 30 {
		t.Fatalf("unexpected entry %#v", entry)
	}
	if f.calls != 1 || f.action != "channels" {
		t.Fatal("the configured Discord channels must still be asked for")
	}
}

func TestConversationsListWithoutAnyDiscordConfiguration(t *testing.T) {
	c := &fakeConversations{listed: []Conversation{{Alias: "team", Kind: "group", RetentionDays: 7}}}
	d := New(nil, "").WithConversations(c)

	answer, err := d.Handle(context.Background(), ActionChannels, nil)

	if err != nil || len(answer["channels"].([]any)) != 1 {
		t.Fatalf("expected the conversation alone, got %#v %v", answer, err)
	}
}

func TestAnUnreachableInterfaceDoesNotHideTheDiscordChannels(t *testing.T) {
	f := &fakeService{}
	c := &fakeConversations{listErr: context.DeadlineExceeded}
	d := New(f, "123").WithConversations(c)

	answer, err := d.Handle(context.Background(), ActionChannels, nil)

	if err != nil {
		t.Fatal(err)
	}
	if unavailable, _ := answer["unavailable"].([]any); len(unavailable) != 1 || unavailable[0] != "whatsapp" {
		t.Fatalf("expected the interface to be said unavailable, got %#v", answer["unavailable"])
	}
}

func TestConversationHistoryOnlyForNamedAliasesAndNumericCursors(t *testing.T) {
	f := &fakeService{}
	c := &fakeConversations{
		listed: []Conversation{{Alias: "friend", Kind: "direct", RetentionDays: 30}},
		page:   map[string]any{"messages": []any{map[string]any{"id": "1"}}, "before": "1", "alias": "friend", "extra": "x"},
	}
	d := New(f, "123").WithConversations(c)

	for name, payload := range map[string]map[string]any{
		"not named":       {"channel_id": "whatsapp:stranger"},
		"path in alias":   {"channel_id": "whatsapp:../friend"},
		"empty alias":     {"channel_id": "whatsapp:"},
		"uppercase alias": {"channel_id": "whatsapp:Friend"},
		"bad cursor":      {"channel_id": "whatsapp:friend", "before": "1;drop"},
	} {
		if _, err := d.Handle(context.Background(), ActionHistory, payload); err == nil {
			t.Fatalf("%s: accepted an invalid history request", name)
		}
	}
	if len(c.asked) != 0 || f.calls != 0 {
		t.Fatal("an invalid request reached a history route")
	}

	answer, err := d.Handle(context.Background(), ActionHistory, map[string]any{"channel_id": "whatsapp:friend", "before": "40"})
	if err != nil {
		t.Fatal(err)
	}
	if len(c.asked) != 1 || c.asked[0] != "friend|40" {
		t.Fatalf("unexpected history request %v", c.asked)
	}
	if answer["channel_id"] != "whatsapp:friend" || answer["before"] != "1" {
		t.Fatalf("unexpected answer %#v", answer)
	}
	if _, leaked := answer["extra"]; leaked {
		t.Fatal("only the page is passed on")
	}
	if f.calls != 0 {
		t.Fatal("a WhatsApp conversation must not reach the Discord read service")
	}
}

func TestWithoutConversationsAWhatsAppIdIsNotAChannel(t *testing.T) {
	f := &fakeService{}
	d := New(f, "123")

	if _, err := d.Handle(context.Background(), ActionHistory, map[string]any{"channel_id": "whatsapp:friend"}); err == nil {
		t.Fatal("accepted a conversation that is not configured")
	}
	if f.calls != 0 {
		t.Fatal("reached the Discord service")
	}
}

type failingService struct{}

func (failingService) Dispatch(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, context.DeadlineExceeded
}

func TestADiscordServiceThatIsDownDoesNotHideTheConversations(t *testing.T) {
	c := &fakeConversations{listed: []Conversation{{Alias: "friend", Kind: "direct", RetentionDays: 30}}}
	d := New(failingService{}, "123").WithConversations(c)

	answer, err := d.Handle(context.Background(), ActionChannels, nil)

	if err != nil {
		t.Fatalf("the conversations must still list, got %v", err)
	}
	channels := answer["channels"].([]any)
	if len(channels) != 1 || channels[0].(map[string]any)["id"] != "whatsapp:friend" {
		t.Fatalf("unexpected channels %#v", channels)
	}
	if unavailable, _ := answer["unavailable"].([]any); len(unavailable) != 1 || unavailable[0] != "discord" {
		t.Fatalf("expected Discord to be said unavailable, got %#v", answer["unavailable"])
	}
}

func TestADiscordServiceThatIsDownIsStillAnErrorWhenThereIsNothingElse(t *testing.T) {
	for name, d := range map[string]*Domain{
		"no conversations configured": New(failingService{}, "123"),
		"conversations empty":         New(failingService{}, "123").WithConversations(&fakeConversations{}),
	} {
		if _, err := d.Handle(context.Background(), ActionChannels, nil); err == nil {
			t.Fatalf("%s: a Discord-only setup must keep failing loudly", name)
		}
	}
}

func TestBothSourcesDownIsReportedAsTheDiscordError(t *testing.T) {
	d := New(failingService{}, "123").WithConversations(&fakeConversations{listErr: context.DeadlineExceeded})

	if _, err := d.Handle(context.Background(), ActionChannels, nil); err == nil {
		t.Fatal("with nothing to show, the error stands")
	}
}
