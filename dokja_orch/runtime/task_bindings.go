package runtime

import (
	"encoding/json"
	"errors"
	"net/http"
	domain "read_books/dokja_domain/dokja_runtime"
)

func (h inferenceHTTPHandler) taskBindings(w http.ResponseWriter, r *http.Request) {
	bindings, err := h.domain.TaskBindings(r.PathValue("profile"))
	if err != nil {
		writeInferenceError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"bindings": bindings})
}

func (h inferenceHTTPHandler) taskChat(w http.ResponseWriter, r *http.Request) {
	messages, binding, ok := decodeChatRequest(w, r)
	if !ok {
		return
	}
	result, err := h.domain.ChatTask(r.Context(), r.PathValue("profile"), binding, messages)
	if err != nil {
		failure := map[string]any{"error": "task inference failed", "code": "provider_error"}
		var providerFailure *domain.ChatProviderFailure
		var cooldown *domain.TaskCooldownError
		if errors.As(err, &providerFailure) {
			failure["error"], failure["code"] = providerFailure.Error(), string(providerFailure.SafeCode())
		} else if errors.As(err, &cooldown) {
			failure["code"], failure["retry_after_ms"] = "cooldown", cooldown.RetryAfterMS
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(failure)
		return
	}
	json.NewEncoder(w).Encode(result)
}
