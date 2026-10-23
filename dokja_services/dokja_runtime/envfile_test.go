package runtime

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// A fixture only: short placeholder ids and an example host, never real ones.
const fixtureEnv = `# local stack
DISCORD_BOT_TOKEN=fixture-token-value
DISCORD_SCHEDULED_MEME_CHANNEL_ID=101,102
export DISCORD_DM_POLICY="open"
DOKJA_DISCORD_WEBHOOKS=102=https://hooks.example/one,bad=https://hooks.example/two
UNMANAGED_API_KEY=fixture-key
`

func fixture(t *testing.T) EnvFile {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte(fixtureEnv), 0o640); err != nil {
		t.Fatal(err)
	}
	return EnvFile{Path: path}
}

func TestReadKeepsOnlySettingsAndWebhookChannelIds(t *testing.T) {
	values, webhooks, err := fixture(t).Read()
	if err != nil {
		t.Fatal(err)
	}
	if values["DISCORD_SCHEDULED_MEME_CHANNEL_ID"] != "101,102" || values["DISCORD_DM_POLICY"] != "open" {
		t.Fatal(values)
	}
	if _, ok := values["DISCORD_BOT_TOKEN"]; ok {
		t.Fatal("a credential must never be read")
	}
	if !reflect.DeepEqual(webhooks, []string{"102"}) {
		t.Fatal(webhooks)
	}
	for _, value := range values {
		if strings.Contains(value, "hooks.example") {
			t.Fatal("a webhook address must never be kept")
		}
	}
}

func TestWriteChangesOnlyTheGivenLinesAndKeepsTheMode(t *testing.T) {
	env := fixture(t)
	if err := env.Write(map[string]string{"DISCORD_DM_POLICY": "closed", "DOKJA_WHATSAPP_ALLOWED_GROUPS": "1-2@g.us"}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(env.Path)
	want := strings.Replace(fixtureEnv, `export DISCORD_DM_POLICY="open"`, `export DISCORD_DM_POLICY=closed`, 1) + "DOKJA_WHATSAPP_ALLOWED_GROUPS=1-2@g.us\n"
	if string(raw) != want {
		t.Fatalf("got\n%s\nwant\n%s", raw, want)
	}
	if info, _ := os.Stat(env.Path); info.Mode().Perm() != 0o640 {
		t.Fatal(info.Mode())
	}
}

func TestUnsafeValuesAndCredentialsAreNeverWritten(t *testing.T) {
	env := fixture(t)
	for _, values := range []map[string]string{
		{"DISCORD_DM_POLICY": "open\nDISCORD_BOT_TOKEN=x"},
		{"DOKJA_EMBED_MODEL": "${HOME}"},
		{"DISCORD_BOT_TOKEN": "x"},
		{"DOKJA_DISCORD_WEBHOOKS": "1=x"},
	} {
		if env.Write(values) == nil {
			t.Fatalf("%v must be refused", values)
		}
	}
	if raw, _ := os.ReadFile(env.Path); string(raw) != fixtureEnv {
		t.Fatal("a refused write must leave .env untouched")
	}
}

func TestAMissingEnvIsCreatedPrivately(t *testing.T) {
	env := EnvFile{Path: filepath.Join(t.TempDir(), ".env")}
	if values, _, err := env.Read(); err != nil || len(values) != 0 {
		t.Fatal(values, err)
	}
	if err := env.Write(map[string]string{"DOKJA_WHATSAPP_SELF_CHAT": "true"}); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(env.Path); info.Mode().Perm() != 0o600 {
		t.Fatal(info.Mode())
	}
}
