package runtime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	store "read_books/dokja_store"
)

func databaseFixture(t *testing.T) *DatabaseSettings {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "dokja.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return &DatabaseSettings{DB: db, Legacy: fixture(t)}
}

func TestDatabaseSettingsImportOnceAndPreserveCredentials(t *testing.T) {
	s := databaseFixture(t)
	s.Lookup = func(key string) (string, bool) { return "closed", key == "DISCORD_DM_POLICY" }
	values, hooks, err := s.Read()
	if err != nil || values["DISCORD_DM_POLICY"] != "closed" || len(hooks) != 1 || hooks[0] != "102" {
		t.Fatal(values, hooks, err)
	}
	c, _, err := s.DB.RuntimeConfiguration()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(c)
	for _, forbidden := range []string{"fixture-token-value", "fixture-key", "hooks.example", "DISCORD_BOT_TOKEN", "DISCORD_DM_POLICY"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatal("configuration leaked a credential or compatibility key")
		}
	}
	if err := s.Write(map[string]string{"DISCORD_DM_POLICY": "open"}); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(s.Legacy.Path); string(raw) != fixtureEnv {
		t.Fatal("legacy file changed")
	}
	if info, _ := os.Stat(s.Legacy.Path); info.Mode().Perm() != 0o640 {
		t.Fatal("legacy mode changed")
	}
	path := s.DB.Path()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s.DB = db
	// Neither a new inherited override nor later manual legacy edits win after import.
	s.Lookup = func(key string) (string, bool) { return "closed", key == "DISCORD_DM_POLICY" }
	if err := os.WriteFile(s.Legacy.Path, []byte("DISCORD_DM_POLICY=closed\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	values, _, err = s.Read()
	if err != nil || values["DISCORD_DM_POLICY"] != "open" {
		t.Fatal(values, err)
	}
}

func TestRefusedDatabaseWritesAreAtomic(t *testing.T) {
	s := databaseFixture(t)
	if _, _, err := s.Read(); err != nil {
		t.Fatal(err)
	}
	before, _, _ := s.DB.RuntimeConfiguration()
	for _, delta := range []map[string]string{
		{"DISCORD_DM_POLICY": "closed", "DISCORD_BOT_TOKEN": "x"},
		{"DOKJA_DISCORD_WEBHOOKS": "x"},
		{"DISCORD_DM_POLICY": "closed\nOTHER=x"},
		{"DOKJA_EMBED_MODEL": "example/model"},
	} {
		if err := s.Write(delta); err == nil {
			t.Fatal("invalid update accepted")
		}
	}
	after, _, _ := s.DB.RuntimeConfiguration()
	if before.Revision != after.Revision || after.Values["discord.direct_messages"] != "open" {
		t.Fatal(after)
	}
}

func TestInvalidLegacySettingsLeaveImportUninitialized(t *testing.T) {
	for _, input := range []string{"DISCORD_DM_POLICY=invalid\n", "DOKJA_EMBED_MODEL=example/model\n"} {
		s := databaseFixture(t)
		if err := os.WriteFile(s.Legacy.Path, []byte(input), 0o640); err != nil {
			t.Fatal(err)
		}
		if _, _, err := s.Read(); err == nil {
			t.Fatal("invalid import accepted")
		}
		if _, exists, err := s.DB.RuntimeConfiguration(); err != nil || exists {
			t.Fatal(exists, err)
		}
		if raw, _ := os.ReadFile(s.Legacy.Path); string(raw) != input {
			t.Fatal("refused import changed legacy file")
		}
	}
}

func TestComposeReceivesDatabaseValuesWithoutChangingLegacyFile(t *testing.T) {
	s := databaseFixture(t)
	if err := s.Write(map[string]string{"DISCORD_DM_POLICY": "closed"}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	script := `#!/bin/sh
printf '%s|%s' "$DISCORD_DM_POLICY" "$FIXTURE_UNMANAGED" > "$FIXTURE_CAPTURE"
`
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	capture := filepath.Join(dir, "capture")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DOCKER_HOST", "")
	t.Setenv("DOCKER_CONTEXT", "")
	t.Setenv("CONTAINER_HOST", "")
	t.Setenv("DISCORD_DM_POLICY", "open")
	t.Setenv("FIXTURE_UNMANAGED", "kept")
	t.Setenv("FIXTURE_CAPTURE", capture)
	c := New(dir)
	c.Environment = s.Environment
	if _, err := c.Run(context.Background(), "config"); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(capture); err != nil || string(raw) != "closed|kept" {
		t.Fatal(string(raw), err)
	}
	if raw, _ := os.ReadFile(s.Legacy.Path); string(raw) != fixtureEnv {
		t.Fatal("compose projection rewrote legacy file")
	}
	if err := os.Remove(capture); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if _, err := c.Run(context.Background(), "config"); err == nil {
		t.Fatal("unavailable database silently fell back to environment")
	}
	if _, err := os.Stat(capture); !os.IsNotExist(err) {
		t.Fatal("engine ran without valid configuration")
	}
}

func TestOversizedLegacyLineIsNotSilentlyIgnored(t *testing.T) {
	env := fixture(t)
	input := strings.Repeat("x", 1024*1024+1) + "\nDISCORD_DM_POLICY=closed\n"
	if err := os.WriteFile(env.Path, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := env.Read(); err == nil {
		t.Fatal("incomplete legacy import accepted")
	}
}

func TestASettingNewToAnExistingDatabaseComesFromTheLegacyFileOnceThenTheDatabaseDecides(t *testing.T) {
	s := databaseFixture(t)
	if _, _, err := s.Read(); err != nil {
		t.Fatal(err)
	}
	// A database created before the setting existed, and a legacy file that names conversations.
	c, _, err := s.DB.RuntimeConfiguration()
	if err != nil {
		t.Fatal(err)
	}
	older := map[string]string{}
	for id, value := range c.Values {
		if id != "whatsapp.conversations" {
			older[id] = value
		}
	}
	path := filepath.Join(t.TempDir(), "older.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.InitializeRuntimeConfiguration(older); err != nil {
		t.Fatal(err)
	}
	s.DB = db
	legacy := fixtureEnv + "DOKJA_WHATSAPP_CONVERSATIONS=eu=5511912345678\n"
	if err := os.WriteFile(s.Legacy.Path, []byte(legacy), 0o640); err != nil {
		t.Fatal(err)
	}

	values, _, err := s.Read()
	if err != nil || values["DOKJA_WHATSAPP_CONVERSATIONS"] != "eu=5511912345678" {
		t.Fatal("the existing value must be imported", values, err)
	}
	if err := s.Write(map[string]string{"DOKJA_WHATSAPP_CONVERSATIONS": "eu=5511912345678,ningo=120363000000000001@g.us"}); err != nil {
		t.Fatal(err)
	}
	env, err := s.Environment([]string{"DOKJA_WHATSAPP_CONVERSATIONS=from-the-shell", "UNRELATED=kept"})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(env, "\n")
	if !strings.Contains(joined, "DOKJA_WHATSAPP_CONVERSATIONS=eu=5511912345678,ningo=120363000000000001@g.us") || strings.Contains(joined, "from-the-shell") || !strings.Contains(joined, "UNRELATED=kept") {
		t.Fatal("the database must take precedence over the environment", joined)
	}
	if err := s.Write(map[string]string{"DOKJA_WHATSAPP_CONVERSATIONS": ""}); err != nil {
		t.Fatal(err)
	}
	if values, _, err = s.Read(); err != nil || values["DOKJA_WHATSAPP_CONVERSATIONS"] != "" {
		t.Fatal("clearing the list must not bring the legacy value back", values, err)
	}
	if raw, _ := os.ReadFile(s.Legacy.Path); string(raw) != legacy {
		t.Fatal("the legacy file must not change")
	}
}
