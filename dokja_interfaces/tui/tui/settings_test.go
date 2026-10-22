package tui

import (
	"strings"
	"testing"

	"dokja_interfaces/cli/cli"
)

func settingsModel(t *testing.T) (*Model, *fakeRuntime) {
	t.Helper()
	runtime := &fakeRuntime{
		snapshot: cli.RuntimeSnapshot{Modules: []cli.RuntimeModule{{Name: "discord", State: "ready"}}},
		settings: cli.SettingsView{WebhookChannels: []string{"102"}, Settings: []cli.SettingRow{
			{ID: "discord.meme_channels", Group: "discord", Kind: "list", Value: "101,102", Affects: []string{"orchestrator", "communications"}, Note: "sessions"},
			{ID: "discord.direct_messages", Group: "discord", Kind: "enum", Options: []string{"open", "closed"}, Value: "open", Affects: []string{"discord"}},
			{ID: "whatsapp.groups", Group: "whatsapp", Kind: "list", Affects: []string{"whatsapp"}},
		}},
	}
	return withModules(t, newFake(), runtime), runtime
}

func setCalls(runtime *fakeRuntime) []call {
	var out []call
	for _, c := range runtime.calls {
		if c.eventType == "runtime.settings.set" {
			out = append(out, c)
		}
	}
	return out
}

func TestSettingsShowGroupsAndWhetherTheBotOrAWebhookDelivers(t *testing.T) {
	m, _ := settingsModel(t)
	press(m, "c")
	view := m.View()
	for _, want := range []string{"Discord", "WhatsApp", "101 (bot), 102 (webhook)", "DOKJA_DISCORD_WEBHOOKS"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q:\n%s", want, view)
		}
	}
}

func TestEditingASettingSaysWhatIsRecreatedAndSavesOnlyOnAYes(t *testing.T) {
	m, runtime := settingsModel(t)
	press(m, "c", "down", "enter")
	m.settingInput.SetValue("closed")
	press(m, "enter")
	if m.overlay != overlayConfirm || len(setCalls(runtime)) != 0 {
		t.Fatal("saving must be confirmed first")
	}
	if view := m.View(); !strings.Contains(view, "discord") {
		t.Fatalf("the confirmation names the recreated module:\n%s", view)
	}
	press(m, "y")
	calls := setCalls(runtime)
	if len(calls) != 1 || calls[0].payload["discord.direct_messages"] != "closed" || m.overlay != overlaySettings {
		t.Fatalf("%+v overlay %v", calls, m.overlay)
	}
}

func TestCancellingAnEditSavesNothing(t *testing.T) {
	m, runtime := settingsModel(t)
	press(m, "c", "enter", "esc", "esc")
	if len(setCalls(runtime)) != 0 || m.overlay != overlayNone {
		t.Fatal("nothing is saved when the edit is left")
	}
}

func TestSettingsShowEffectiveDefaults(t *testing.T) {
	m, _ := settingsModel(t)
	row := cli.SettingRow{ID: "discord.direct_messages", Default: "open"}
	if got := m.settingValue(row); !strings.Contains(got, "open") || strings.Contains(got, "none") {
		t.Fatal(got)
	}
}
