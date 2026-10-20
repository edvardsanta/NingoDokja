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
	for _, value := range object {
		if bytes.Equal(value, []byte("null")) {
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
