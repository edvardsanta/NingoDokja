package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type providerStoreFixture struct {
	preferences ProviderPreferences
	writes      int
	err         error
}

func (f *providerStoreFixture) ReadProviders() (ProviderPreferences, error) {
	return f.preferences, f.err
}
func (f *providerStoreFixture) WriteProvider(revision uint64, p ProviderRegistration) (uint64, error) {
	if revision != f.preferences.Revision {
		return 0, ErrProviderConflict
	}
	for i, row := range f.preferences.Providers {
		if row.ID == p.ID {
			f.preferences.Providers[i] = p
		}
	}
	f.preferences.Revision++
	f.writes++
	return f.preferences.Revision, nil
}
func preferencesFixture(t *testing.T) *providerStoreFixture {
	t.Helper()
	p, err := DecodeProviderPreferences(1, DefaultProviderValues())
	if err != nil {
		t.Fatal(err)
	}
	return &providerStoreFixture{preferences: p}
}

func TestProviderRegistrationNormalizesDeclarationsWithoutVerifyingInference(t *testing.T) {
	d, f := fresh()
	store := preferencesFixture(t)
	d.WithProviderSettings(store).WithProviders(providerFixture{{ID: "codex", State: "installed"}})
	result, err := d.ConfigureProvider(1, ProviderRegistration{ID: "codex", Enabled: true, Models: []string{" fixture/model-b ", "fixture/model-a", "fixture/model-b"}})
	if err != nil || result.Revision != 2 {
		t.Fatal(result, err)
	}
	row := d.Providers(context.Background()).Providers[0]
	if strings.Join(row.DeclaredModels, ",") != "fixture/model-a,fixture/model-b" || len(row.Models) != 0 || row.InferenceStatus != "not_tested" {
		t.Fatal(row)
	}
	if len(f.trace()) != 0 {
		t.Fatal("registration performed lifecycle operations")
	}
}
func TestInvalidOrStaleProviderRegistrationWritesNothing(t *testing.T) {
	d, _ := fresh()
	store := preferencesFixture(t)
	d.WithProviderSettings(store)
	for _, p := range []ProviderRegistration{
		{ID: "unknown", Models: []string{}},
		{ID: "codex", Models: []string{"model\nTOKEN=x"}},
		{ID: "codex", Models: []string{""}},
		{ID: "codex", Models: make([]string, MaxProviderModels+1)},
	} {
		if _, err := d.ConfigureProvider(1, p); err == nil {
			t.Fatal("invalid registration accepted")
		}
	}
	for _, revision := range []uint64{0, 2, MaxProviderRevision} {
		if _, err := d.ConfigureProvider(revision, ProviderRegistration{ID: "codex"}); err == nil {
			t.Fatal("invalid revision accepted")
		}
	}
	if store.writes != 0 {
		t.Fatal("refused registration wrote data")
	}
}
func TestDisabledRegistrationRemainsSeparateFromAvailability(t *testing.T) {
	d, _ := fresh()
	store := preferencesFixture(t)
	d.WithProviderSettings(store).WithProviders(providerFixture{{ID: "codex", State: "installed"}})
	if _, err := d.ConfigureProvider(1, ProviderRegistration{ID: "codex", Enabled: false, Models: []string{"fixture/model"}}); err != nil {
		t.Fatal(err)
	}
	row := d.Providers(context.Background()).Providers[0]
	if row.Enabled || row.State != "unavailable" || row.Reason != "disabled" || row.DeclaredModels[0] != "fixture/model" {
		t.Fatal(row)
	}
	store.err = errors.New("fixture failure")
	view := d.Providers(context.Background())
	if view.Error == "" || len(view.Providers) != 0 {
		t.Fatal("failed configuration fell back to defaults", view)
	}
}
func TestCorruptProviderPreferencesAreRejected(t *testing.T) {
	for _, mutate := range []func(map[string]string){
		func(v map[string]string) { delete(v, "codex.enabled") },
		func(v map[string]string) { v["codex.enabled"] = "1" },
		func(v map[string]string) { v["secret"] = "fixture-value" },
		func(v map[string]string) { v["ollama.models"] = "model,invalid name" },
	} {
		values := DefaultProviderValues()
		mutate(values)
		if _, err := DecodeProviderPreferences(1, values); err == nil {
			t.Fatal("corrupt preferences accepted")
		}
	}
}
