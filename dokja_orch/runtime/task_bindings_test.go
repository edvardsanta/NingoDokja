package runtime

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestInferenceTasksExposeRegisteredBindings(t *testing.T) {
	handler := streamHandler(streamInference{reply: "should not be requested"})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/chat/tasks/lightweight", nil))
	var body struct {
		Bindings []struct{ Provider, Model string } `json:"bindings"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != 200 || len(body.Bindings) != 1 || body.Bindings[0].Provider != "ollama" || body.Bindings[0].Model != "fixture" {
		t.Fatalf("response %d %s, error %v", w.Code, w.Body.String(), err)
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/chat/tasks/unknown", nil))
	if w.Code == 200 {
		t.Fatal("unknown task profile accepted")
	}
}
