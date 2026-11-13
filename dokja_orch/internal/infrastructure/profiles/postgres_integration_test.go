//go:build integration

package profiles

import (
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	conversation "read_books/dokja_domain/dokja_conversation"
)

// database opens the disposable profiles_test database named by DOKJA_PROFILES_TEST_DSN and drops
// this namespace, so every test starts from nothing.
func database(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("DOKJA_PROFILES_TEST_DSN")
	if dsn == "" {
		t.Fatal("integration tests require a disposable DOKJA_PROFILES_TEST_DSN")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal("open fixture database failed")
	}
	t.Cleanup(func() { db.Close() })
	var name string
	if err := db.QueryRow(`SELECT current_database()`).Scan(&name); err != nil || name != "profiles_test" {
		t.Fatal("profile integration fixtures require the disposable profiles_test database")
	}
	if _, err := db.Exec(`DROP SCHEMA IF EXISTS dokja_profiles CASCADE`); err != nil {
		t.Fatal("reset fixture schema failed")
	}
	return db
}

func TestOpeningNeverCreatesTheNamespaceAndInitializingIsIdempotent(t *testing.T) {
	db := database(t)
	if _, err := OpenPostgres(t.Context(), db); !errors.Is(err, ErrSchema) {
		t.Fatal("an uninitialized namespace was opened", err)
	}
	if _, err := InitializePostgres(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	store, err := InitializePostgres(t.Context(), db)
	if err != nil {
		t.Fatal("initializing twice failed", err)
	}
	if _, err := OpenPostgres(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE dokja_profiles.schema_version SET version = 99`); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenPostgres(t.Context(), db); !errors.Is(err, ErrSchema) {
		t.Fatal("another schema version was opened", err)
	}
	if _, err := InitializePostgres(t.Context(), db); !errors.Is(err, ErrSchema) {
		t.Fatal("another schema version was initialized over", err)
	}
	_ = store
}

func TestProfilesAndPersonSessionsKeepOnlyTheLatestInPostgres(t *testing.T) {
	db := database(t)
	store, err := InitializePostgres(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	first := conversation.Profile{Person: "person:Sam", Summary: "old", GeneratedAt: time.Unix(10, 0).UTC(), Exchanges: 3}
	second := first
	second.Summary, second.Topics, second.GeneratedAt = "new", []string{"games"}, time.Unix(20, 0).UTC()
	for _, profile := range []conversation.Profile{first, second, {Person: conversation.OwnerPerson, Summary: "owner", GeneratedAt: time.Unix(5, 0).UTC()}} {
		if err := store.SaveProfile(t.Context(), profile); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.NoteSession(t.Context(), "whatsapp:group:sender", "person:Sam"); err != nil {
		t.Fatal(err)
	}
	if err := store.NoteSession(t.Context(), "whatsapp:group:sender", "person:Kim"); err != nil {
		t.Fatal(err)
	}
	kept, err := store.AllProfiles(t.Context())
	if err != nil || len(kept) != 2 || kept[0].Person != conversation.OwnerPerson {
		t.Fatal(kept, err)
	}
	if kept[1].Summary != "new" || kept[1].Topics[0] != "games" || !kept[1].GeneratedAt.Equal(second.GeneratedAt) {
		t.Fatalf("%+v", kept[1])
	}
	sessions, err := store.PersonSessions(t.Context())
	if err != nil || len(sessions) != 1 || sessions["whatsapp:group:sender"] != "person:Kim" {
		t.Fatal(sessions, err)
	}
}

func TestPostgresRefusesInvalidRowsAndNeverLeaksDiagnostics(t *testing.T) {
	db := database(t)
	store, err := InitializePostgres(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveProfile(t.Context(), conversation.Profile{Summary: "nobody"}); err == nil {
		t.Fatal("a profile of nobody was kept")
	}
	if err := store.SaveProfile(t.Context(), conversation.Profile{Person: "person:Sam", Summary: strings.Repeat("x", maxProfileBytes)}); err == nil {
		t.Fatal("an unbounded profile was kept")
	}
	if err := store.NoteSession(t.Context(), "", "person:Sam"); err == nil {
		t.Fatal("an empty session key was kept")
	}
	db.Close()
	if err := store.NoteSession(t.Context(), "key", "person:Sam"); err == nil || !errors.Is(err, ErrStorage) {
		t.Fatal("a storage failure must be the safe error", err)
	}
}
