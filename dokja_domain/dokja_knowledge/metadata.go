package knowledge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// MetadataInput contains user assertions only. Provenance is assigned by Knowledge.
type MetadataInput struct {
	Form        Form        `json:"form,omitempty"`
	ContentKind ContentKind `json:"content_kind,omitempty"`
	Topics      []string    `json:"topics,omitempty"`
	Entities    []Entity    `json:"entities,omitempty"`
	DateAnchor  string      `json:"date_anchor,omitempty"`
}

type Entity struct {
	Name string `json:"name"`
	Type string `json:"type,omitempty"`
}

type ReadingProvenance struct {
	Format        string `json:"format"`
	Reader        string `json:"reader"`
	ReaderVersion string `json:"reader_version"`
}

// DefiningMetadata is independent of the legacy kind field and free-form tags.
type DefiningMetadata struct {
	Classification
	Topics     []string           `json:"topics,omitempty"`
	Entities   []Entity           `json:"entities,omitempty"`
	DateAnchor string             `json:"date_anchor,omitempty"`
	Reading    *ReadingProvenance `json:"reading,omitempty"`
}

func decodeMetadata(raw any) (MetadataInput, error) {
	var input MetadataInput
	data, err := json.Marshal(raw)
	if err != nil || len(data) > 65536 || bytes.Equal(data, []byte("null")) {
		return input, fmt.Errorf("metadata must be an object of at most 65536 bytes")
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(data, &object) != nil || object == nil {
		return input, fmt.Errorf("metadata must be an object")
	}
	for key, value := range object {
		if bytes.Equal(value, []byte("null")) && key != "form" && key != "content_kind" {
			return input, fmt.Errorf("metadata fields cannot be null")
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return input, fmt.Errorf("metadata has unsupported fields or field types")
	}
	for _, topic := range input.Topics {
		if strings.TrimSpace(topic) == "" {
			return input, fmt.Errorf("metadata topics must be nonempty")
		}
	}
	for _, entity := range input.Entities {
		if strings.TrimSpace(entity.Name) == "" {
			return input, fmt.Errorf("metadata entities require a name")
		}
	}
	if input.DateAnchor != "" {
		if _, err := time.Parse("2006-01-02", input.DateAnchor); err != nil {
			return input, fmt.Errorf("metadata date_anchor must be a valid YYYY-MM-DD date")
		}
	}
	return input, nil
}

func resolvedMetadata(raw any, defaults ClassificationValues, reading *ReadingProvenance) (map[string]any, error) {
	input := MetadataInput{}
	var err error
	if raw != nil {
		input, err = decodeMetadata(raw)
		if err != nil {
			return nil, err
		}
	}
	classification, err := ResolveClassification(ClassificationInput{
		Explicit:      ClassificationValues{Form: input.Form, ContentKind: input.ContentKind},
		SourceDefault: defaults,
	})
	if err != nil {
		return nil, err
	}
	metadata := DefiningMetadata{Classification: classification, Topics: input.Topics,
		Entities: input.Entities, DateAnchor: input.DateAnchor, Reading: reading}
	data, err := json.Marshal(metadata)
	if err != nil || len(data) > 65536 {
		return nil, fmt.Errorf("resolved metadata exceeds 65536 bytes")
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// Source defaults describe structure only. A PDF says nothing about either axis.
// A feed is not necessarily news: its subject requires an explicit value or classifier.
func readingDefaults(format string) ClassificationValues {
	switch strings.ToLower(strings.TrimPrefix(format, ".")) {
	case "epub", "mobi":
		return ClassificationValues{Form: FormBook}
	case "html", "htm", "rss", "atom":
		return ClassificationValues{Form: FormArticle}
	}
	return ClassificationValues{}
}

// classificationQuestions describes unresolved axes without assigning a type. A
// persisted user_required choice survives rereads; only explicit user values patch it.
func classificationQuestions(metadata map[string]any) map[string]any {
	forms := make([]string, len(Forms))
	for i, form := range Forms {
		forms[i] = string(form)
	}
	missing := []string{}
	questions := []map[string]any{}
	for _, axis := range []struct {
		field, question string
		values          []string
	}{
		{"form", "What is the form of this document?", forms},
		{"content_kind", "What type of content does this document contain?", []string{"news", "reference", "research", "narrative"}},
	} {
		value, _ := metadata[axis.field].(string)
		valid := validForm(value)
		if axis.field == "content_kind" {
			valid = validContentKind(value)
		}
		if valid {
			continue
		}
		missing = append(missing, axis.field)
		questions = append(questions, map[string]any{"field": axis.field, "question": axis.question, "allowed_values": axis.values})
	}
	status := "classified"
	if len(missing) > 0 {
		status = "pending"
	}
	return map[string]any{"status": status, "needs_user_input": len(missing) > 0, "missing_axes": missing, "questions": questions}
}

// annotateClassification uses effective persisted metadata, never a default from
// the latest request, since unchanged rereads may preserve earlier user assertions.
func annotateClassification(result map[string]any) {
	if metadata, ok := result["metadata"].(map[string]any); ok {
		result["classification"] = classificationQuestions(metadata)
	}
	for _, key := range []string{"documents", "hits"} {
		switch entries := result[key].(type) {
		case []any:
			for _, raw := range entries {
				if entry, ok := raw.(map[string]any); ok {
					annotateClassification(entry)
				}
			}
		case []map[string]any:
			for _, entry := range entries {
				annotateClassification(entry)
			}
		}
	}
}
