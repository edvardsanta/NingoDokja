package knowledge

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

const ActionClassifyKnowledge = "classify-knowledge"

// classify gets questions or applies explicit user answers to a stored document.
// It does not classify text or retry providers. Storage applies a version-checked
// metadata patch atomically; content and embeddings never travel in this request.
func (d *Domain) classify(ctx context.Context, payload map[string]any) (map[string]any, error) {
	sourceID, _ := payload["source_id"].(string)
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return nil, fmt.Errorf("source_id is required")
	}
	for key := range payload {
		switch key {
		case "source_id", "answers", "revision":
		default:
			return nil, fmt.Errorf("unsupported classification request field")
		}
	}
	request := map[string]any{"source_id": sourceID}
	eventType := "knowledge.metadata.get"
	if raw, exists := payload["answers"]; exists {
		patch, err := userAnswerPatch(raw)
		if err != nil {
			return nil, err
		}
		revision, _ := payload["revision"].(string)
		if decoded, err := hex.DecodeString(revision); err != nil || len(decoded) != 32 {
			return nil, fmt.Errorf("revision from the classification questions is required")
		}
		request["revision"] = revision
		request["patch"] = patch
		eventType = "knowledge.metadata.patch"
	} else if _, exists := payload["revision"]; exists {
		return nil, fmt.Errorf("revision requires answers")
	}
	result, err := d.service.Dispatch(ctx, eventType, request)
	if err == nil {
		annotateClassification(result)
	}
	return result, err
}

func userAnswerPatch(raw any) (map[string]any, error) {
	data, err := json.Marshal(raw)
	if err != nil || len(data) > 4096 {
		return nil, fmt.Errorf("answers must be an object containing form or content_kind")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil || len(fields) == 0 {
		return nil, fmt.Errorf("answers must contain form or content_kind")
	}
	var values struct {
		Form        Form        `json:"form"`
		ContentKind ContentKind `json:"content_kind"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&values); err != nil {
		return nil, fmt.Errorf("answers only accept form and content_kind strings")
	}
	if _, ok := fields["form"]; ok && normalizeLabel(string(values.Form)) == "" {
		return nil, fmt.Errorf("form answer must be nonempty")
	}
	if _, ok := fields["content_kind"]; ok && normalizeLabel(string(values.ContentKind)) == "" {
		return nil, fmt.Errorf("content_kind answer must be nonempty")
	}
	resolved, err := ResolveClassification(ClassificationInput{Explicit: ClassificationValues{Form: values.Form, ContentKind: values.ContentKind}})
	if err != nil {
		return nil, err
	}
	patch := map[string]any{}
	if _, ok := fields["form"]; ok {
		patch["form"] = string(resolved.Form)
		patch["form_choice"] = map[string]any{"origin": string(OriginUser)}
	}
	if _, ok := fields["content_kind"]; ok {
		patch["content_kind"] = string(resolved.ContentKind)
		patch["kind_choice"] = map[string]any{"origin": string(OriginUser)}
	}
	return patch, nil
}
