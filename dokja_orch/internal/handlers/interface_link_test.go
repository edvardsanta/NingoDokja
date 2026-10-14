package handlers

import (
	"context"
	"errors"
	"read_books/internal/core"
	"strings"
	"testing"
)

type fakeLinker struct {
	state  string
	matrix []string
	err    error
	calls  []string
}

func (f *fakeLinker) Status(context.Context) (string, []string, error) {
	f.calls = append(f.calls, "status")
	return f.state, f.matrix, f.err
}

func (f *fakeLinker) Start(context.Context) (string, error) {
	f.calls = append(f.calls, "start")
	return f.state, f.err
}

func (f *fakeLinker) Reset(context.Context) (string, error) {
	f.calls = append(f.calls, "reset")
	return f.state, f.err
}

func linkEvent(eventType string, payload map[string]any) (core.Event, core.WorkflowStep) {
	return core.Event{Type: eventType, Payload: payload}, core.WorkflowStep{Domain: core.DomainSystem, Action: "link"}
}

func TestLinkStatusGivesTheStateAndTheRowsOfTheCode(t *testing.T) {
	linker := &fakeLinker{state: "waiting_for_link", matrix: []string{"0101", "1010"}}
	handler, _ := quietHandler(t)
	handler.WithInterfaceLinks(map[string]InterfaceLinker{"whatsapp": linker})

	event, step := linkEvent("communications.link.status", map[string]any{"interface": "whatsapp"})
	result, err := handler.Handle(context.Background(), event, step)
	if err != nil {
		t.Fatal(err)
	}

	if result["state"] != "waiting_for_link" || result["interface"] != "whatsapp" {
		t.Fatalf("unexpected reply %#v", result)
	}
	if rows, _ := result["matrix"].([]string); len(rows) != 2 {
		t.Fatalf("expected the rows of the code, got %#v", result["matrix"])
	}
}

func TestLinkStartAndResetAnswerTheStateAndNeverACode(t *testing.T) {
	linker := &fakeLinker{state: "connecting", matrix: []string{"0101"}}
	handler, _ := quietHandler(t)
	handler.WithInterfaceLinks(map[string]InterfaceLinker{"whatsapp": linker})

	for _, eventType := range []string{"communications.link.start", "communications.link.reset"} {
		event, step := linkEvent(eventType, map[string]any{"interface": "whatsapp"})
		result, err := handler.Handle(context.Background(), event, step)
		if err != nil {
			t.Fatal(err)
		}
		if result["state"] != "connecting" {
			t.Fatalf("%s: unexpected reply %#v", eventType, result)
		}
		if _, leaked := result["matrix"]; leaked {
			t.Fatalf("%s must not carry a code, got %#v", eventType, result)
		}
	}
	if strings.Join(linker.calls, ",") != "start,reset" {
		t.Fatalf("unexpected calls %v", linker.calls)
	}
}

func TestLinkRefusesAnInterfaceThatCannotBeLinked(t *testing.T) {
	handler, _ := quietHandler(t)
	handler.WithInterfaceLinks(map[string]InterfaceLinker{"whatsapp": &fakeLinker{state: "connected"}})

	for name, payload := range map[string]map[string]any{
		"other interface": {"interface": "discord"},
		"no interface":    {},
		"not a name":      {"interface": "../whatsapp"},
	} {
		t.Run(name, func(t *testing.T) {
			event, step := linkEvent("communications.link.status", payload)
			if _, err := handler.Handle(context.Background(), event, step); err == nil {
				t.Fatal("an interface without a linker must be refused")
			}
		})
	}
}

func TestLinkErrorsDoNotEchoWhatTheInterfaceSaid(t *testing.T) {
	linker := &fakeLinker{err: errors.New("call interface link: dial tcp 10.0.0.9:8094: connection refused")}
	handler, _ := quietHandler(t)
	handler.WithInterfaceLinks(map[string]InterfaceLinker{"whatsapp": linker})

	event, step := linkEvent("communications.link.reset", map[string]any{"interface": "whatsapp"})
	_, err := handler.Handle(context.Background(), event, step)

	if err == nil || strings.Contains(err.Error(), "10.0.0.9") {
		t.Fatalf("expected an error that hides the address, got %v", err)
	}
}
