package runtime

import (
	"context"
	"errors"
	"os"
	"testing"

	domain "read_books/dokja_domain/dokja_runtime"
	store "read_books/dokja_store"
)

func TestProviderRegistrationsSurviveReopenAndDoNotChangeOtherConfiguration(t *testing.T) {
	s := databaseFixture(t)
	if err := s.Write(map[string]string{"DISCORD_DM_POLICY": "closed"}); err != nil {
		t.Fatal(err)
	}
	before, _, _ := s.DB.RuntimeConfiguration()
	p, err := s.ReadProviders()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.WriteProvider(p.Revision, domain.ProviderRegistration{ID: "codex", Enabled: false, Models: []string{"fixture/model"}}); err != nil {
		t.Fatal(err)
	}
	path := s.DB.Path()
	s.Close()
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s.DB = db
	p, err = s.ReadProviders()
	if err != nil || p.Revision != 2 || p.Providers[0].Enabled || p.Providers[0].Models[0] != "fixture/model" {
		t.Fatal(p, err)
	}
	after, _, _ := s.DB.RuntimeConfiguration()
	if after.Revision != before.Revision || after.Values["discord.direct_messages"] != "closed" {
		t.Fatal(after)
	}
	if raw, _ := os.ReadFile(s.Legacy.Path); string(raw) != fixtureEnv {
		t.Fatal("legacy configuration changed")
	}
}
func TestProviderConflictRefusesStaleWriteAndPreservesOtherRows(t *testing.T) {
	s := databaseFixture(t)
	p, err := s.ReadProviders()
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(s.DB.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	other := &DatabaseSettings{DB: db}
	if _, err = s.WriteProvider(p.Revision, domain.ProviderRegistration{ID: "codex", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if _, err = other.WriteProvider(p.Revision, domain.ProviderRegistration{ID: "ollama", Enabled: false}); !errors.Is(err, domain.ErrProviderConflict) {
		t.Fatal(err)
	}
	current, err := other.ReadProviders()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = other.WriteProvider(current.Revision, domain.ProviderRegistration{ID: "ollama", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	final, err := s.ReadProviders()
	if err != nil || final.Providers[0].Enabled || final.Providers[2].Enabled || !final.Providers[1].Enabled {
		t.Fatal(final, err)
	}
	if _, err = s.WriteProvider(final.Revision, domain.ProviderRegistration{ID: "codex", Models: []string{"bad\ncredential"}}); err == nil {
		t.Fatal("invalid values accepted")
	}
}
func TestDisabledProvidersAreNotInspected(t *testing.T) {
	calls := 0
	p := ProviderDiscovery{LookPath: func(string) (string, error) { calls++; return "", errors.New("missing") }, Compose: &Compose{Run: func(context.Context, ...string) ([]byte, error) { calls++; return nil, nil }}}
	if rows := p.Inspect(context.Background(), map[string]bool{}); len(rows) != 0 || calls != 0 {
		t.Fatal(rows, calls)
	}
}
