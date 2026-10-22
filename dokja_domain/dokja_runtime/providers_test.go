package runtime

import (
	"context"
	"testing"
)

type providerFixture []ProviderObservation

func (p providerFixture) Inspect(context.Context) []ProviderObservation { return p }

func TestProviderInventoryCannotClaimSuccessfulInference(t *testing.T) {
	d, f := fresh()
	d.WithProviders(providerFixture{
		{ID: "codex", State: "installed", Models: []string{"invented/model"}},
		{ID: "claude_code", State: "unavailable", Reason: "executable_missing"},
		{ID: "ollama", State: "reachable", Models: []string{"fixture/model:latest", "bge-m3:latest", "fixture/model:latest"}},
		{ID: "arbitrary", State: "reachable"},
	})
	v := d.Providers(context.Background())
	if len(v.Providers) != 3 || v.Providers[0].State != "installed" || len(v.Providers[0].Models) != 0 || v.Providers[0].ModelDiscovery != "unsupported" {
		t.Fatal(v)
	}
	if len(v.Providers[2].Models) != 2 || v.Providers[2].Models[0] != "bge-m3:latest" {
		t.Fatal(v)
	}
	for _, row := range v.Providers {
		if row.InferenceStatus != "not_tested" {
			t.Fatal(row)
		}
	}
	if len(f.trace()) != 0 {
		t.Fatal("inventory performed lifecycle operations")
	}
}

func TestMalformedModelsAndMissingInspectorRemainUnavailable(t *testing.T) {
	d, _ := fresh()
	for _, row := range d.Providers(context.Background()).Providers {
		if row.State != "unavailable" || len(row.Models) != 0 {
			t.Fatal(row)
		}
	}
	d.WithProviders(providerFixture{{ID: "ollama", State: "reachable", Models: []string{"safe/model", "bad\nsecret"}}})
	row := d.Providers(context.Background()).Providers[2]
	if row.State != "unavailable" || row.Reason != "invalid_response" || len(row.Models) != 0 {
		t.Fatal(row)
	}
}
