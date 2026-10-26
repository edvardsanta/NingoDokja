package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	domain "read_books/dokja_domain/dokja_runtime"
	"strings"
	"testing"
)

type httpMeasuredInference struct {
	streamInference
	calls int
}

func (*httpMeasuredInference) SupportsTokenUsage(string) bool { return true }
func (f *httpMeasuredInference) GenerateMeasured(context.Context, domain.ChatBinding, []domain.ChatMessage) (domain.MeasuredInferenceResult, error) {
	f.calls++
	input, output := int64(15), int64(3)
	return domain.MeasuredInferenceResult{Reply: "private answer", Usage: domain.TokenUsage{InputTokens: &input, OutputTokens: &output}}, nil
}
func TestMeasuredHTTPRetainsRejectedUsageWithoutReply(t *testing.T) {
	f := &httpMeasuredInference{}
	d := domain.New(nil, domain.Journal{}, nil).WithChat(&streamStore{b: domain.ChatBinding{Revision: 2, Provider: "ollama", Model: "fixture"}}, f).WithProviderSettings(streamProviders{})
	h := ChatHandler(d)
	for _, limit := range []string{"10", "20"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", "/chat/measured", strings.NewReader(`{"messages":[{"role":"user","content":"q"}],"budget":{"max_input_tokens":`+limit+`,"max_output_tokens":5}}`)))
		var result map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if w.Code != http.StatusOK || result["budget_mode"] != "post_generation" || result["usage"].(map[string]any)["input_tokens"] != float64(15) {
			t.Fatal(w.Code, result)
		}
		if limit == "10" && (result["status"] != "over_budget" || result["reply"] != nil) {
			t.Fatal("rejected text published", result)
		}
		if limit == "20" && (result["status"] != "ok" || result["reply"] != "private answer") {
			t.Fatal(result)
		}
	}
	for _, body := range []string{`{"messages":[],"unknown":true}`, `{} {}`, `{"budget":{"max_input_tokens":1.5}}`} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", "/chat/measured", strings.NewReader(body)))
		if w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if f.calls != 2 {
		t.Fatal("invalid input reached inference", f.calls)
	}
}

func TestOrdinaryChatStillIgnoresUnknownBudgetFields(t *testing.T) {
	w := httptest.NewRecorder()
	streamHandler(streamInference{reply: "answer"}).ServeHTTP(w, httptest.NewRequest("POST", "/chat", strings.NewReader(`{"messages":[{"role":"user","content":"q"}],"budget":"ignored legacy extension"}`)))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "answer") {
		t.Fatal(w.Code, w.Body.String())
	}
}
