package handlers

import (
	"context"
	"encoding/json"
	"errors"
	store "read_books/dokja_store"
	"read_books/internal/core"
	"strings"
	"testing"
	"time"
)

type fakeMemeFetcher struct {
	limit  *int
	result map[string]any
	err    error
}

func (f *fakeMemeFetcher) Fetch(_ context.Context, limit *int) (map[string]any, error) {
	f.limit = limit
	return f.result, f.err
}

type deliveredMessage struct {
	channelID     string
	content       string
	attachmentURL string
}

type fakeDiscordDeliverer struct {
	deliveries []deliveredMessage
	err        error
}

func (f *fakeDiscordDeliverer) Deliver(_ context.Context, channelID, content, attachmentURL string) error {
	if f.err != nil {
		return f.err
	}

	f.deliveries = append(f.deliveries, deliveredMessage{
		channelID:     channelID,
		content:       content,
		attachmentURL: attachmentURL,
	})
	return nil
}

func TestSystemDomainHandlerScheduledDispatchDeliversFetchedMemes(t *testing.T) {
	fetcher := &fakeMemeFetcher{
		result: map[string]any{
			"memes": []any{
				map[string]any{"title": "First meme", "url": "https://example.com/first.jpg"},
				map[string]any{"url": "https://example.com/second.jpg"},
			},
		},
	}
	deliverer := &fakeDiscordDeliverer{}
	handler := NewSystemDomainHandler(nil, nil, fetcher, deliverer, "discord-channel")

	result, err := handler.Handle(context.Background(), core.Event{
		Type:    "meme.dispatch.scheduled",
		Payload: map[string]any{"limit": 2},
	}, core.WorkflowStep{
		Domain: core.DomainSystem,
		Action: "deliver-scheduled-memes",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fetcher.limit == nil || *fetcher.limit != 2 {
		t.Fatalf("expected fetch limit 2, got %#v", fetcher.limit)
	}
	if len(deliverer.deliveries) != 2 {
		t.Fatalf("expected 2 deliveries, got %d", len(deliverer.deliveries))
	}
	if deliverer.deliveries[0].channelID != "discord-channel" {
		t.Fatalf("expected delivery channel discord-channel, got %q", deliverer.deliveries[0].channelID)
	}
	if deliverer.deliveries[0].content != "First meme" {
		t.Fatalf("expected first delivery content to be title, got %q", deliverer.deliveries[0].content)
	}
	if deliverer.deliveries[0].attachmentURL != "https://example.com/first.jpg" {
		t.Fatalf("expected first attachment URL, got %q", deliverer.deliveries[0].attachmentURL)
	}
	if deliverer.deliveries[1].content != "meme agendado" {
		t.Fatalf("expected fallback content for url-only meme, got %q", deliverer.deliveries[1].content)
	}
	if deliverer.deliveries[1].attachmentURL != "https://example.com/second.jpg" {
		t.Fatalf("expected second attachment URL, got %q", deliverer.deliveries[1].attachmentURL)
	}
	if result["delivered_count"] != 2 {
		t.Fatalf("expected delivered_count 2, got %#v", result["delivered_count"])
	}
}

func TestSystemDomainHandlerScheduledDispatchSkipsEmptyMemeEntries(t *testing.T) {
	fetcher := &fakeMemeFetcher{
		result: map[string]any{
			"memes": []any{
				map[string]any{},
				map[string]any{"title": "Valid meme"},
			},
		},
	}
	deliverer := &fakeDiscordDeliverer{}
	handler := NewSystemDomainHandler(nil, nil, fetcher, deliverer, "discord-channel")

	result, err := handler.Handle(context.Background(), core.Event{
		Type:    "meme.dispatch.scheduled",
		Payload: map[string]any{},
	}, core.WorkflowStep{
		Domain: core.DomainSystem,
		Action: "deliver-scheduled-memes",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(deliverer.deliveries) != 1 {
		t.Fatalf("expected 1 delivery, got %d", len(deliverer.deliveries))
	}
	if result["delivered_count"] != 1 {
		t.Fatalf("expected delivered_count 1, got %#v", result["delivered_count"])
	}
}

func TestSystemDomainHandlerScheduledDispatchPropagatesDeliveryFailure(t *testing.T) {
	expected := errors.New("discord is down")
	handler := NewSystemDomainHandler(
		nil,
		nil,
		&fakeMemeFetcher{
			result: map[string]any{
				"memes": []any{
					map[string]any{"title": "First meme", "url": "https://example.com/first.jpg"},
				},
			},
		},
		&fakeDiscordDeliverer{err: expected},
		"discord-channel",
	)

	_, err := handler.Handle(context.Background(), core.Event{
		Type:    "meme.dispatch.scheduled",
		Payload: map[string]any{},
	}, core.WorkflowStep{
		Domain: core.DomainSystem,
		Action: "deliver-scheduled-memes",
	})
	if !errors.Is(err, expected) {
		t.Fatalf("expected delivery error to propagate, got %v", err)
	}
}

type channelFailingDeliverer struct {
	fakeDiscordDeliverer
	failChannel string
	err         error
	attempts    map[string]int
}

func (f *channelFailingDeliverer) Deliver(ctx context.Context, channelID, content, attachmentURL string) error {
	if f.attempts == nil {
		f.attempts = map[string]int{}
	}
	f.attempts[channelID]++
	if channelID == f.failChannel {
		return f.err
	}
	return f.fakeDiscordDeliverer.Deliver(ctx, channelID, content, attachmentURL)
}

func scheduledMemeEvent() (core.Event, core.WorkflowStep) {
	return core.Event{Type: "meme.dispatch.scheduled", Payload: map[string]any{}},
		core.WorkflowStep{Domain: core.DomainSystem, Action: "deliver-scheduled-memes"}
}

func TestSystemDomainHandlerScheduledDispatchDeliversToEveryChannel(t *testing.T) {
	fetcher := &fakeMemeFetcher{
		result: map[string]any{
			"memes": []any{
				map[string]any{"title": "First", "url": "https://example.com/1.jpg"},
				map[string]any{"title": "Second", "url": "https://example.com/2.jpg"},
			},
		},
	}
	deliverer := &fakeDiscordDeliverer{}
	handler := NewSystemDomainHandler(nil, nil, fetcher, deliverer, " chan-a , chan-b,,chan-a ")

	event, step := scheduledMemeEvent()
	result, err := handler.Handle(context.Background(), event, step)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(deliverer.deliveries) != 4 {
		t.Fatalf("expected 4 deliveries (2 memes x 2 channels), got %d", len(deliverer.deliveries))
	}
	got := []string{}
	for _, d := range deliverer.deliveries {
		got = append(got, d.channelID+":"+d.content)
	}
	want := []string{"chan-a:First", "chan-b:First", "chan-a:Second", "chan-b:Second"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected deliveries %v, want %v", got, want)
	}
	if result["delivered_count"] != 2 {
		t.Fatalf("expected delivered_count 2 memes, got %#v", result["delivered_count"])
	}
}

func TestSystemDomainHandlerScheduledDispatchOneChannelFailingDoesNotBlockOthers(t *testing.T) {
	expected := errors.New("webhook deleted")
	fetcher := &fakeMemeFetcher{
		result: map[string]any{
			"memes": []any{
				map[string]any{"title": "First"},
				map[string]any{"title": "Second"},
			},
		},
	}
	deliverer := &channelFailingDeliverer{failChannel: "chan-a", err: expected}
	handler := NewSystemDomainHandler(nil, nil, fetcher, deliverer, "chan-a,chan-b")

	event, step := scheduledMemeEvent()
	_, err := handler.Handle(context.Background(), event, step)

	if !errors.Is(err, expected) {
		t.Fatalf("expected failing channel error to surface, got %v", err)
	}
	if len(deliverer.deliveries) != 2 {
		t.Fatalf("expected healthy channel to still get 2 memes, got %d", len(deliverer.deliveries))
	}
	for _, d := range deliverer.deliveries {
		if d.channelID != "chan-b" {
			t.Fatalf("unexpected delivery to %q", d.channelID)
		}
	}
	if deliverer.attempts["chan-a"] != 1 {
		t.Fatalf("expected failing channel to be attempted once, got %d", deliverer.attempts["chan-a"])
	}
}

func TestSystemDomainHandlerScheduledDispatchValidation(t *testing.T) {
	if _, err := (*SystemDomainHandler)(nil).Handle(context.Background(), core.Event{}, core.WorkflowStep{}); err == nil {
		t.Fatal("expected nil handler error")
	}

	handler := NewSystemDomainHandler(nil, nil, nil, nil, "")
	if _, err := handler.Handle(context.Background(), core.Event{
		Type:    "meme.dispatch.scheduled",
		Payload: map[string]any{},
	}, core.WorkflowStep{
		Domain: core.DomainSystem,
		Action: "deliver-scheduled-memes",
	}); err == nil {
		t.Fatal("expected missing meme fetch client error")
	}

	handler = NewSystemDomainHandler(nil, nil, &fakeMemeFetcher{}, nil, "")
	if _, err := handler.Handle(context.Background(), core.Event{
		Type:    "meme.dispatch.scheduled",
		Payload: map[string]any{},
	}, core.WorkflowStep{
		Domain: core.DomainSystem,
		Action: "deliver-scheduled-memes",
	}); err == nil {
		t.Fatal("expected missing discord deliverer error")
	}

	handler = NewSystemDomainHandler(nil, nil, &fakeMemeFetcher{}, &fakeDiscordDeliverer{}, "")
	if _, err := handler.Handle(context.Background(), core.Event{
		Type:    "meme.dispatch.scheduled",
		Payload: map[string]any{},
	}, core.WorkflowStep{
		Domain: core.DomainSystem,
		Action: "deliver-scheduled-memes",
	}); err == nil {
		t.Fatal("expected missing delivery channel error")
	}
}

func discordSendEvent(payload map[string]any) (core.Event, core.WorkflowStep) {
	return core.Event{Type: "discord.send", Payload: payload},
		core.WorkflowStep{Domain: core.DomainSystem, Action: "send-discord-message"}
}

func TestSystemDomainHandlerDiscordSendDeliversToChosenChannelOnly(t *testing.T) {
	deliverer := &fakeDiscordDeliverer{}
	handler := NewSystemDomainHandler(nil, nil, nil, deliverer, "bot-chan,hook-chan")

	event, step := discordSendEvent(map[string]any{
		"channel_ids":    []any{"hook-chan", "hook-chan"},
		"content":        "  oi kkk  ",
		"attachment_url": "https://example.com/a.png",
	})
	result, err := handler.Handle(context.Background(), event, step)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(deliverer.deliveries) != 1 || deliverer.deliveries[0].channelID != "hook-chan" {
		t.Fatalf("expected a single delivery to hook-chan, got %#v", deliverer.deliveries)
	}
	if deliverer.deliveries[0].content != "oi kkk" || deliverer.deliveries[0].attachmentURL != "https://example.com/a.png" {
		t.Fatalf("unexpected delivery %#v", deliverer.deliveries[0])
	}
	if result["has_attachment"] != true {
		t.Fatalf("expected has_attachment true, got %#v", result["has_attachment"])
	}
}

func TestSystemDomainHandlerDiscordSendAllTargetsMemeChannels(t *testing.T) {
	deliverer := &fakeDiscordDeliverer{}
	handler := NewSystemDomainHandler(nil, nil, nil, deliverer, "bot-chan,hook-chan")

	event, step := discordSendEvent(map[string]any{"all": true, "content": "teste"})
	if _, err := handler.Handle(context.Background(), event, step); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(deliverer.deliveries) != 2 {
		t.Fatalf("expected 2 deliveries, got %d", len(deliverer.deliveries))
	}
}

func TestSystemDomainHandlerDiscordSendRejectsUnknownChannelBeforeSending(t *testing.T) {
	deliverer := &fakeDiscordDeliverer{}
	handler := NewSystemDomainHandler(nil, nil, nil, deliverer, "bot-chan")

	event, step := discordSendEvent(map[string]any{
		"channel_ids": []any{"bot-chan", "somewhere-else"},
		"content":     "oi",
	})
	if _, err := handler.Handle(context.Background(), event, step); err == nil {
		t.Fatal("expected unknown channel to be rejected")
	}
	if len(deliverer.deliveries) != 0 {
		t.Fatalf("expected nothing to be sent, got %#v", deliverer.deliveries)
	}
}

func TestSystemDomainHandlerDiscordSendValidation(t *testing.T) {
	handler := NewSystemDomainHandler(nil, nil, nil, &fakeDiscordDeliverer{}, "bot-chan")

	for name, payload := range map[string]map[string]any{
		"no content":  {"channel_ids": []any{"bot-chan"}},
		"no channels": {"content": "oi"},
	} {
		event, step := discordSendEvent(payload)
		if _, err := handler.Handle(context.Background(), event, step); err == nil {
			t.Fatalf("%s: expected validation error", name)
		}
	}
}

func TestSystemDomainHandlerDiscordSendReportsPartialFailure(t *testing.T) {
	expected := errors.New("webhook deleted")
	deliverer := &channelFailingDeliverer{failChannel: "hook-chan", err: expected}
	handler := NewSystemDomainHandler(nil, nil, nil, deliverer, "bot-chan,hook-chan")

	event, step := discordSendEvent(map[string]any{"all": true, "content": "oi"})
	_, err := handler.Handle(context.Background(), event, step)

	if !errors.Is(err, expected) {
		t.Fatalf("expected failure to surface, got %v", err)
	}
	if !strings.Contains(err.Error(), "delivered to [bot-chan]") {
		t.Fatalf("expected error to say what was delivered, got %v", err)
	}
	if len(deliverer.deliveries) != 1 || deliverer.deliveries[0].channelID != "bot-chan" {
		t.Fatalf("expected bot-chan to still be delivered, got %#v", deliverer.deliveries)
	}
}

func labelledMeme(title string, safe any, reason string) map[string]any {
	meme := map[string]any{"title": title, "url": "https://example.com/" + title + ".jpg"}
	if safe != nil {
		meme["nsfw"] = map[string]any{"safe": safe, "reason": reason}
	}
	return meme
}

func dispatchWithSafeOnly(t *testing.T, memes []any, safeOnly string) (*fakeDiscordDeliverer, map[string]any) {
	t.Helper()
	deliverer := &fakeDiscordDeliverer{}
	handler := NewSystemDomainHandler(nil, nil, &fakeMemeFetcher{result: map[string]any{"memes": memes}}, deliverer, "open-chan,hook-chan").
		WithSafeOnlyChannels(safeOnly)

	event, step := scheduledMemeEvent()
	result, err := handler.Handle(context.Background(), event, step)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return deliverer, result
}

func TestScheduledDispatchSafeOnlyChannelSkipsUnsafeMemes(t *testing.T) {
	deliverer, result := dispatchWithSafeOnly(t, []any{
		labelledMeme("clean", true, ""),
		labelledMeme("spicy", false, "BUTTOCKS_COVERED score=0.57 (strict)"),
	}, "hook-chan")

	got := []string{}
	for _, d := range deliverer.deliveries {
		got = append(got, d.channelID+":"+d.content)
	}
	want := []string{"open-chan:clean", "hook-chan:clean", "open-chan:spicy"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected deliveries %v, want %v", got, want)
	}
	if result["delivered_count"] != 2 {
		t.Fatalf("expected both memes delivered somewhere, got %#v", result["delivered_count"])
	}
	skipped, _ := result["skipped_unsafe"].(map[string]int)
	if skipped["hook-chan"] != 1 || len(skipped) != 1 {
		t.Fatalf("expected one skip for hook-chan, got %#v", result["skipped_unsafe"])
	}
}

func TestScheduledDispatchTreatsMissingLabelAsUnsafeForSafeOnlyChannels(t *testing.T) {
	deliverer, _ := dispatchWithSafeOnly(t, []any{
		labelledMeme("unlabelled", nil, ""),
		labelledMeme("malformed", "yes", ""),
	}, "hook-chan")

	for _, d := range deliverer.deliveries {
		if d.channelID == "hook-chan" {
			t.Fatalf("unlabelled meme leaked to safe-only channel: %#v", d)
		}
	}
	if len(deliverer.deliveries) != 2 {
		t.Fatalf("expected the open channel to still receive both, got %d", len(deliverer.deliveries))
	}
}

func TestScheduledDispatchWithoutSafeOnlyChannelsDeliversEverything(t *testing.T) {
	deliverer, result := dispatchWithSafeOnly(t, []any{
		labelledMeme("spicy", false, "blocked word"),
	}, "")

	if len(deliverer.deliveries) != 2 {
		t.Fatalf("expected delivery to both channels, got %d", len(deliverer.deliveries))
	}
	if _, present := result["skipped_unsafe"]; present {
		t.Fatalf("did not expect skipped_unsafe, got %#v", result["skipped_unsafe"])
	}
}

type fakeScreener struct {
	result   map[string]any
	err      error
	urls     []string
	captions []string
}

func (f *fakeScreener) Screen(_ context.Context, url, caption string) (map[string]any, error) {
	f.urls = append(f.urls, url)
	f.captions = append(f.captions, caption)
	return f.result, f.err
}

type fakeMarker struct {
	urls []string
	err  error
}

func (f *fakeMarker) MarkSent(_ context.Context, url string) error {
	f.urls = append(f.urls, url)
	return f.err
}

func sendHandler(screener MemeScreener, marker MemeMarker) (*SystemDomainHandler, *fakeDiscordDeliverer) {
	deliverer := &fakeDiscordDeliverer{}
	handler := NewSystemDomainHandler(nil, nil, nil, deliverer, "open-chan,hook-chan").
		WithSafeOnlyChannels("hook-chan").
		WithMemeTools(screener, marker)
	return handler, deliverer
}

func sendImage(handler *SystemDomainHandler, payload map[string]any) (map[string]any, error) {
	payload["attachment_url"] = "https://example.com/a.png"
	event, step := discordSendEvent(payload)
	return handler.Handle(context.Background(), event, step)
}

func deliveredChannels(d *fakeDiscordDeliverer) string {
	ids := []string{}
	for _, delivery := range d.deliveries {
		ids = append(ids, delivery.channelID)
	}
	return strings.Join(ids, ",")
}

func TestDiscordSendScreensImagesForSafeOnlyChannels(t *testing.T) {
	screener := &fakeScreener{result: map[string]any{"safe": true}}
	handler, deliverer := sendHandler(screener, nil)

	if _, err := sendImage(handler, map[string]any{"all": true, "content": "olha isso"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deliveredChannels(deliverer) != "open-chan,hook-chan" {
		t.Fatalf("expected both channels, got %q", deliveredChannels(deliverer))
	}
	if len(screener.urls) != 1 || screener.captions[0] != "olha isso" {
		t.Fatalf("expected one screen with the caption, got %v / %v", screener.urls, screener.captions)
	}
}

func TestDiscordSendSkipsSafeOnlyChannelsWhenImageIsFlagged(t *testing.T) {
	screener := &fakeScreener{result: map[string]any{"safe": false, "reason": "blocked word 'torava' in image text"}}
	handler, deliverer := sendHandler(screener, nil)

	result, err := sendImage(handler, map[string]any{"all": true, "content": "x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deliveredChannels(deliverer) != "open-chan" {
		t.Fatalf("expected only the open channel, got %q", deliveredChannels(deliverer))
	}
	skipped, _ := result["skipped_unsafe"].([]string)
	if len(skipped) != 1 || skipped[0] != "hook-chan" || !strings.Contains(result["unsafe_reason"].(string), "torava") {
		t.Fatalf("unexpected skip report %#v", result)
	}
}

func TestDiscordSendErrorsWhenEveryTargetIsRestrictedAndFlagged(t *testing.T) {
	screener := &fakeScreener{result: map[string]any{"safe": false, "reason": "BUTTOCKS_COVERED score=0.57 (strict)"}}
	marker := &fakeMarker{}
	handler, deliverer := sendHandler(screener, marker)

	_, err := sendImage(handler, map[string]any{"channel_ids": []any{"hook-chan"}, "content": "x", "mark_sent": true})
	if err == nil || !strings.Contains(err.Error(), "BUTTOCKS_COVERED") {
		t.Fatalf("expected a refusal that names the reason, got %v", err)
	}
	if len(deliverer.deliveries) != 0 || len(marker.urls) != 0 {
		t.Fatalf("nothing may be sent or marked, got %v / %v", deliverer.deliveries, marker.urls)
	}
}

func TestDiscordSendFailsClosedWhenScreenFailsOrIsMissing(t *testing.T) {
	for name, screener := range map[string]MemeScreener{
		"screen error": &fakeScreener{err: errors.New("model gone")},
		"no screener":  nil,
		"malformed":    &fakeScreener{result: map[string]any{"safe": "yes"}},
	} {
		handler, deliverer := sendHandler(screener, nil)
		result, err := sendImage(handler, map[string]any{"all": true, "content": "x"})
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", name, err)
		}
		if deliveredChannels(deliverer) != "open-chan" || result["skipped_unsafe"] == nil {
			t.Fatalf("%s: expected only the open channel, got %q", name, deliveredChannels(deliverer))
		}
	}
}

func TestDiscordSendDoesNotScreenTextOnlyOrOpenChannelSends(t *testing.T) {
	screener := &fakeScreener{result: map[string]any{"safe": false}}
	handler, deliverer := sendHandler(screener, nil)

	event, step := discordSendEvent(map[string]any{"all": true, "content": "so texto"})
	if _, err := handler.Handle(context.Background(), event, step); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := sendImage(handler, map[string]any{"channel_ids": []any{"open-chan"}, "content": "x"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(screener.urls) != 0 {
		t.Fatalf("screener should not run, got %v", screener.urls)
	}
	if deliveredChannels(deliverer) != "open-chan,hook-chan,open-chan" {
		t.Fatalf("unexpected deliveries %q", deliveredChannels(deliverer))
	}
}

func TestDiscordSendMarksTheMemeSentOnRequest(t *testing.T) {
	marker := &fakeMarker{}
	handler, _ := sendHandler(&fakeScreener{result: map[string]any{"safe": true}}, marker)

	result, err := sendImage(handler, map[string]any{"all": true, "content": "x", "mark_sent": true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["marked_sent"] != true || len(marker.urls) != 1 || marker.urls[0] != "https://example.com/a.png" {
		t.Fatalf("expected meme to be marked, got %#v / %v", result, marker.urls)
	}

	failing := &fakeMarker{err: errors.New("not in pool")}
	handler, _ = sendHandler(&fakeScreener{result: map[string]any{"safe": true}}, failing)
	result, err = sendImage(handler, map[string]any{"all": true, "content": "x", "mark_sent": true})
	if err != nil || result["marked_sent"] != false {
		t.Fatalf("a marking failure must not fail the send, got %#v / %v", result, err)
	}
}

func TestNingoStatusListsDeliveryChannels(t *testing.T) {
	handler, _ := sendHandler(nil, nil)

	result, err := handler.Handle(context.Background(), core.Event{Type: "ningo.status"},
		core.WorkflowStep{Domain: core.DomainSystem, Action: "inspect-ningo-platform"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	channels, _ := result["channels"].(map[string]any)
	if strings.Join(channels["safe_only"].([]string), ",") != "hook-chan" ||
		strings.Join(channels["meme"].([]string), ",") != "open-chan,hook-chan" {
		t.Fatalf("unexpected channel summary %#v", channels)
	}
}

type countingHealth struct{ calls int }

func (c *countingHealth) Health(context.Context) error {
	c.calls++
	return errors.New("connection refused")
}

type countingMemeStatus struct{ calls int }

func (c *countingMemeStatus) Status(context.Context) (map[string]any, error) {
	c.calls++
	return map[string]any{"status": "ok", "unsent_count": 7}, nil
}

func controlledHandler(t *testing.T) (*SystemDomainHandler, *core.Controls) {
	t.Helper()
	controls, err := core.NewControls(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatal(err)
	}
	handler := NewSystemDomainHandler(&countingHealth{}, &countingMemeStatus{}, nil, &fakeDiscordDeliverer{}, "open-chan,hook-chan").
		WithSafeOnlyChannels("hook-chan").WithControls(controls)
	return handler, controls
}

func adminEvent(eventType string, payload map[string]any) (core.Event, core.WorkflowStep) {
	return core.Event{Type: eventType, Payload: payload}, core.WorkflowStep{Domain: core.DomainSystem, Action: "admin"}
}

func TestServicesSetSwitchesAServiceAndRejectsBadInput(t *testing.T) {
	handler, controls := controlledHandler(t)

	event, step := adminEvent("services.set", map[string]any{"name": "book", "enabled": false})
	if _, err := handler.Handle(context.Background(), event, step); err != nil || controls.ServiceEnabled("book") {
		t.Fatalf("expected book to be switched off, err=%v", err)
	}

	for name, payload := range map[string]map[string]any{
		"unknown service": {"name": "root", "enabled": false},
		"missing enabled": {"name": "meme"},
		"string enabled":  {"name": "meme", "enabled": "false"},
	} {
		event, step := adminEvent("services.set", payload)
		if _, err := handler.Handle(context.Background(), event, step); err == nil {
			t.Fatalf("%s: expected an error", name)
		}
	}
	if !controls.ServiceEnabled("meme") {
		t.Fatal("rejected requests must not change anything")
	}
}

func TestJobsSetPausesResumesAndSetsIntervals(t *testing.T) {
	handler, controls := controlledHandler(t)
	set := func(payload map[string]any) (map[string]any, error) {
		event, step := adminEvent("scheduler.jobs.set", payload)
		return handler.Handle(context.Background(), event, step)
	}

	if _, err := set(map[string]any{"name": "meme.dispatch", "enabled": false}); err != nil || controls.JobEnabled("meme.dispatch") {
		t.Fatalf("job should be paused, err=%v", err)
	}
	result, err := set(map[string]any{"name": "meme.dispatch", "interval": "2h"})
	if err != nil || result["interval"] != "2h0m0s" || result["interval_override"] != true {
		t.Fatalf("unexpected interval result %#v / %v", result, err)
	}
	if _, err := set(map[string]any{"name": "meme.dispatch", "enabled": true, "interval": "default"}); err != nil {
		t.Fatal(err)
	}
	if !controls.JobEnabled("meme.dispatch") {
		t.Fatal("job should be resumed")
	}
	for _, job := range controls.Jobs() {
		if job.Name == "meme.dispatch" && job.IntervalOverride {
			t.Fatal("'default' must clear the override")
		}
	}

	for name, payload := range map[string]map[string]any{
		"nothing to change": {"name": "meme.dispatch"},
		"bad duration":      {"name": "meme.dispatch", "interval": "soon"},
		"too short":         {"name": "meme.dispatch", "interval": "5s"},
		"unknown job":       {"name": "meme.fetch", "enabled": false},
		"non-bool enabled":  {"name": "meme.dispatch", "enabled": "no"},
	} {
		if _, err := set(payload); err == nil {
			t.Fatalf("%s: expected an error", name)
		}
	}
}

func TestStatusShowsSwitchesJobsAndDoesNotProbeDisabledServices(t *testing.T) {
	handler, controls := controlledHandler(t)
	health := handler.chatAI.(*countingHealth)
	memeStatus := handler.memeStatus.(*countingMemeStatus)
	_ = controls.SetService("chat_ai", false)
	_ = controls.SetService("meme", false)
	off := false
	_ = controls.SetJob("meme.refresh", core.JobPatch{Enabled: &off})

	event, step := adminEvent("ningo.status", map[string]any{})
	result, err := handler.Handle(context.Background(), event, step)
	if err != nil {
		t.Fatal(err)
	}

	if health.calls != 0 || memeStatus.calls != 0 {
		t.Fatalf("disabled services must not be probed (chat_ai=%d meme=%d)", health.calls, memeStatus.calls)
	}
	if result["status"] != "ok" {
		t.Fatalf("a switched-off service must not make the platform degraded, got %v", result["status"])
	}
	services := result["services"].(map[string]any)
	chat := services["chat_ai"].(map[string]any)
	book := services["book"].(map[string]any)
	if chat["status"] != "disabled" || chat["enabled"] != false || book["status"] != "unchecked" || book["enabled"] != true {
		t.Fatalf("unexpected service view chat=%#v book=%#v", chat, book)
	}

	jobs := result["jobs"].([]map[string]any)
	if len(jobs) != len(core.KnownJobs) {
		t.Fatalf("expected %d jobs, got %d", len(core.KnownJobs), len(jobs))
	}
	for _, job := range jobs {
		if job["name"] == "meme.refresh" && job["enabled"] != false {
			t.Fatalf("paused job should be reported as disabled: %#v", job)
		}
	}
}

func TestStatusStillProbesEnabledServices(t *testing.T) {
	handler, _ := controlledHandler(t)
	event, step := adminEvent("ningo.status", map[string]any{})
	result, err := handler.Handle(context.Background(), event, step)
	if err != nil {
		t.Fatal(err)
	}
	services := result["services"].(map[string]any)
	if services["chat_ai"].(map[string]any)["status"] != "error" || services["meme"].(map[string]any)["status"] != "ok" {
		t.Fatalf("enabled services keep their real status: %#v", services)
	}
	if result["status"] != "degraded" {
		t.Fatalf("an enabled service that fails is degraded, got %v", result["status"])
	}
}

func TestJobAnnounceFeedsTheStatusJobs(t *testing.T) {
	handler, _ := controlledHandler(t)
	event, step := adminEvent("scheduler.jobs.announce", map[string]any{"jobs": []any{
		map[string]any{"name": "meme.dispatch", "interval": "6h0m0s", "last_fire": "2026-09-18T15:00:00Z", "next_fire": "2026-09-18T21:00:00Z"},
		map[string]any{"name": "bogus", "interval": "1h"},
		"junk",
	}})
	result, err := handler.Handle(context.Background(), event, step)
	if err != nil || result["accepted"] != 2 {
		t.Fatalf("unexpected announce result %#v / %v", result, err)
	}

	event, step = adminEvent("ningo.status", map[string]any{})
	status, _ := handler.Handle(context.Background(), event, step)
	for _, job := range status["jobs"].([]map[string]any) {
		if job["name"] == "meme.dispatch" && (job["interval"] != "6h0m0s" || job["next_at"] != "2026-09-18T21:00:00Z") {
			t.Fatalf("announce not reflected: %#v", job)
		}
	}
}

func TestDisabledMemeServiceClosesTheSafeOnlyGateAndSkipsMarking(t *testing.T) {
	controls, _ := core.NewControls("")
	screener := &fakeScreener{result: map[string]any{"safe": true}}
	marker := &fakeMarker{}
	deliverer := &fakeDiscordDeliverer{}
	handler := NewSystemDomainHandler(nil, nil, nil, deliverer, "open-chan,hook-chan").
		WithSafeOnlyChannels("hook-chan").WithMemeTools(screener, marker).WithControls(controls)
	_ = controls.SetService("meme", false)

	result, err := sendImage(handler, map[string]any{"all": true, "content": "x", "mark_sent": true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if deliveredChannels(deliverer) != "open-chan" {
		t.Fatalf("only the unrestricted channel may receive it, got %q", deliveredChannels(deliverer))
	}
	if len(screener.urls) != 0 {
		t.Fatal("a disabled meme service must not be asked to screen")
	}
	if !strings.Contains(result["unsafe_reason"].(string), "disabled") {
		t.Fatalf("the reason should say why, got %v", result["unsafe_reason"])
	}
	if len(marker.urls) != 0 || result["marked_sent"] != nil {
		t.Fatal("a disabled meme service must not be asked to mark either")
	}

	_ = controls.SetService("meme", true)
	if _, err := sendImage(handler, map[string]any{"all": true, "content": "x"}); err != nil || len(screener.urls) != 1 {
		t.Fatalf("re-enabling restores screening, err=%v urls=%v", err, screener.urls)
	}
}

func TestStatusReportsWhetherTheSchedulerIsRunning(t *testing.T) {
	handler, controls := controlledHandler(t)
	status := func() map[string]any {
		event, step := adminEvent("ningo.status", map[string]any{})
		result, err := handler.Handle(context.Background(), event, step)
		if err != nil {
			t.Fatal(err)
		}
		return result["services"].(map[string]any)["scheduler"].(map[string]any)
	}

	if entry := status(); entry["status"] != "stopped" || !strings.Contains(entry["detail"].(string), "nunca anunciou") {
		t.Fatalf("a scheduler that never announced is stopped, got %#v", entry)
	}

	controls.Announce([]core.JobAnnounce{{Name: "meme.refresh", Interval: time.Hour}}, time.Now())
	if entry := status(); entry["status"] != "ok" || entry["enabled"] != true {
		t.Fatalf("a scheduler that just announced is running, got %#v", entry)
	}

	controls.Announce([]core.JobAnnounce{{Name: "meme.refresh", Interval: time.Hour}}, time.Now().Add(-10*time.Minute))
	controls2, _ := core.NewControls("")
	controls2.Announce([]core.JobAnnounce{{Name: "meme.refresh", Interval: time.Hour}}, time.Now().Add(-10*time.Minute))
	handler.controls = controls2
	if entry := status(); entry["status"] != "stopped" || !strings.Contains(entry["detail"].(string), "sem anúncio há") {
		t.Fatalf("a stale announce means stopped, got %#v", entry)
	}

	_ = controls2.SetService("scheduler", false)
	if entry := status(); entry["status"] != "disabled" || entry["enabled"] != false {
		t.Fatalf("a switched-off scheduler reads as disabled, got %#v", entry)
	}
}

const profileSecret = "sk-or-v1-SUPERSECRETTOKEN-zzzz9999"

func profileHandler(t *testing.T) (*SystemDomainHandler, *store.DB) {
	t.Helper()
	db, err := store.Open(t.TempDir() + "/dokja.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, name := range []string{"hosted", "backup"} {
		err := db.SaveProfile(store.NewProfile{Name: name, BaseURL: "https://" + name + ".example/v1", Model: "m-" + name, APIKey: profileSecret})
		if err != nil {
			t.Fatal(err)
		}
	}
	handler := NewSystemDomainHandler(nil, nil, nil, &fakeDiscordDeliverer{}, "open-chan").WithProfiles(db)
	return handler, db
}

func TestProfilesAreListedMaskedAndSelectableThroughTheOrchestrator(t *testing.T) {
	handler, db := profileHandler(t)

	event, step := adminEvent("chat.profile.use", map[string]any{"name": "backup"})
	result, err := handler.Handle(context.Background(), event, step)
	if err != nil || result["active"] != "backup" {
		t.Fatalf("selecting an existing profile should work, got %#v / %v", result, err)
	}
	if active, _ := db.ActiveProfile(); active != "backup" {
		t.Fatalf("the selection must be stored, got %q", active)
	}

	event, step = adminEvent("chat.profiles.list", map[string]any{})
	listed, err := handler.Handle(context.Background(), event, step)
	if err != nil {
		t.Fatal(err)
	}
	profiles := listed["profiles"].([]map[string]any)
	if listed["active"] != "backup" || len(profiles) != 2 || profiles[0]["key_hint"] != "…9999" {
		t.Fatalf("unexpected listing %#v", listed)
	}
}

func TestTheTokenNeverAppearsInAnyOrchestratorResponse(t *testing.T) {
	handler, _ := profileHandler(t)
	responses := map[string]map[string]any{}
	for name, request := range map[string]struct {
		eventType string
		payload   map[string]any
	}{
		"list":   {"chat.profiles.list", map[string]any{}},
		"use":    {"chat.profile.use", map[string]any{"name": "hosted"}},
		"status": {"ningo.status", map[string]any{}},
	} {
		event, step := adminEvent(request.eventType, request.payload)
		result, err := handler.Handle(context.Background(), event, step)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		responses[name] = result
	}

	// Marshal them the way every ingress and the verbose response mode would.
	for name, result := range responses {
		encoded, err := json.Marshal(core.ProcessResult{Workflow: "admin", Result: map[string]any{"system": result}})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), "SUPERSECRET") || strings.Contains(string(encoded), profileSecret) {
			t.Fatalf("%s leaked the token: %s", name, encoded)
		}
	}
	if status := responses["status"]["chat_profiles"].(map[string]any); status["active"] != "hosted" {
		t.Fatalf("ningo.status should carry the profile summary, got %#v", status)
	}
}

func TestProfileSelectionRejectsUnknownAndMalformedNames(t *testing.T) {
	handler, db := profileHandler(t)
	for name, payload := range map[string]map[string]any{
		"unknown":  {"name": "ghost"},
		"empty":    {"name": ""},
		"missing":  {},
		"too long": {"name": strings.Repeat("a", 40)},
	} {
		event, step := adminEvent("chat.profile.use", payload)
		if _, err := handler.Handle(context.Background(), event, step); err == nil {
			t.Fatalf("%s: expected an error", name)
		}
	}
	if active, _ := db.ActiveProfile(); active != "" {
		t.Fatalf("a rejected selection must not change anything, got %q", active)
	}
}

func TestThereIsNoEventThatCreatesOrChangesAProfile(t *testing.T) {
	handler, db := profileHandler(t)
	// A caller on the request port tries every plausible way to plant a key or an endpoint.
	for _, eventType := range []string{"chat.profile.save", "chat.profile.add", "chat.profile.set", "chat.profile.update", "chat.profile.delete"} {
		event, step := adminEvent(eventType, map[string]any{
			"name": "evil", "base_url": "https://attacker.example/v1", "model": "m", "api_key": "sk-attacker-key-000011112222",
		})
		if _, err := handler.Handle(context.Background(), event, step); err != nil {
			continue // unhandled events fall through to the generic status reply, which changes nothing
		}
	}
	profiles, _ := db.ListProfiles()
	for _, profile := range profiles {
		if profile.Name == "evil" || strings.Contains(profile.BaseURL, "attacker") {
			t.Fatalf("a profile was planted through the orchestrator: %+v", profile)
		}
	}
	if len(profiles) != 2 {
		t.Fatalf("the profile set must be unchanged, got %d", len(profiles))
	}
}

func TestProfileEventsFailCleanlyWithoutADatabase(t *testing.T) {
	handler := NewSystemDomainHandler(nil, nil, nil, &fakeDiscordDeliverer{}, "open-chan")
	for _, eventType := range []string{"chat.profiles.list", "chat.profile.use"} {
		event, step := adminEvent(eventType, map[string]any{"name": "x"})
		if _, err := handler.Handle(context.Background(), event, step); err == nil || !strings.Contains(err.Error(), "DOKJA_DB_FILE") {
			t.Fatalf("%s: expected a configuration error, got %v", eventType, err)
		}
	}
}
