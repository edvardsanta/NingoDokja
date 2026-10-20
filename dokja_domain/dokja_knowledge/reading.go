package knowledge

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"path"
	"regexp"
	"strings"
)

// ActionReadKnowledge stores a file read by the reading host (ADR 0006). It is not a service
// event: the orchestrator runs the reading and hands the documents to StoreReading.
const ActionReadKnowledge = "read-knowledge"

// maxUserTags leaves room for the two provenance tags under Knowledge's limit of 20.
const maxUserTags = 18

var unsafeIDRunes = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// ReadDocument is Knowledge's Extracted contract as the reading host returns it.
type ReadDocument struct {
	Title     string
	Text      string
	SourceRef string
	Key       string
}

// Reading is one file read on demand and what the request said about storing it.
type Reading struct {
	Filename      string
	Format        string // the extension the reading host chose the reader by, such as ".pdf"
	Reader        string // the plugin that read it
	ReaderVersion string
	Documents     []ReadDocument
	Payload       map[string]any // the knowledge.read request, without the file
}

// ValidateRead checks a knowledge.read request: a file and its name, and nothing that
// would make it another kind of ingest.
func ValidateRead(payload map[string]any) error {
	for _, field := range []string{"filename", "content_b64"} {
		if value, _ := payload[field].(string); strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required", field)
		}
	}
	for _, field := range []string{"body", "source"} {
		if value, _ := payload[field].(string); strings.TrimSpace(value) != "" {
			return fmt.Errorf("knowledge.read takes a file; send %s with knowledge.ingest", field)
		}
	}
	if tags := readingTags(payload["tags"], Reading{}); len(tags) > maxUserTags {
		return fmt.Errorf("at most %d tags; two are kept for the reading's provenance", maxUserTags)
	}
	return nil
}

// StoreReading ingests every document of a reading. Knowledge keeps the provenance: the
// source in source_id (file:<name>, as an uploaded file gets) and source_ref, and the format
// and reader as tags, so the same file read again updates its documents instead of adding
// copies. It stops at the first document Knowledge refuses and returns what was stored.
func (d *Domain) StoreReading(ctx context.Context, reading Reading) ([]map[string]any, error) {
	if d == nil || d.service == nil {
		return nil, fmt.Errorf("knowledge service is not configured")
	}
	if len(reading.Documents) == 0 {
		return nil, fmt.Errorf("the reading has no documents")
	}
	request := reading.Payload
	base, _ := request["source_id"].(string)
	base = strings.TrimSpace(base)
	if base == "" {
		base = "file:" + safeID(path.Base(reading.Filename))
	}
	tags := readingTags(request["tags"], reading)

	stored := make([]map[string]any, 0, len(reading.Documents))
	for _, document := range reading.Documents {
		payload := map[string]any{
			"title": documentTitle(request, reading, document),
			"body":  document.Text,
			"tags":  tags,
		}
		payload["source_id"] = base
		if len(reading.Documents) > 1 {
			key := document.Key
			if key == "" {
				key = document.Title
			}
			sum := sha1.Sum([]byte(key))
			payload["source_id"] = base + "#" + hex.EncodeToString(sum[:])[:10]
		}
		if kind, _ := request["kind"].(string); strings.TrimSpace(kind) != "" {
			payload["kind"] = kind
		}
		if ref, _ := request["source_ref"].(string); strings.TrimSpace(ref) != "" {
			payload["source_ref"] = ref
		} else if document.SourceRef != "" {
			payload["source_ref"] = document.SourceRef
		}
		result, err := d.service.Dispatch(ctx, "knowledge.ingest", payload)
		if err != nil {
			return stored, err
		}
		stored = append(stored, result)
	}
	return stored, nil
}

// documentTitle follows the upload path: the request's title names a single document;
// otherwise each document keeps its own, and the file name is the last resort.
func documentTitle(request map[string]any, reading Reading, document ReadDocument) string {
	if title, _ := request["title"].(string); strings.TrimSpace(title) != "" && len(reading.Documents) == 1 {
		return strings.TrimSpace(title)
	}
	if strings.TrimSpace(document.Title) != "" {
		return document.Title
	}
	return path.Base(reading.Filename)
}

func readingTags(raw any, reading Reading) []string {
	var tags []string
	switch value := raw.(type) {
	case string:
		tags = strings.Split(value, ",")
	case []any:
		for _, tag := range value {
			tags = append(tags, fmt.Sprint(tag))
		}
	case []string:
		tags = append(tags, value...)
	}
	kept := make([]string, 0, len(tags)+2)
	for _, tag := range tags {
		if tag = strings.TrimSpace(tag); tag != "" {
			kept = append(kept, tag)
		}
	}
	if format := strings.TrimPrefix(reading.Format, "."); format != "" {
		kept = append(kept, "format:"+format)
	}
	if reading.Reader != "" {
		kept = append(kept, "reader:"+reading.Reader)
	}
	return kept
}

// safeID is how Knowledge names an uploaded file in a source_id.
func safeID(name string) string {
	id := strings.Trim(unsafeIDRunes.ReplaceAllString(name, "-"), "-")
	if len(id) > 120 {
		id = id[:120]
	}
	if id == "" {
		return "item"
	}
	return id
}
