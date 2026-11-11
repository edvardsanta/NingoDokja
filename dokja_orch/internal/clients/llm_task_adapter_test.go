package clients

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"testing"
	"time"
)

func TestTaskAdapterFallsBackToKimiAfterTransportAndContractFailures(t *testing.T) {
	seen := []string{}
	client := inferenceSocket(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/chat/tasks/lightweight" {
			json.NewEncoder(w).Encode(map[string]any{"bindings": []map[string]string{
				{"provider": "codex", "model": "luna"}, {"provider": "claude_code", "model": "haiku"},
				{"provider": "kimi", "model": "registered-kimi"}, {"provider": "ollama", "model": "small-qwen"},
			}})
			return
		}
		if r.URL.Path != "/chat" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		provider, _ := body["provider"].(string)
		seen = append(seen, provider)
		if body["format"] == nil {
			t.Error("schema was dropped")
		}
		switch provider {
		case "codex":
			http.Error(w, `{"error":"unavailable"}`, 503)
		case "claude_code":
			json.NewEncoder(w).Encode(map[string]any{"reply": `{"wrong":true}`})
		case "kimi":
			if body["model"] != "registered-kimi" {
				t.Errorf("unregistered model %v", body["model"])
			}
			json.NewEncoder(w).Encode(map[string]any{"reply": `{"queries":["direct peer"]}`, "provider": "kimi"})
		default:
			t.Error("fallback continued after valid Kimi reply")
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	reply, err := client.GenerateTaskJSON(ctx, "lightweight", map[string]any{"type": "object"}, []map[string]string{{"role": "user", "content": "task"}}, func(reply map[string]any) error {
		var parsed map[string]any
		json.Unmarshal([]byte(reply["reply"].(string)), &parsed)
		if parsed["queries"] == nil {
			return fmt.Errorf("missing queries")
		}
		return nil
	})
	if err != nil || reply["provider"] != "kimi" || !reflect.DeepEqual(seen, []string{"codex", "claude_code", "kimi"}) {
		t.Fatalf("reply %v error %v seen %v", reply, err, seen)
	}
}

func TestTaskAdapterPreservesLegacyModelOverrides(t *testing.T) {
	t.Setenv("DOKJA_PRICE_CODEX_MODEL", " custom-model ")
	if got := legacyTaskModel("codex", "registered"); got != "custom-model" {
		t.Fatal(got)
	}
	if got := legacyTaskModel("kimi", "registered-kimi"); got != "registered-kimi" {
		t.Fatal(got)
	}
}

func TestTaskAdapterReservesTimeForTheNextProvider(t *testing.T) {
	client := inferenceSocket(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/chat/tasks/lightweight" {
			json.NewEncoder(w).Encode(map[string]any{"bindings": []map[string]string{
				{"provider": "codex", "model": "slow"}, {"provider": "kimi", "model": "registered"},
			}})
			return
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["provider"] == "codex" {
			<-r.Context().Done()
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"reply": `{"ok":true}`, "provider": "kimi"})
	})
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	reply, err := client.GenerateTaskJSON(ctx, "lightweight", map[string]any{"type": "object"}, nil, nil)
	if err != nil || reply["provider"] != "kimi" {
		t.Fatalf("fallback was starved: %v %v", reply, err)
	}
}
