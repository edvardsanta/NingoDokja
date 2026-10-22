package cli

import (
	"strings"
	"testing"
)

func settingsFixture() SettingsView {
	return SettingsView{WebhookChannels: []string{"102"}, Settings: []SettingRow{
		{ID: "discord.meme_channels", Group: "discord", Value: "101,102", Affects: []string{"orchestrator", "communications"}, Note: "sessions"},
		{ID: "discord.answer_channels", Group: "discord", Value: "", Affects: []string{"discord"}},
		{ID: "models.embedding", Group: "models", Value: "", Affects: []string{"embedding_init", "memory", "knowledge"}, Note: "reindex"},
	}}
}

func TestDeliveryChannelsSayWhetherTheBotOrAWebhookDelivers(t *testing.T) {
	view := settingsFixture()
	row, _ := view.Lookup("discord.meme_channels")
	if got := SettingValueText(view, row); got != "101 (bot), 102 (webhook)" {
		t.Fatal(got)
	}
	row, _ = view.Lookup("discord.answer_channels")
	if got := SettingValueText(view, row); got != "(every channel)" {
		t.Fatal(got)
	}
}

func TestTheNoticeNamesWhatIsRecreatedNowAndLater(t *testing.T) {
	lines := strings.Join(SettingsChangeNotice(settingsFixture(), []string{"discord.meme_channels"}, map[string]bool{"orchestrator": true}), "\n")
	for _, want := range []string{"Recreated now: orchestrator.", "next start: communications.", "chat sessions"} {
		if !strings.Contains(lines, want) {
			t.Fatalf("missing %q in\n%s", want, lines)
		}
	}
	if lines := strings.Join(SettingsChangeNotice(settingsFixture(), []string{"models.embedding"}, nil), "\n"); !strings.Contains(lines, "embed what they store again") {
		t.Fatal(lines)
	}
}

func TestSetRefusesArgumentsThatAreNotAssignments(t *testing.T) {
	if err := NewApp().Run(t.Context(), []string{"dokja-cli", "settings", "set", "discord.direct_messages"}); err == nil || !strings.Contains(err.Error(), "<id>=<value>") {
		t.Fatal(err)
	}
}
