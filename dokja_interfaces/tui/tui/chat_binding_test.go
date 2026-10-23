package tui

import (
	"errors"
	"strings"
	"testing"

	"dokja_interfaces/cli/cli"
)

func chatWriteCalls(r *fakeRuntime) []call {
	var result []call
	for _, c := range r.calls {
		if c.eventType == "runtime.chat.configure" {
			result = append(result, c)
		}
	}
	return result
}
func TestChatBindingRequiresConfirmationAndDoesNotInfer(t *testing.T) {
	m, r := editableProviders(t)
	press(m, "c")
	if m.overlay != overlayChatBinding || !strings.Contains(m.View(), "fixture/old") {
		t.Fatal(m.View())
	}
	press(m, "enter")
	if m.overlay != overlayConfirm || len(chatWriteCalls(r)) != 0 {
		t.Fatal("saved before confirmation")
	}
	press(m, "y")
	writes := chatWriteCalls(r)
	if len(writes) != 1 || writes[0].payload["configuration_revision"] != uint64(2) || r.chat.Model != "fixture/old" || m.overlay != overlayChatBinding {
		t.Fatal(writes, m.overlay)
	}
	for _, c := range r.calls {
		if c.eventType != "runtime.modules" && c.eventType != "runtime.settings" && c.eventType != "runtime.providers" && c.eventType != "runtime.chat" && c.eventType != "runtime.chat.configure" {
			t.Fatal("unexpected effect", c)
		}
	}
}
func TestChatBindingCancellationAndConflictKeepOriginalRevision(t *testing.T) {
	m, r := editableProviders(t)
	press(m, "c", "enter", "n")
	if len(chatWriteCalls(r)) != 0 {
		t.Fatal("cancel saved")
	}
	// A late inventory update must not change the choices or the read revision.
	r.providers.Providers[0].DeclaredModels = []string{"fixture/new"}
	m.Update(providersMsg{view: r.providers})
	r.chat.Revision++
	press(m, "enter", "y")
	writes := chatWriteCalls(r)
	if len(writes) != 1 || writes[0].payload["model"] != "fixture/old" || writes[0].payload["configuration_revision"] != uint64(2) || !m.noticeErr {
		t.Fatal(writes, m.notice)
	}
	if m.chatBinding.Revision != 2 {
		t.Fatal("rebased stale draft")
	}
}
func TestChatBindingOnlyOffersEnabledDeclarations(t *testing.T) {
	m, r := editableProviders(t)
	r.providers.Providers = []cli.ProviderRow{
		{ID: "codex", Enabled: false, DeclaredModels: []string{"disabled"}},
		{ID: "ollama", Enabled: true, Models: []string{"observed-only"}},
	}
	m.providers = &r.providers
	press(m, "c", "enter")
	if len(m.chatChoices) != 0 || len(chatWriteCalls(r)) != 0 || !strings.Contains(m.View(), tr("chat_binding_no_choices")) {
		t.Fatal(m.View(), r.calls)
	}
}
func TestChatBindingReadFailureDoesNotPermitSaveOrRetry(t *testing.T) {
	m, r := editableProviders(t)
	r.chatErr = errors.New("unsupported local runtime event")
	press(m, "c", "enter")
	if len(chatWriteCalls(r)) != 0 || !strings.Contains(m.View(), "unsupported local runtime event") {
		t.Fatal(m.View(), r.calls)
	}
	count := 0
	for _, c := range r.calls {
		if c.eventType == "runtime.chat" {
			count++
		}
	}
	if count != 1 {
		t.Fatal("automatic read retry", r.calls)
	}
}
