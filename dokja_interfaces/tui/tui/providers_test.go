package tui

import (
	"dokja_interfaces/cli/cli"
	"strings"
	"testing"
)

func TestProvidersAreInspectedOnlyOnDemandFromSettings(t *testing.T) {
	m, runtime := settingsModel(t)
	runtime.providers = cli.ProvidersView{Providers: []cli.ProviderRow{
		{ID: "codex", Name: "Codex", State: "installed", ModelDiscovery: "unsupported", InferenceStatus: "not_tested"},
		{ID: "ollama", Name: "Ollama", State: "reachable", ModelDiscovery: "listed", Models: []string{"fixture/model:latest"}, InferenceStatus: "not_tested"},
	}}
	press(m, "c")
	for _, c := range runtime.calls {
		if c.eventType == "runtime.providers" {
			t.Fatal("inventory fetched before requested")
		}
	}
	press(m, "p")
	if m.overlay != overlayProviders {
		t.Fatal(m.overlay)
	}
	for _, want := range []string{"Codex", "fixture/model:latest", tr("providers_discovery_only"), tr("provider_installed")} {
		if !strings.Contains(m.View(), want) {
			t.Fatal(m.View())
		}
	}
	press(m, "r", "esc")
	count := 0
	for _, c := range runtime.calls {
		if c.eventType == "runtime.providers" {
			count++
		}
		if c.eventType == "runtime.set" || c.eventType == "runtime.settings.set" {
			t.Fatal(c)
		}
	}
	if count != 2 || m.overlay != overlaySettings {
		t.Fatal(count, m.overlay)
	}
}
