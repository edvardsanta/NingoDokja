package handlers

import (
	"context"
	"errors"
	"read_books/internal/core"
	"strings"
	"sync/atomic"
	"testing"
)

func interfaceProbe(level, state, detail string, err error, calls *atomic.Int32) InterfaceProbe {
	return func(context.Context) (string, string, string, error) {
		if calls != nil {
			calls.Add(1)
		}
		return level, state, detail, err
	}
}

// quietHandler has no service that fails on its own, so the overall status says only what the
// interfaces make it say. (controlledHandler's chat service is down on purpose.)
func quietHandler(t *testing.T) (*SystemDomainHandler, *core.Controls) {
	t.Helper()
	controls, err := core.NewControls(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatal(err)
	}
	return NewSystemDomainHandler(nil, nil, nil, &fakeDiscordDeliverer{}, "open-chan").WithControls(controls), controls
}

func TestStatusListsAnInterfaceWithItsStateAndKind(t *testing.T) {
	handler, _ := controlledHandler(t)
	handler.WithInterfaceProbes(map[string]InterfaceProbe{
		"discord":  interfaceProbe("ok", "connected", "", nil, nil),
		"whatsapp": interfaceProbe("error", "waiting_for_link", "waiting for the account to be linked", nil, nil),
	})

	result, services := statusServices(t, handler)

	discord := services["discord"].(map[string]any)
	if discord["status"] != "ok" || discord["state"] != "connected" || discord["kind"] != "interface" || discord["enabled"] != true {
		t.Fatalf("unexpected discord entry %#v", discord)
	}
	whatsapp := services["whatsapp"].(map[string]any)
	if whatsapp["status"] != "error" || whatsapp["state"] != "waiting_for_link" || whatsapp["kind"] != "interface" {
		t.Fatalf("unexpected whatsapp entry %#v", whatsapp)
	}
	if result["status"] != "degraded" {
		t.Fatalf("an interface that needs the operator degrades the platform, got %v", result["status"])
	}
}

func TestAnInterfaceWithoutAProbeIsNotListed(t *testing.T) {
	handler, _ := quietHandler(t)
	handler.WithInterfaceProbes(map[string]InterfaceProbe{
		"discord": interfaceProbe("ok", "connected", "", nil, nil),
	})

	_, services := statusServices(t, handler)

	if _, listed := services["whatsapp"]; listed {
		t.Fatalf("an interface without a probe must not be listed, got %#v", services["whatsapp"])
	}
}

func TestStatusStaysOkWhenEveryInterfaceIsUp(t *testing.T) {
	handler, _ := quietHandler(t)
	handler.WithInterfaceProbes(map[string]InterfaceProbe{
		"discord": interfaceProbe("ok", "connected", "", nil, nil),
	})

	result, _ := statusServices(t, handler)

	if result["status"] != "ok" {
		t.Fatalf("expected ok, got %v", result["status"])
	}
}

func TestAnInterfaceThatDoesNotAnswerIsReportedWithAFixedSentence(t *testing.T) {
	handler, _ := controlledHandler(t)
	handler.WithInterfaceProbes(map[string]InterfaceProbe{
		"whatsapp": interfaceProbe("", "", "", errors.New("call interface status: dial tcp 10.0.0.7:8094: connection refused"), nil),
	})

	_, services := statusServices(t, handler)

	entry := services["whatsapp"].(map[string]any)
	if entry["status"] != "error" || entry["state"] != "unreachable" || entry["detail"] != "no answer from the interface" {
		t.Fatalf("unexpected entry %#v", entry)
	}
	for key, value := range entry {
		if text, ok := value.(string); ok && strings.Contains(text, "10.0.0.7") {
			t.Fatalf("the error text must not reach the unauthenticated status (%s): %q", key, text)
		}
	}
}

