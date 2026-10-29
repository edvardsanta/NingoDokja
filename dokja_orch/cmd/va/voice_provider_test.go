package main

import "testing"

func TestVoiceProviderRequiresExplicitLocalSelection(t *testing.T) {
	for _, provider := range []string{"", "runtime", "ollama", "unknown"} {
		generator, err := voiceGeneratorForProvider(provider, "")
		if (err != nil) != (provider == "unknown") {
			t.Fatalf("provider=%q err=%v", provider, err)
		}
		if (generator != nil) != (provider == "ollama") {
			t.Fatalf("unexpected local generator for %q", provider)
		}
	}
}
