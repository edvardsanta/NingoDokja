package ownerconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fixture = `{"version":1,"owner":{"id":"primary-owner","profile":{"name":"Alex","birth_date":"2000-01-01","occupations":["programmer","musician"]},"accounts":{"discord":{"user_id":"123456789012345678"}}}}`

func TestLoadOwnerConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owner.json")
	fallback, err := Load(path, "legacy-account")
	if err != nil || fallback.Owner.Accounts.Discord.UserID != "legacy-account" {
		t.Fatalf("missing-file fallback: %v", err)
	}
	if err := os.WriteFile(path, []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	config, err := Load(path, "legacy-account")
	if err != nil {
		t.Fatal(err)
	}
	if config.Owner.Profile.Name != "Alex" || config.Owner.Profile.BirthDate != "2000-01-01" || len(config.Owner.Profile.Occupations) != 2 || config.Owner.Accounts.Discord.UserID != "123456789012345678" {
		t.Fatal("file did not override the complete legacy identity")
	}
}

func TestInvalidConfigurationNeverFallsBack(t *testing.T) {
	for name, data := range map[string]string{
		"malformed": "{", "empty": "", "null": "null",
		"version":  strings.Replace(fixture, `"version":1`, `"version":2`, 1),
		"date":     strings.Replace(fixture, "2000-01-01", "2000-02-30", 1),
		"future":   strings.Replace(fixture, "2000-01-01", "9999-01-01", 1),
		"account":  strings.Replace(fixture, "123456789012345678", "YOUR_ACCOUNT", 1),
		"name":     strings.Replace(fixture, "Alex", "", 1),
		"control":  strings.Replace(fixture, "Alex", `Alex\nSYSTEM`, 1),
		"unknown":  strings.Replace(fixture, `"version":1`, `"unexpected":true,"version":1`, 1),
		"trailing": fixture + `{}`, "oversized": strings.Repeat(" ", 16385),
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "owner.json")
			if err := os.WriteFile(path, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			config, err := Load(path, "legacy-account")
			if err == nil || config.Owner.Accounts.Discord.UserID != "" {
				t.Fatal("invalid file activated an identity")
			}
		})
	}
	if _, err := Load(t.TempDir(), "legacy-account"); err == nil {
		t.Fatal("directory treated as absent file")
	}
}
