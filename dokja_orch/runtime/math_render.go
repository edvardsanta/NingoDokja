package runtime

import (
	"encoding/json"
	"io"
	"net/http"

	service "read_books/dokja_services/dokja_runtime"
)

const maxMathRequestBytes = 8192

type mathRenderRequest struct {
	Expression string `json:"expression"`
}

type mathRenderResponse struct {
	Data        []byte `json:"data_b64"`
	ContentType string `json:"content_type"`
}

func renderMath(w http.ResponseWriter, r *http.Request) {
	var request mathRenderRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxMathRequestBytes))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		writeInferenceError(w, http.StatusBadRequest, "invalid formula request")
		return
	}

	data, err := service.RenderMath(r.Context(), request.Expression)
	if err != nil {
		writeInferenceError(w, http.StatusUnprocessableEntity, "formula could not be rendered")
		return
	}
	json.NewEncoder(w).Encode(mathRenderResponse{Data: data, ContentType: "image/png"})
}
