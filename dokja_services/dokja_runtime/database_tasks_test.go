package runtime

import (
	"errors"
	domain "read_books/dokja_domain/dokja_runtime"
	store "read_books/dokja_store"
	"testing"
)

func TestTaskProfilesPersistIndependentlyAndRejectStaleWriters(t *testing.T) {
	s := databaseFixture(t)
	providers, _ := s.ReadProviders()
	p, err := s.ReadTaskProfiles()
	if err != nil {
		t.Fatal(err)
	}
	p.Profiles["analysis"] = []domain.ChatBinding{{Provider: "kimi", Model: "fixture"}}
	saved, err := s.WriteTaskProfiles(p)
	if err != nil || saved.Revision != 2 {
		t.Fatal(saved, err)
	}
	if _, err = s.WriteTaskProfiles(p); !errors.Is(err, domain.ErrProviderConflict) {
		t.Fatal(err)
	}
	db, err := store.Open(s.DB.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	reopened := &DatabaseSettings{DB: db}
	got, err := reopened.ReadTaskProfiles()
	if err != nil || got.Profiles["analysis"][0].Provider != "kimi" {
		t.Fatal(got, err)
	}
	after, _ := s.ReadProviders()
	if after.Revision != providers.Revision {
		t.Fatal("task write changed provider revision")
	}
}
