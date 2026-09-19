package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const secret = "sk-or-v1-SUPERSECRETTOKEN-abcd1234"

func open(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "sub", "dokja.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func good(name string) NewProfile {
	return NewProfile{Name: name, BaseURL: "https://api.example.com/v1", Model: "meta/llama", APIKey: secret}
}

func TestOpenCreatesAPrivateWALDatabaseAndMigratesOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "dokja.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the database holds keys and must be owner-only, got %v %v", info, err)
	}
	var mode string
	db.db.QueryRow("PRAGMA journal_mode").Scan(&mode)
	if mode != "wal" {
		t.Fatalf("expected WAL mode, got %q", mode)
	}
	db.Close()

	again, err := Open(path)
	if err != nil {
		t.Fatalf("reopening must be a no-op migration: %v", err)
	}
	again.Close()

	if _, err := Open(""); err == nil {
		t.Fatal("an empty path must be an error")
	}
}

func TestOpenRefusesADatabaseFromTheFuture(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dokja.db")
	db, _ := Open(path)
	db.db.Exec("PRAGMA user_version = 99")
	db.Close()

	if _, err := Open(path); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("expected a version error, got %v", err)
	}
}

func TestOpenRejectsAFileThatIsNotADatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dokja.db")
	os.WriteFile(path, []byte("this is not sqlite, it is a long enough line of text to look like a corrupt file"), 0o600)
	if _, err := Open(path); err == nil {
		t.Fatal("a corrupt file must be an error")
	}
}

func TestReadsNeverReturnTheToken(t *testing.T) {
	db := open(t)
	if err := db.SaveProfile(good("hosted")); err != nil {
		t.Fatal(err)
	}
	if err := db.UseProfile("hosted"); err != nil {
		t.Fatal(err)
	}

	profiles, err := db.ListProfiles()
	if err != nil {
		t.Fatal(err)
	}
	active, _ := db.ActiveProfile()

	encoded, _ := json.Marshal(map[string]any{"profiles": profiles, "active": active})
	if strings.Contains(string(encoded), "SUPERSECRET") || strings.Contains(string(encoded), secret) {
		t.Fatalf("the token leaked through a read: %s", encoded)
	}
	if fmt.Sprintf("%+v", profiles) == "" || strings.Contains(fmt.Sprintf("%+v %#v", profiles, profiles), "SUPERSECRET") {
		t.Fatal("the token must not appear even in a debug print")
	}
	if len(profiles) != 1 || profiles[0].KeyHint != "…1234" || !profiles[0].Active {
		t.Fatalf("expected one active profile with a masked hint, got %+v", profiles)
	}
}

func TestTheKeyIsStoredForTheChatService(t *testing.T) {
	db := open(t)
	_ = db.SaveProfile(good("hosted"))

	var stored string
	if err := db.db.QueryRow("SELECT api_key FROM chat_profiles WHERE name = 'hosted'").Scan(&stored); err != nil || stored != secret {
		t.Fatalf("the chat service needs the real key in the file, got %q %v", stored, err)
	}
}

func TestSaveProfileValidatesEveryField(t *testing.T) {
	db := open(t)
	bad := map[string]NewProfile{
		"upper case name":  {Name: "Hosted", BaseURL: "https://x.example", Model: "m", APIKey: secret},
		"path in name":     {Name: "../etc", BaseURL: "https://x.example", Model: "m", APIKey: secret},
		"empty name":       {Name: "", BaseURL: "https://x.example", Model: "m", APIKey: secret},
		"file url":         {Name: "a", BaseURL: "file:///etc/passwd", Model: "m", APIKey: secret},
		"no scheme":        {Name: "a", BaseURL: "api.example.com", Model: "m", APIKey: secret},
		"no model":         {Name: "a", BaseURL: "https://x.example", Model: " ", APIKey: secret},
		"no token":         {Name: "a", BaseURL: "https://x.example", Model: "m", APIKey: ""},
		"token with space": {Name: "a", BaseURL: "https://x.example", Model: "m", APIKey: "sk abc"},
		"token newline":    {Name: "a", BaseURL: "https://x.example", Model: "m", APIKey: "sk-abc\n"},
	}
	for name, profile := range bad {
		if err := db.SaveProfile(profile); err == nil {
			t.Errorf("%s: expected a validation error", name)
		}
	}
	if profiles, _ := db.ListProfiles(); len(profiles) != 0 {
		t.Fatalf("rejected profiles must not be stored, got %+v", profiles)
	}
	local := NewProfile{Name: "local", BaseURL: "http://localhost:11434/v1", Model: "llama", APIKey: "ollama-local-key"}
	if err := db.SaveProfile(local); err != nil {
		t.Fatalf("a plain http local server is allowed: %v", err)
	}
}