func TestASwitchedOffInterfaceIsNotReadAndSaysOnlyThatItIsOff(t *testing.T) {
	handler, controls := quietHandler(t)
	var calls atomic.Int32
	handler.WithInterfaceProbes(map[string]InterfaceProbe{
		"whatsapp": interfaceProbe("ok", "connected", "", nil, &calls),
	})
	if err := controls.SetService("whatsapp", false); err != nil {
		t.Fatal(err)
	}

	result, services := statusServices(t, handler)

	entry := services["whatsapp"].(map[string]any)
	if entry["status"] != "disabled" || entry["enabled"] != false || entry["kind"] != "interface" || entry["detail"] != "switched off" {
		t.Fatalf("unexpected entry %#v", entry)
	}
	if _, leaked := entry["state"]; leaked {
		t.Fatalf("an interface that is off reports no state, got %#v", entry)
	}
	if calls.Load() != 0 {
		t.Fatal("a switched-off interface must not be read")
	}
	if result["status"] != "ok" {
		t.Fatalf("an interface that is off never degrades the platform, got %v", result["status"])
	}
}

func TestServicesSetSwitchesAnInterface(t *testing.T) {
	handler, controls := controlledHandler(t)

	event, step := adminEvent("services.set", map[string]any{"name": "whatsapp", "enabled": false})
	if _, err := handler.Handle(context.Background(), event, step); err != nil || controls.ServiceEnabled("whatsapp") {
		t.Fatalf("expected whatsapp to be switched off, err=%v", err)
	}
}

func interfaceDispatchHandler(t *testing.T) (*SystemDomainHandler, *core.Controls, *fakeDiscordDeliverer) {
	t.Helper()
	controls, err := core.NewControls(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatal(err)
	}
	discord := &fakeDiscordDeliverer{}
	handler := NewSystemDomainHandler(nil, nil, &fakeMemeFetcher{result: map[string]any{
		"memes": []any{labelledMeme("clean", true, "")},
	}}, discord, "open-chan").WithControls(controls)
	return handler, controls, discord
}

func TestScheduledDispatchLeavesOutDiscordWhenDiscordIsOffWithoutFailing(t *testing.T) {
	handler, controls, discord := interfaceDispatchHandler(t)
	if err := controls.SetService("discord", false); err != nil {
		t.Fatal(err)
	}

	event, step := scheduledMemeEvent()
	result, err := handler.Handle(context.Background(), event, step)
	if err != nil {
		t.Fatalf("a channel of an interface that is off must not fail the run: %v", err)
	}

	if len(discord.deliveries) != 0 {
		t.Fatalf("nothing may be delivered while discord is off, got %d", len(discord.deliveries))
	}
	skipped, _ := result["skipped_interfaces"].([]string)
	if len(skipped) != 1 || skipped[0] != "open-chan" {
		t.Fatalf("expected the channel to be reported, got %#v", result["skipped_interfaces"])
	}
}

func TestScheduledDispatchDeliversWhenDiscordIsOn(t *testing.T) {
	handler, _, discord := interfaceDispatchHandler(t)

	event, step := scheduledMemeEvent()
	result, err := handler.Handle(context.Background(), event, step)
	if err != nil {
		t.Fatal(err)
	}

	if len(discord.deliveries) != 1 {
		t.Fatalf("expected one delivery, got %d", len(discord.deliveries))
	}
	if _, reported := result["skipped_interfaces"]; reported {
		t.Fatalf("nothing was left out, got %#v", result["skipped_interfaces"])
	}
}

func TestAdminSendRefusesWhenTheInterfaceIsOff(t *testing.T) {
	handler, controls, discord := interfaceDispatchHandler(t)
	if err := controls.SetService("discord", false); err != nil {
		t.Fatal(err)
	}
	event := core.Event{Type: "discord.send", Payload: map[string]any{
		"content": "hello", "channel_ids": []any{"open-chan"},
	}}
	step := core.WorkflowStep{Domain: core.DomainSystem, Action: "admin"}

	_, err := handler.Handle(context.Background(), event, step)

	if err == nil || !strings.Contains(err.Error(), "switched off") {
		t.Fatalf("nothing left to send to must be refused, got %v", err)
	}
	if len(discord.deliveries) != 0 {
		t.Fatal("nothing may be delivered while the interface is off")
	}
}
