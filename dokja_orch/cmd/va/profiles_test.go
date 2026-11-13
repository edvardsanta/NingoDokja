package main

import (
	"path/filepath"
	"testing"
)

func TestProfilesAreKeptBesideTheSharedDatabaseWithoutADSN(t *testing.T) {
	book, closeBook := configureProfiles("", filepath.Join(t.TempDir(), "shared.db"))
	defer closeBook()
	if book == nil {
		t.Fatal("profiles must default to the file beside the shared database")
	}
}

func TestAnUnusableDSNDisablesProfilesInsteadOfKeepingThemElsewhere(t *testing.T) {
	for name, dsn := range map[string]string{
		"unreachable": "postgres://user:secret-pass@127.0.0.1:1/none?sslmode=disable&connect_timeout=1",
		"malformed":   "postgres://user:secret-pass@[::1/none",
	} {
		t.Run(name, func(t *testing.T) {
			book, closeBook := configureProfiles(dsn, filepath.Join(t.TempDir(), "shared.db"))
			defer closeBook()
			if book != nil {
				t.Fatal("profiles must not fall back to another store when a database is configured")
			}
		})
	}
}
