package cli

import (
	"reflect"
	"testing"
)

func TestBuildDispatchPayloadAlwaysCarriesAnIntegerLimit(t *testing.T) {
	payload, err := buildDispatchPayload(3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if limit, ok := payload["limit"].(int); !ok || limit != 3 {
		t.Fatalf("expected integer limit 3, got %#v", payload["limit"])
	}
}

func TestBuildDispatchPayloadRejectsLimitsThatMeanEverything(t *testing.T) {
	for _, limit := range []int{0, -1} {
		if _, err := buildDispatchPayload(limit); err == nil {
			t.Fatalf("expected limit %d to be rejected", limit)
		}
	}
}

func TestBuildDiscordSendPayload(t *testing.T) {
	payload, err := buildDiscordSendPayload([]string{" 111 ", "", "222"}, false, "  oi kkk ", "https://example.com/a.png")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]any{
		"content":        "oi kkk",
		"attachment_url": "https://example.com/a.png",
		"channel_ids":    []string{"111", "222"},
	}
	if !reflect.DeepEqual(payload, want) {
		t.Fatalf("unexpected payload %#v", payload)
	}

	payload, err = buildDiscordSendPayload(nil, true, "teste", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if payload["all"] != true || payload["channel_ids"] != nil || payload["attachment_url"] != nil {
		t.Fatalf("unexpected payload %#v", payload)
	}
}

func TestBuildDiscordSendPayloadValidation(t *testing.T) {
	cases := map[string]func() error{
		"no content":      func() error { _, err := buildDiscordSendPayload([]string{"1"}, false, " ", ""); return err },
		"no destination":  func() error { _, err := buildDiscordSendPayload(nil, false, "oi", ""); return err },
		"all and channel": func() error { _, err := buildDiscordSendPayload([]string{"1"}, true, "oi", ""); return err },
	}
	for name, run := range cases {
		if err := run(); err == nil {
			t.Fatalf("%s: expected validation error", name)
		}
	}
}

func TestBuildListPayload(t *testing.T) {
	payload, err := buildListPayload(" Sent ", 10, 20)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]any{"scope": "sent", "limit": 10, "offset": 20}
	if !reflect.DeepEqual(payload, want) {
		t.Fatalf("unexpected payload %#v", payload)
	}

	for name, run := range map[string]func() error{
		"bad scope":      func() error { _, err := buildListPayload("all", 10, 0); return err },
		"limit too low":  func() error { _, err := buildListPayload("unsent", 0, 0); return err },
		"limit too high": func() error { _, err := buildListPayload("unsent", 101, 0); return err },
		"bad offset":     func() error { _, err := buildListPayload("unsent", 10, -1); return err },
	} {
		if err := run(); err == nil {
			t.Fatalf("%s: expected validation error", name)
		}
	}
}

func TestJobEventMapsEveryJobToItsEvent(t *testing.T) {
	want := map[string]string{
		"meme.refresh":  "meme.pool.refresh",
		"meme.dispatch": "meme.dispatch.scheduled",
	}
	for _, name := range JobNames {
		eventType, payload, err := JobEvent(name, 2)
		if err != nil || eventType != want[name] || payload == nil {
			t.Fatalf("%s: got %q %v %v", name, eventType, payload, err)
		}
	}
	_, payload, _ := JobEvent("meme.dispatch", 3)
	if payload["limit"] != 3 {
		t.Fatalf("meme.dispatch must carry an integer limit, got %#v", payload)
	}
	if _, _, err := JobEvent("meme.dispatch", 0); err == nil {
		t.Fatal("a zero limit means the whole pool and must be refused")
	}
	if _, _, err := JobEvent("rm -rf", 1); err == nil {
		t.Fatal("unknown jobs must be refused")
	}
}

func TestBuildJobIntervalPayload(t *testing.T) {
	payload, err := BuildJobIntervalPayload("meme.dispatch", "90m")
	if err != nil || payload["interval"] != "1h30m0s" {
		t.Fatalf("unexpected payload %#v / %v", payload, err)
	}
	if payload, err := BuildJobIntervalPayload("meme.dispatch", " default "); err != nil || payload["interval"] != "default" {
		t.Fatalf("default should clear the override, got %#v / %v", payload, err)
	}
	for name, run := range map[string]func() error{
		"too short":      func() error { _, err := BuildJobIntervalPayload("meme.dispatch", "10s"); return err },
		"too long":       func() error { _, err := BuildJobIntervalPayload("meme.dispatch", "9999h"); return err },
		"not a duration": func() error { _, err := BuildJobIntervalPayload("meme.dispatch", "soon"); return err },
		"unknown job":    func() error { _, err := BuildJobIntervalPayload("nope", "1h"); return err },
	} {
		if err := run(); err == nil {
			t.Fatalf("%s: expected an error", name)
		}
	}
}

func TestSwitchPayloadsRejectUnknownNames(t *testing.T) {
	if payload, err := buildServicePayload("meme", false); err != nil || payload["enabled"] != false {
		t.Fatalf("unexpected %#v / %v", payload, err)
	}
	if payload, err := buildJobTogglePayload("meme.dispatch", true); err != nil || payload["enabled"] != true {
		t.Fatalf("unexpected %#v / %v", payload, err)
	}
	if _, err := buildServicePayload("orchestrator", false); err == nil {
		t.Fatal("only the switchable services are accepted")
	}
	if _, err := buildJobTogglePayload("meme.fetch", false); err == nil {
		t.Fatal("only scheduler jobs are accepted")
	}
}

func TestDomainFieldFindsThePartOfTheStatusResponse(t *testing.T) {
	response := map[string]any{
		"event_id": "e",
		"workflow": "ningo",
		"result":   map[string]any{"system": map[string]any{"services": map[string]any{"meme": "ok"}, "jobs": []any{"x"}}},
	}
	services, err := domainField(response, "services")
	if err != nil || services.(map[string]any)["meme"] != "ok" {
		t.Fatalf("unexpected %#v / %v", services, err)
	}
	flat := map[string]any{"result": map[string]any{"jobs": []any{"y"}}}
	if jobs, err := domainField(flat, "jobs"); err != nil || jobs.([]any)[0] != "y" {
		t.Fatalf("unexpected %#v / %v", jobs, err)
	}
	if _, err := domainField(response, "missing"); err == nil {
		t.Fatal("a missing field must be an error")
	}
	if _, err := domainField("nope", "x"); err == nil {
		t.Fatal("a malformed response must be an error")
	}
}
