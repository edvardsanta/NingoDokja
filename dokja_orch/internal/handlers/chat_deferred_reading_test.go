package handlers

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeProactiveDeliverer struct {
	mu        sync.Mutex
	delivered []proactiveDelivery
}

type proactiveDelivery struct {
	channelID string
	content   string
	replyTo   string
}

func (f *fakeProactiveDeliverer) DeliverReply(_ context.Context, channelID, content, replyToMessageID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.delivered = append(f.delivered, proactiveDelivery{channelID: channelID, content: content, replyTo: replyToMessageID})
	return nil
}

func (f *fakeProactiveDeliverer) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.delivered)
}

func (f *fakeProactiveDeliverer) last() proactiveDelivery {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.delivered[len(f.delivered)-1]
}

// waitForDelivery polls until the background settlement lands or the budget runs out.
func waitForDelivery(t *testing.T, deliverer *fakeProactiveDeliverer, want int) []proactiveDelivery {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		deliverer.mu.Lock()
		if len(deliverer.delivered) >= want {
			out := append([]proactiveDelivery(nil), deliverer.delivered...)
			deliverer.mu.Unlock()
			return out
		}
		deliverer.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no proactive delivery arrived (want %d)", want)
	return nil
}

func TestDeferredReadingPromisesAndSettlesWithTheFreshEvidence(t *testing.T) {
	service := &webReadingService{fail: true}
	generator := &fakeChatGenerator{result: map[string]any{"reply": "model reply"}}
	deliverer := &fakeProactiveDeliverer{}
	h := NewChatDomainHandler(generator).WithWebReading(service, nil).WithProactiveDeliverer(deliverer)
	event := chatEvent("https://example.com/slow-story")
	event.Context = map[string]any{"message_id": "original-1"}
	result, err := h.Handle(context.Background(), event, chatStep)
	if err != nil {
		t.Fatal(err)
	}
	reply := result["reply"].(string)
	if !strings.Contains(reply, "Não consegui ler 1") || !strings.Contains(reply, "vou tentar de novo") {
		t.Fatalf("turn did not report the failure and promise the follow-up: %s", reply)
	}
	service.fail = false
	service.injection = "settled"
	delivered := waitForDelivery(t, deliverer, 1)
	if delivered[0].channelID != "c1" || delivered[0].replyTo != "original-1" {
		t.Fatalf("follow-up lost its correlation: %#v", delivered[0])
	}
	if delivered[0].content != "model reply" {
		t.Fatalf("follow-up did not deliver the generated reply: %s", delivered[0].content)
	}
	evidence := false
	for _, m := range generator.messages {
		if m["role"] == "user" && strings.Contains(m["content"], "UNTRUSTED_WEB_EVIDENCE") && strings.Contains(m["content"], "settled") {
			evidence = true
		}
	}
	if !evidence {
		t.Fatal("follow-up generation did not receive the fresh evidence")
	}
	h.sessions.mu.Lock()
	state := h.sessions.items[chatSessionBaseKey(event)]
	h.sessions.mu.Unlock()
	found := false
	for _, m := range state.messages {
		if m.Role == "assistant" && m.Content == delivered[0].content {
			found = true
		}
	}
	if !found {
		t.Fatal("settlement was not recorded in the session transcript")
	}
}

func TestDeferredReadingSettlesWithAFixedNoticeWhenTheRetryFails(t *testing.T) {
	service := &webReadingService{fail: true}
	generator := &fakeChatGenerator{result: map[string]any{"reply": "model reply"}}
	deliverer := &fakeProactiveDeliverer{}
	h := NewChatDomainHandler(generator).WithWebReading(service, nil).WithProactiveDeliverer(deliverer)
	event := chatEvent("https://example.com/unavailable")
	if _, err := h.Handle(context.Background(), event, chatStep); err != nil {
		t.Fatal(err)
	}
	delivered := waitForDelivery(t, deliverer, 1)
	if !strings.Contains(delivered[0].content, "Tentei de novo") {
		t.Fatalf("settlement notice missing: %s", delivered[0].content)
	}
}

func TestDeferredReadingIsNotOfferedWithoutADeliverer(t *testing.T) {
	service := &webReadingService{fail: true}
	generator := &fakeChatGenerator{result: map[string]any{"reply": "model reply"}}
	h := NewChatDomainHandler(generator).WithWebReading(service, nil)
	result, err := h.Handle(context.Background(), chatEvent("https://example.com/unavailable"), chatStep)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result["reply"].(string), "vou tentar de novo") {
		t.Fatal("promise made with no way to deliver")
	}
	if len(h.deferred.entries) != 0 {
		t.Fatal("registry grew without a deliverer")
	}
}

func TestDeferredReadingRefusesPermanentFailures(t *testing.T) {
	service := &webReadingService{fail: true, failureReason: "web_pdf_unreadable: private"}
	generator := &fakeChatGenerator{result: map[string]any{"reply": "model reply"}}
	deliverer := &fakeProactiveDeliverer{}
	h := NewChatDomainHandler(generator).WithWebReading(service, nil).WithProactiveDeliverer(deliverer)
	result, err := h.Handle(context.Background(), chatEvent("https://example.com/protected.pdf"), chatStep)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result["reply"].(string), "vou tentar de novo") {
		t.Fatal("promise made for a permanent failure")
	}
	if len(h.deferred.entries) != 0 || deliverer.count() != 0 {
		t.Fatal("permanent failure was deferred")
	}
}

func TestDeferredReadingRegistryHoldsOnePromisePerChannel(t *testing.T) {
	registry := newDeferredReadingRegistry()
	entry := deferredReading{channelID: "chan", url: "https://example.com/one"}
	if !registry.add(entry) {
		t.Fatal("first promise refused")
	}
	if registry.add(deferredReading{channelID: "chan", url: "https://example.com/two"}) {
		t.Fatal("a second promise for the same channel was admitted")
	}
	if registry.add(deferredReading{channelID: "chan", url: "https://example.com/one"}) {
		t.Fatal("a duplicate promise for the same URL was admitted")
	}
	if !registry.add(deferredReading{channelID: "other", url: "https://example.com/two"}) {
		t.Fatal("another channel's promise was refused")
	}
	claimed, ok := registry.claim("chan", "https://example.com/one")
	if !ok || claimed.channelID != "chan" {
		t.Fatalf("claim did not return the pending entry: %#v", claimed)
	}
	if _, ok := registry.claim("chan", "https://example.com/one"); ok {
		t.Fatal("claimed twice")
	}
	registry.add(deferredReading{channelID: "chan", url: "https://example.com/late"})
	registry.now = func() time.Time { return time.Now().Add(deferredReadingTTL + time.Minute) }
	if _, ok := registry.claim("chan", "https://example.com/late"); ok {
		t.Fatal("an expired promise settled")
	}
}
