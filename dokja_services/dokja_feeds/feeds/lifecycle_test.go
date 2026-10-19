//go:build unix

package feeds

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// v1 returns a valid versioned extractor manifest, changed by overrides (nil removes a key).
func v1(overrides map[string]any) map[string]any {
	manifest := map[string]any{
		"plugin_api": 1, "version": "0.1.0", "runtime": "subprocess",
		"provides": []string{CapabilityDocumentExtract}, "formats": []string{".pdf"},
		"command": []string{"./run.sh"}, "limits": map[string]any{"timeout_ms": 5000, "output_bytes": 4096},
		"category": nil,
	}
	for key, value := range overrides {
		manifest[key] = value
	}
	return manifest
}

func TestALegacyManifestKeepsItsShapeAndProvidesCollection(t *testing.T) {
	root := t.TempDir()
	newPlugin(t, root, "legacy", nil)

	found := discoverOne(t, root)
	if found.Err != nil || found.Plugin.APIVersion != 0 || !found.Plugin.Collects() || found.Plugin.OutputBytes != 0 {
		t.Fatalf("a manifest without plugin_api is the legacy feed shape: %#v, %v", found.Plugin, found.Err)
	}
}

func TestAnExplicitVersionIsNeverReadAsLegacy(t *testing.T) {
	root := t.TempDir()
	newPlugin(t, root, "mixed", map[string]any{"plugin_api": 1})

	found := discoverOne(t, root)
	if found.Err == nil || !strings.Contains(found.Err.Error(), "unknown field") {
		t.Fatalf("a legacy body with plugin_api 1 must fail the versioned validator: %v", found.Err)
	}
}

func TestUnsupportedVersionsRuntimesAndCapabilitiesAreIncompatible(t *testing.T) {
	cases := map[string]map[string]any{
		"lifecycle 2":     {"plugin_api": 2},
		"wasm runtime":    {"runtime": "wasm"},
		"unknown ability": {"provides": []string{"speech.transcribe.v1"}},
	}
	for name, overrides := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			newPlugin(t, root, "future", v1(overrides))

			found := discoverOne(t, root)
			if !errors.Is(found.Err, ErrIncompatible) || !strings.HasPrefix(found.Err.Error(), "incompatible:") {
				t.Fatalf("expected a stable incompatibility, got %v", found.Err)
			}
		})
	}
}

func TestAMalformedVersionIsInvalidNotIncompatible(t *testing.T) {
	for _, version := range []any{"1", 0, 1.5, -1} {
		root := t.TempDir()
		newPlugin(t, root, "odd", v1(map[string]any{"plugin_api": version}))

		found := discoverOne(t, root)
		if found.Err == nil || errors.Is(found.Err, ErrIncompatible) {
			t.Fatalf("plugin_api %v: expected an invalid manifest, got %v", version, found.Err)
		}
	}
}

func TestAVersionedExtractorIsDisabledUntilEnabled(t *testing.T) {
	root := t.TempDir()
	newPlugin(t, root, "reader", v1(nil))

	found := discoverOne(t, root)
	plugin := found.Plugin
	if found.Err != nil || plugin.Enabled || plugin.Collects() || plugin.APIVersion != 1 || plugin.Version != "0.1.0" ||
		plugin.Timeout != 5*time.Second || plugin.OutputBytes != 4096 || plugin.Formats[0] != ".pdf" {
		t.Fatalf("unexpected versioned plugin: %#v, %v", plugin, found.Err)
	}
}

func TestAVersionedManifestIsValidatedStrictly(t *testing.T) {
	cases := map[string]map[string]any{
		"unknown field":            {"colour": "blue"},
		"no version":               {"version": nil},
		"no limits":                {"limits": nil},
		"zero output":              {"limits": map[string]any{"timeout_ms": 5000, "output_bytes": 0}},
		"timeout out of range":     {"limits": map[string]any{"timeout_ms": 1, "output_bytes": 10}},
		"nothing provided":         {"provides": []string{}},
		"capability twice":         {"provides": []string{CapabilityDocumentExtract, CapabilityDocumentExtract}},
		"extractor without format": {"formats": []string{}},
		"bad format":               {"formats": []string{"pdf"}},
		"interval on an extractor": {"interval": "15m"},
		"formats on a collector":   {"provides": []string{CapabilitySourceCollect}},
	}
	for name, overrides := range cases {
		t.Run(name, func(t *testing.T) {
			manifest := v1(overrides)
			for key, value := range overrides {
				if value == nil {
					delete(manifest, key)
				}
			}
			root := t.TempDir()
			newPlugin(t, root, "strict", manifest)

			if found := discoverOne(t, root); found.Err == nil {
				t.Fatalf("expected a refusal for %s", name)
			}
		})
	}
}

