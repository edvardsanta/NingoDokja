package clients

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestTaskAdapterFallsBackToKimiAfterTransportAndContractFailures(t *testing.T) {
	seen := []string{}
	client := inferenceSocket(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/chat/task-attempts" {
			w.Write([]byte(`{"recorded":true}`))
			return
		}
		if r.Method == "GET" && r.URL.Path == "/chat/tasks/lightweight" {
			json.NewEncoder(w).Encode(map[string]any{"bindings": []map[string]string{
				{"provider": "codex", "model": "luna"}, {"provider": "claude_code", "model": "haiku"},
				{"provider": "kimi", "model": "registered-kimi"}, {"provider": "ollama", "model": "small-qwen"},
			}})
			return
		}
		if r.URL.Path != "/chat/tasks/lightweight" {
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
			http.Error(w, `{"error":"private-prompt-marker"}`, 503)
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
	attempts := reply["attempts"].([]TaskAttempt)
	if len(attempts) != 3 || attempts[1].Reason != "contract_rejected" || attempts[2].Outcome != "success" || attempts[0].TaskID != attempts[2].TaskID {
		t.Fatalf("missing attempt diagnostics: %v", attempts)
	}
	raw, _ := json.Marshal(attempts)
	if strings.Contains(string(raw), "private-prompt-marker") || strings.Contains(string(raw), "direct peer") {
		t.Fatal("private content leaked into diagnostics")
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
		if r.URL.Path == "/chat/task-attempts" {
			w.Write([]byte(`{"recorded":true}`))
			return
		}
		if r.Method == "GET" && r.URL.Path == "/chat/tasks/lightweight" {
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

func TestTaskAdapterSkipsCoolingProvidersWithoutGenerating(t *testing.T) {
	calls := 0
	client := inferenceSocket(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/chat/task-attempts" {
			w.Write([]byte(`{"recorded":true}`))
			return
		}
		if r.Method == "GET" {
			json.NewEncoder(w).Encode(map[string]any{"bindings": []map[string]any{
				{"provider": "codex", "model": "slow", "retry_after_ms": 60000},
				{"provider": "kimi", "model": "registered"},
			}})
			return
		}
		calls++
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["provider"] != "kimi" {
			t.Error("cooling provider was invoked")
		}
		json.NewEncoder(w).Encode(map[string]any{"reply": `{"ok":true}`, "provider": "kimi"})
	})
	reply, err := client.GenerateTaskJSON(t.Context(), "lightweight", map[string]any{"type": "object"}, nil, nil)
	if err != nil || calls != 1 {
		t.Fatalf("reply %v error %v calls %d", reply, err, calls)
	}
	attempts := reply["attempts"].([]TaskAttempt)
	if attempts[0].Outcome != "skipped" || attempts[0].Reason != "cooldown" || attempts[0].RetryAfterMS != 60000 {
		t.Fatal(attempts)
	}
}

func TestFailedTaskDiagnosticsDoNotKeepPrivateProviderOrValidatorMessages(t *testing.T) {
	client := inferenceSocket(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/chat/task-attempts" {
			w.Write([]byte(`{"recorded":true}`))
			return
		}
		if r.Method == "GET" {
			json.NewEncoder(w).Encode(map[string]any{"bindings": []map[string]string{{"provider": "kimi", "model": "registered"}}})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"reply": `{"private":"private-prompt-marker"}`})
	})
	_, err := client.GenerateTaskJSON(t.Context(), "lightweight", map[string]any{"type": "object"}, []map[string]string{{"role": "user", "content": "private-prompt-marker"}}, func(map[string]any) error { return fmt.Errorf("private-prompt-marker validation detail") })
	var failure *TaskInferenceError
	if !errors.As(err, &failure) || len(failure.Attempts) != 1 || failure.Attempts[0].Reason != "contract_rejected" {
		t.Fatalf("diagnostics %v", err)
	}
	raw, _ := json.Marshal(failure)
	if strings.Contains(string(raw), "private-prompt-marker") || strings.Contains(err.Error(), "private-prompt-marker") {
		t.Fatal("private details retained")
	}
}

func TestConfiguredTaskModelWinsOverLegacyPriceOverride(t *testing.T) {
	t.Setenv("DOKJA_PRICE_CODEX_MODEL", "legacy")
	reports := make(chan TaskAttempt, 1)
	client := inferenceSocket(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			json.NewEncoder(w).Encode(map[string]any{"bindings": []any{map[string]any{"provider": "codex", "model": "chosen", "configured": true}}})
			return
		}
		if r.URL.Path == "/chat/task-attempts" {
			var a TaskAttempt
			json.NewDecoder(r.Body).Decode(&a)
			reports <- a
			w.Write([]byte(`{"recorded":true}`))
			return
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != "chosen" {
			t.Errorf("explicit priority overridden: %v", body["model"])
		}
		json.NewEncoder(w).Encode(map[string]any{"reply": `{"ok":true}`})
	})
	if _, err := client.GenerateTaskJSON(t.Context(), "lightweight", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case a := <-reports:
		if a.Model != "chosen" || a.Outcome != "success" {
			t.Fatal(a)
		}
	default:
		t.Fatal("attempt metadata was not reported")
	}
}
