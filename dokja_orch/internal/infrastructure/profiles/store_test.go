package profiles

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	conversation "read_books/dokja_domain/dokja_conversation"
)

func TestProfilesAndPersonSessionsSurviveAReopenAndKeepOnlyTheLatest(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	store, err := Open(path)
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
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	kept, err := store.AllProfiles(t.Context())
	if err != nil || len(kept) != 2 {
		t.Fatal(kept, err)
	}
	if kept[1].Person != "person:Sam" || kept[1].Summary != "new" || kept[1].Topics[0] != "games" || !kept[1].GeneratedAt.Equal(second.GeneratedAt) {
		t.Fatalf("%+v", kept[1])
	}
	sessions, err := store.PersonSessions(t.Context())
	if err != nil || len(sessions) != 1 || sessions["whatsapp:group:sender"] != "person:Kim" {
		t.Fatal(sessions, err)
	}
}

func TestInvalidRowsAreRefused(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), FileName))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveProfile(t.Context(), conversation.Profile{Summary: "nobody"}); err == nil {
		t.Fatal("a profile of nobody was kept")
	}
	if err := store.SaveProfile(t.Context(), conversation.Profile{Person: "person:Sam", Summary: strings.Repeat("x", maxProfileBytes)}); err == nil {
		t.Fatal("an unbounded profile was kept")
	}
	if err := store.NoteSession(t.Context(), "", "person:Sam"); err == nil {
		t.Fatal("an empty session key was kept")
	}
	if _, err := Open(""); err == nil {
		t.Fatal("an empty path opened")
	}
}
