package runtime

import (
	"encoding/json"
	"net/http"
)

func (h inferenceHTTPHandler) taskBindings(w http.ResponseWriter, r *http.Request) {
	bindings, err := h.domain.TaskBindings(r.PathValue("profile"))
	if err != nil {
		writeInferenceError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"bindings": bindings})
}
