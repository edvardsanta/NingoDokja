package knowledge

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// ingestService records every knowledge.ingest payload and can refuse the nth one.
type ingestService struct {
	payloads []map[string]any
	failAt   int
}

func (s *ingestService) Dispatch(_ context.Context, eventType string, payload map[string]any) (map[string]any, error) {
	if eventType != "knowledge.ingest" {
		return nil, errors.New("unexpected " + eventType)
	}
	s.payloads = append(s.payloads, payload)
	if s.failAt == len(s.payloads) {
		return nil, errors.New("body is larger than 2000000 characters")
	}
	return map[string]any{"source_id": payload["source_id"], "changed": true}, nil
}

func reading(payload map[string]any, documents ...ReadDocument) Reading {
	return Reading{Filename: "Deep Work.pdf", Format: ".pdf", Reader: "knowledge-extractors", ReaderVersion: "0.1.0",
		Documents: documents, Payload: payload}
}

func TestValidateReadNeedsAFileAndNothingElse(t *testing.T) {
	cases := map[string]struct {
		payload map[string]any
		want    string
	}{
		"no file":       {map[string]any{"filename": "a.pdf"}, "content_b64 is required"},
		"no name":       {map[string]any{"content_b64": "eA=="}, "filename is required"},
		"with body":     {map[string]any{"filename": "a.pdf", "content_b64": "eA==", "body": "x"}, "knowledge.ingest"},
		"with source":   {map[string]any{"filename": "a.pdf", "content_b64": "eA==", "source": "https://docs.example/a"}, "knowledge.ingest"},
		"too many tags": {map[string]any{"filename": "a.pdf", "content_b64": "eA==", "tags": strings.Repeat("t,", 19) + "t"}, "at most 18"},
	}
	for name, tc := range cases {
		if err := ValidateRead(tc.payload); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: got %v, want %q", name, err, tc.want)
		}
	}
	if err := ValidateRead(map[string]any{"filename": "a.pdf", "content_b64": "eA==", "tags": []any{"study"}}); err != nil {
		t.Fatal(err)
	}
}

func TestStoreReadingKeepsSourceFormatAndReaderProvenance(t *testing.T) {
	service := &ingestService{}
	stored, err := New(service).StoreReading(context.Background(), reading(
		map[string]any{"kind": "book", "tags": []any{"study"}},
		ReadDocument{Title: "Deep Work", Text: "Focus is a skill.", SourceRef: "page-1"},
	))
	if err != nil || len(stored) != 1 {
		t.Fatalf("stored=%v err=%v", stored, err)
	}
	want := map[string]any{
		"title": "Deep Work", "body": "Focus is a skill.", "kind": "book", "source_id": "file:Deep-Work.pdf",
		"source_ref": "page-1", "tags": []string{"study", "format:pdf", "reader:knowledge-extractors"},
	}
	if service.payloads[0]["metadata"] == nil || service.payloads[0]["metadata_if_unchanged"] == nil {
		t.Fatal("missing defining metadata")
	}
	delete(service.payloads[0], "metadata")
	delete(service.payloads[0], "metadata_if_unchanged")
	if !reflect.DeepEqual(service.payloads[0], want) {
		t.Fatalf("payload=%#v\nwant   %#v", service.payloads[0], want)
	}
}

func TestStoreReadingGivesEachDocumentOfAFileAStableID(t *testing.T) {
	service := &ingestService{}
	documents := []ReadDocument{{Title: "One", Text: "a", Key: "c1"}, {Title: "Two", Text: "b"}}
	payload := map[string]any{"title": "Ignored for many", "source_id": "book:deep-work", "source_ref": "shelf"}
	if _, err := New(service).StoreReading(context.Background(), reading(payload, documents...)); err != nil {
		t.Fatal(err)
	}
	again := &ingestService{}
	if _, err := New(again).StoreReading(context.Background(), reading(payload, documents...)); err != nil {
		t.Fatal(err)
	}
	first, second := service.payloads[0]["source_id"].(string), service.payloads[1]["source_id"].(string)
	if !strings.HasPrefix(first, "book:deep-work#") || first == second || again.payloads[0]["source_id"] != first {
		t.Fatalf("ids must be per document and stable across readings: %s %s %v", first, second, again.payloads[0]["source_id"])
	}
	if service.payloads[0]["title"] != "One" || service.payloads[1]["source_ref"] != "shelf" {
		t.Fatalf("a multi-document file keeps each title; the request's ref wins: %#v", service.payloads)
	}
}

func TestStoreReadingStopsAtTheFirstRefusalAndReportsWhatWasStored(t *testing.T) {
	service := &ingestService{failAt: 2}
	stored, err := New(service).StoreReading(context.Background(), reading(nil,
		ReadDocument{Title: "One", Text: "a"}, ReadDocument{Title: "Two", Text: "b"}, ReadDocument{Title: "Three", Text: "c"}))
	if err == nil || len(stored) != 1 || len(service.payloads) != 2 {
		t.Fatalf("stored=%v calls=%d err=%v", stored, len(service.payloads), err)
	}
}

func TestStoreReadingFallsBackToTheFileName(t *testing.T) {
	service := &ingestService{}
	if _, err := New(service).StoreReading(context.Background(), reading(nil, ReadDocument{Text: "a"})); err != nil {
		t.Fatal(err)
	}
	if service.payloads[0]["title"] != "Deep Work.pdf" {
		t.Fatalf("title=%v", service.payloads[0]["title"])
	}
}

func TestHandleDoesNotServeReadingByItself(t *testing.T) {
	service := &recordingService{}
	_, err := New(service).Handle(context.Background(), Request{Action: ActionReadKnowledge,
		Event: Event{Payload: map[string]any{"filename": "a.pdf", "content_b64": "eA=="}}})
	if err == nil || service.calls != 0 {
		t.Fatalf("knowledge.read must not reach the service as an event: %v", err)
	}
}