func TestSaveProfileReplacesInsteadOfDuplicating(t *testing.T) {
	db := open(t)
	_ = db.SaveProfile(good("p"))
	updated := good("p")
	updated.Model, updated.APIKey = "other-model", "sk-second-key-9999"
	if err := db.SaveProfile(updated); err != nil {
		t.Fatal(err)
	}

	profiles, _ := db.ListProfiles()
	if len(profiles) != 1 || profiles[0].Model != "other-model" || profiles[0].KeyHint != "…9999" {
		t.Fatalf("expected the profile to be replaced, got %+v", profiles)
	}
}

func TestUseAndDeleteProfiles(t *testing.T) {
	db := open(t)
	_ = db.SaveProfile(good("a"))
	_ = db.SaveProfile(good("b"))

	if err := db.UseProfile("ghost"); err == nil {
		t.Fatal("selecting an unknown profile must fail")
	}
	if active, _ := db.ActiveProfile(); active != "" {
		t.Fatalf("a failed selection must not change anything, got %q", active)
	}

	_ = db.UseProfile("a")
	if err := db.DeleteProfile("a", false); err == nil {
		t.Fatal("deleting the active profile needs force")
	}
	if err := db.DeleteProfile("b", false); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteProfile("b", false); err == nil {
		t.Fatal("deleting a missing profile must fail")
	}
	if err := db.DeleteProfile("a", true); err != nil {
		t.Fatal(err)
	}
	if active, _ := db.ActiveProfile(); active != "" {
		t.Fatalf("deleting the active profile must clear the selection, got %q", active)
	}
}

func TestSeveralProcessesCanUseTheSameFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dokja.db")
	first, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			db := first
			if i%2 == 1 {
				db = second
			}
			for n := 0; n < 10; n++ {
				if err := db.SaveProfile(good(fmt.Sprintf("p%d", i))); err != nil {
					t.Errorf("writer %d: %v", i, err)
					return
				}
				if _, err := db.ListProfiles(); err != nil {
					t.Errorf("reader %d: %v", i, err)
					return
				}
			}
		}(i)
	}
	wg.Wait()

	if profiles, _ := first.ListProfiles(); len(profiles) != 6 {
		t.Fatalf("both handles should see all 6 profiles, got %d", len(profiles))
	}
}

func TestShortKeysGetNoHint(t *testing.T) {
	if keyHint("short") != "" || keyHint("sk-1234567890") != "…7890" {
		t.Fatalf("unexpected hints %q %q", keyHint("short"), keyHint("sk-1234567890"))
	}
}

// The chat service (Python) reads these tables directly. If this test needs changing,
// dokja_services/dokja_chat_ai/profiles.py needs the same change.
func TestSchemaMatchesWhatTheChatServiceReads(t *testing.T) {
	db := open(t)
	_ = db.SaveProfile(good("hosted"))
	_ = db.UseProfile("hosted")

	var name, baseURL, model, apiKey, updatedAt string
	err := db.db.QueryRow(`
		SELECT p.name, p.base_url, p.model, p.api_key, p.updated_at
		FROM settings s JOIN chat_profiles p ON p.name = s.value
		WHERE s.key = 'chat_active_profile'`).Scan(&name, &baseURL, &model, &apiKey, &updatedAt)
	if err != nil {
		t.Fatalf("the query profiles.py runs no longer works: %v", err)
	}
	if name != "hosted" || apiKey != secret || updatedAt == "" {
		t.Fatalf("unexpected row %q %q %q", name, apiKey, updatedAt)
	}

	// Editing a profile must change updated_at, which is how the chat service notices.
	before := updatedAt
	updated := good("hosted")
	updated.APIKey = "sk-rotated-key-000011112222"
	time.Sleep(2 * time.Millisecond)
	_ = db.SaveProfile(updated)
	db.db.QueryRow("SELECT updated_at FROM chat_profiles WHERE name = 'hosted'").Scan(&updatedAt)
	if updatedAt == before {
		t.Fatal("updated_at must change when a profile is edited")
	}
}
