package clients

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestTaskTextUsesValidatedFallbackAndPreservesCallerMessages(t *testing.T) {
	messages := []map[string]string{{"role": "system", "content": "Configured persona"}, {"role": "user", "content": "Private question"}}
	client := inferenceSocket(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			json.NewEncoder(w).Encode(map[string]any{"bindings": []map[string]string{{"provider": "kimi", "model": "registered"}}})
			return
		}
		var body struct {
			Messages []map[string]string `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if len(body.Messages) != 3 || body.Messages[0]["content"] != "Configured persona" {
			t.Error("persona lost")
		}
		json.NewEncoder(w).Encode(map[string]any{"reply": `{"text":"Short reply"}`, "provider": "kimi"})
	})
	reply, err := client.GenerateTaskText(t.Context(), "lightweight", messages, nil)
	if err != nil || reply["reply"] != "Short reply" || reply["attempts"] == nil || len(messages) != 2 {
		t.Fatalf("%v %v", reply, err)
	}
}
