package runtime

import (
	"bytes"
	"encoding/json"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMathRenderRouteReturnsPNG(t *testing.T) {
	body, _ := json.Marshal(mathRenderRequest{Expression: `\frac{x}{2}`})
	w := httptest.NewRecorder()
	ChatHandler(nil).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/math/render", bytes.NewReader(body)))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var result mathRenderResponse
	if json.Unmarshal(w.Body.Bytes(), &result) != nil || result.ContentType != "image/png" {
		t.Fatal("invalid image response")
	}
	if _, err := png.DecodeConfig(bytes.NewReader(result.Data)); err != nil {
		t.Fatal(err)
	}
}

func TestMathRenderRouteRejectsInvalidRequests(t *testing.T) {
	for _, body := range []string{`{}`, `{"expression":"x","command":"anything"}`, `{"expression":"x"}{}`, `{"expression":"\\input{x}"}`} {
		w := httptest.NewRecorder()
		ChatHandler(nil).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/math/render", strings.NewReader(body)))
		if w.Code < 400 {
			t.Fatal("invalid render request succeeded")
		}
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/math/render", strings.NewReader(`{"expression":"x"}`))
	r.Header.Set("Origin", "http://localhost")
	ChatHandler(nil).ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatal("browser origin reached rendering")
	}
}
