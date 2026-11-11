package runtime

import "testing"

func TestLightweightTasksUseRegisteredModelsAndIncludeKimi(t *testing.T) {
	d, _ := fresh()
	store := preferencesFixture(t)
	store.preferences.Providers = []ProviderRegistration{
		{ID: "codex", Enabled: true, Models: []string{"large-model", "gpt-6-luna"}},
		{ID: "claude_code", Enabled: true, Models: []string{"sonnet", "haiku"}},
		{ID: "kimi", Enabled: true, Models: []string{"owner-kimi-model"}},
		{ID: "ollama", Enabled: true, Models: []string{"qwen3:32b", "qwen3.5:2b", "qwen2.5:0.5b"}},
	}
	d.WithProviderSettings(store)
	bindings, err := d.TaskBindings("lightweight")
	if err != nil || len(bindings) != 4 {
		t.Fatalf("%v %v", bindings, err)
	}
	for i, want := range []string{"gpt-6-luna", "haiku", "owner-kimi-model", "qwen2.5:0.5b"} {
		if bindings[i].Model != want {
			t.Fatalf("bindings %v", bindings)
		}
	}
	analysis, err := d.TaskBindings("analysis")
	if err != nil || analysis[0].Model != "large-model" || analysis[3].Model != "qwen3:32b" {
		t.Fatal(analysis, err)
	}
	store.preferences.Providers[2].Enabled = false
	bindings, err = d.TaskBindings("lightweight")
	if err != nil || len(bindings) != 3 {
		t.Fatalf("disabled provider included: %v %v", bindings, err)
	}
	if _, err := d.TaskBindings("unknown"); err == nil {
		t.Fatal("unknown task accepted")
	}
	if store.writes != 0 {
		t.Fatal("task selection changed provider preferences")
	}
}