func TestOnlyCollectorsRunAndExtractorsAreListedApart(t *testing.T) {
	root := t.TempDir()
	newPlugin(t, root, "news", map[string]any{"enabled": true, "interval": "1h"})
	newPlugin(t, root, "reader", v1(map[string]any{"enabled": true}))
	runner := newFakeRunner(func(context.Context, Plugin) ([]byte, error) { return itemsOutput("n", 1), nil })

	service := startService(t, root, runner)
	waitUntil(t, 2*time.Second, func() bool { return runner.called("news") == 1 })
	status, err := service.Dispatch(context.Background(), "feeds.status", nil)
	if err != nil {
		t.Fatal(err)
	}

	if runner.called("reader") != 0 {
		t.Fatal("an extractor is not a scheduled source and must not run on its own")
	}
	plugins := status["plugins"].([]map[string]any)
	extractors := status["extractors"].([]map[string]any)
	if len(plugins) != 1 || plugins[0]["id"] != "news" || len(extractors) != 1 || extractors[0]["id"] != "reader" ||
		extractors[0]["plugin_api"] != 1 {
		t.Fatalf("sources and extractors must be listed apart: %#v / %#v", plugins, extractors)
	}
	if status["disabled"] != 0 || status["pending"] != 0 {
		t.Fatalf("an extractor must not change the digest's source counts: %#v", status)
	}
}

func TestAVersionedPluginOutputLimitIsEnforced(t *testing.T) {
	plugin := scriptPlugin(t, "printf '%0100d' 0", 5*time.Second)
	plugin.OutputBytes = 10

	if _, err := (ExecRunner{}).Run(context.Background(), plugin, []string{testPath}); !errors.Is(err, ErrOutputTooLarge) {
		t.Fatalf("expected the plugin's own output limit to apply, got %v", err)
	}
}

func extractor(id string, enabled bool, formats ...string) Found {
	return Found{Dir: id, Plugin: &Plugin{ID: id, Enabled: enabled, APIVersion: 1,
		Provides: []string{CapabilityDocumentExtract}, Formats: formats}}
}

func TestSelectionIsDeterministic(t *testing.T) {
	pdf := Binding{Capability: CapabilityDocumentExtract, Key: ".pdf"}
	cases := []struct {
		name     string
		found    []Found
		binding  Binding
		primary  string
		fallback []string
		reason   string
	}{
		{"a single provider is chosen", []Found{extractor("a", true, ".pdf"), extractor("b", true, ".epub")}, pdf, "a", nil, ""},
		{"no provider is unavailable", []Found{extractor("a", false, ".pdf")}, pdf, "", nil, "unavailable:"},
		{"two providers are ambiguous", []Found{extractor("b", true, ".pdf"), extractor("a", true, ".pdf")}, pdf, "", nil, "ambiguous: document.extract.v1 for \".pdf\" is provided by a, b"},
		{"an explicit binding wins", []Found{extractor("a", true, ".pdf"), extractor("b", true, ".pdf")},
			Binding{Capability: CapabilityDocumentExtract, Key: ".pdf", Plugin: "b"}, "b", nil, ""},
		{"an explicit binding to a disabled plugin is unavailable", []Found{extractor("a", true, ".pdf"), extractor("b", false, ".pdf")},
			Binding{Capability: CapabilityDocumentExtract, Key: ".pdf", Plugin: "b"}, "", nil, "unavailable:"},
		{"an ordered fallback is kept", []Found{extractor("a", true, ".pdf"), extractor("b", true, ".pdf")},
			Binding{Capability: CapabilityDocumentExtract, Key: ".pdf", Plugin: "a", Fallback: []string{"b"}}, "a", []string{"b"}, ""},
		{"an ineligible fallback fails closed", []Found{extractor("a", true, ".pdf"), extractor("b", true, ".epub")},
			Binding{Capability: CapabilityDocumentExtract, Key: ".pdf", Plugin: "a", Fallback: []string{"b"}}, "", nil, "unavailable:"},
		{"the primary cannot be its own fallback", []Found{extractor("a", true, ".pdf")},
			Binding{Capability: CapabilityDocumentExtract, Key: ".pdf", Plugin: "a", Fallback: []string{"a"}}, "", nil, "unavailable:"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			selection := Select(tc.found, tc.binding)
			primary := ""
			if selection.Primary != nil {
				primary = selection.Primary.ID
			}
			var fallback []string
			for _, plugin := range selection.Fallback {
				fallback = append(fallback, plugin.ID)
			}
			if primary != tc.primary || strings.Join(fallback, ",") != strings.Join(tc.fallback, ",") ||
				!strings.HasPrefix(selection.Reason, tc.reason) || (tc.reason == "" && selection.Reason != "") {
				t.Fatalf("expected %q %v %q, got %q %v %q", tc.primary, tc.fallback, tc.reason, primary, fallback, selection.Reason)
			}
		})
	}
}
