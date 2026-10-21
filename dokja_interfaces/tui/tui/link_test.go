package tui

import (
	"strings"
	"testing"

	"dokja_interfaces/cli/cli"
)

func linkCode() []any {
	rows := make([]any, 21)
	for y := range rows {
		rows[y] = strings.Repeat("10", 10) + "1"
	}
	return rows
}

func linkModel(t *testing.T) (*Model, *fakeClient) {
	t.Helper()
	fake := newFake()
	fake.responses["communications.link.status"] = map[string]any{"interface": "whatsapp", "state": "waiting_for_link", "matrix": linkCode()}
	fake.responses["communications.link.start"] = map[string]any{"interface": "whatsapp", "state": "waiting_for_link"}
	fake.responses["communications.link.reset"] = map[string]any{"interface": "whatsapp", "state": "logged_out"}
	runtime := &fakeRuntime{snapshot: cli.RuntimeSnapshot{Modules: []cli.RuntimeModule{{Name: "whatsapp", State: "ready"}}}}
	return withModules(t, fake, runtime), fake
}

func TestTheLinkWindowDrawsTheCodeForTheWhatsAppRow(t *testing.T) {
	m, fake := linkModel(t)
	press(m, "l")
	if m.overlay != overlayLink || fake.last("communications.link.status").payload["interface"] != "whatsapp" {
		t.Fatalf("overlay %v, calls %+v", m.overlay, fake.calls)
	}
	if view := m.View(); !strings.Contains(view, "▀") && !strings.Contains(view, "▄") {
		t.Fatalf("the code must be drawn:\n%s", view)
	}
}

func TestANewCodeAndAnUnlinkAreAskedFromTheWindow(t *testing.T) {
	m, fake := linkModel(t)
	press(m, "l", "n")
	if fake.count("communications.link.start") != 1 {
		t.Fatal("n asks for a new code")
	}
	press(m, "u")
	if fake.count("communications.link.reset") != 0 || m.overlay != overlayConfirm {
		t.Fatal("unlinking erases the session and must be confirmed first")
	}
	press(m, "y")
	if fake.count("communications.link.reset") != 1 || m.overlay != overlayLink {
		t.Fatalf("a yes unlinks and returns to the window, overlay %v", m.overlay)
	}
}

func TestOtherRowsDoNotOpenTheLinkWindow(t *testing.T) {
	m, fake := linkModel(t)
	press(m, "down", "l")
	if m.overlay == overlayLink || fake.count("communications.link.status") != 0 {
		t.Fatal("only the whatsapp row links")
	}
}

func TestClosingTheWindowStopsItsRefresh(t *testing.T) {
	m, fake := linkModel(t)
	press(m, "l", "esc")
	before := fake.count("communications.link.status")
	_, cmd := m.Update(linkTickMsg{})
	pump(m, cmd)
	if fake.count("communications.link.status") != before || cmd != nil {
		t.Fatal("a closed window must not read again")
	}
}
