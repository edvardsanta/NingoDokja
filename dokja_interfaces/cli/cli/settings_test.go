package cli

import (
	"bytes"
	"os"
	"path/filepath"
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

func TestEmptySettingsDisplayTheirEffectiveDefaults(t *testing.T) {
	for _, row := range []SettingRow{{ID: "discord.direct_messages", Default: "open"}, {ID: "whatsapp.own_chat", Default: "true"}, {ID: "models.embedding", Kind: "text", Default: "bge-m3"}} {
		if got := SettingValueText(SettingsView{}, row); got != row.Default+" (default)" {
			t.Fatal(got)
		}
	}
}

func TestCLIListsFixedProfileAndRejectsItsEditBeforeSending(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "docker-compose.lite.yml"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	base, err := os.MkdirTemp("", "dokja-settings-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(base) })
	t.Setenv("XDG_STATE_HOME", base)
	markers := containerMarkers
	containerMarkers = nil
	t.Cleanup(func() { containerMarkers = markers })
	var writes int
	client := fakeDaemon(t, root, func(event map[string]any) map[string]any {
		if event["type"] == "runtime.settings.set" {
			writes++
		}
		return map[string]any{"version": 1, "project_id": RuntimeProjectID(root), "status": "ok", "result": map[string]any{"domain": "runtime", "result": map[string]any{"busy": false, "settings": []any{map[string]any{"id": "models.embedding", "group": "models", "default": "bge-m3", "value": "", "read_only": true}}}}}
	})
	state, err := RuntimeStateDir(root, os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(client.StateDir, "control.sock"), filepath.Join(state, "control.sock")); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	view, err := client.Settings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	printSettings(&out, view)
	if !strings.Contains(out.String(), "bge-m3 (default) (fixed)") {
		t.Fatal(out.String())
	}
	err = NewApp().Run(t.Context(), []string{"dokja-cli", "settings", "set", "models.embedding=example/model", "--yes", "--runtime-root", root})
	if err == nil || !strings.Contains(err.Error(), "fixed for this version") || writes != 0 {
		t.Fatal(err, writes)
	}
}
