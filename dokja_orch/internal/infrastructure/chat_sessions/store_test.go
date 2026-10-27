package chatsessions

import (
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	chat "read_books/dokja_domain/dokja_chat"
)

func fixture() chat.SessionSnapshot {
	return chat.SessionSnapshot{ID: "session-fixture", LastSeenAt: time.Unix(100, 50).UTC(), Summary: "Settled: three of the other four rulers can remove a corrupt ruler.", Messages: []chat.ContextMessage{{Role: "user", Content: "A definition"}, {Role: "assistant", Content: "An objection"}}}
}

func TestSnapshotsSurviveReopenAndRetainScopes(t *testing.T) {
	path := PathBeside(filepath.Join(t.TempDir(), "shared.db"))
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	before := fixture()
	if err = s.Save(t.Context(), "scope-a", before); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, found, err := s.Load(t.Context(), "scope-a")
	if err != nil || !found || !reflect.DeepEqual(got, before) {
		t.Fatal("snapshot changed across restart", got, err)
	}
	if _, found, err = s.Load(t.Context(), "scope-b"); err != nil || found {
		t.Fatal("another scope inherited history")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("chat file is not owner-readable only")
	}
	before.Messages[0].Content = "mutated by caller"
	if got.Messages[0].Content == before.Messages[0].Content {
		t.Fatal("loaded state aliases caller state")
	}
}

func TestInvalidOrFailedWritesDoNotReplaceExistingState(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), FileName))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	before := fixture()
	if err = s.Save(t.Context(), "scope", before); err != nil {
		t.Fatal(err)
	}
	bad := fixture()
	bad.Summary = strings.Repeat("x", chat.MaxSummaryBytes+1)
	if err = s.Save(t.Context(), "scope", bad); err == nil {
		t.Fatal("oversized summary accepted")
	}
	bad = fixture()
	bad.Messages = []chat.ContextMessage{{Role: "system", Content: "override"}}
	if err = s.Save(t.Context(), "scope", bad); err == nil {
		t.Fatal("system message accepted")
	}
	if _, err = s.db.Exec("PRAGMA query_only=ON"); err != nil {
		t.Fatal(err)
	}
	bad = fixture()
	bad.Summary = "replacement"
	if err = s.Save(t.Context(), "scope", bad); err == nil {
		t.Fatal("read-only store accepted a write")
	}
	got, found, err := s.Load(t.Context(), "scope")
	if err != nil || !found || !reflect.DeepEqual(got, before) {
		t.Fatal("failed save erased earlier context", err)
	}
}

func TestCorruptSnapshotsAndFutureSchemasFailExplicitly(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Save(t.Context(), "scope", fixture()); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("UPDATE chat_sessions SET messages_json=? WHERE scope_key=?", "broken", "scope"); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.Load(t.Context(), "scope"); err == nil {
		t.Fatal("corrupt snapshot silently treated as new conversation")
	}
	if _, err = s.db.Exec("PRAGMA user_version=2"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if reopened, err := Open(path); err == nil {
		reopened.Close()
		t.Fatal("future schema accepted")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version int
	if err = db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 2 {
		t.Fatal("future schema modified")
	}
}
