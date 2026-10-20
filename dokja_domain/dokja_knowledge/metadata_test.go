package knowledge

import (
	"context"
	"reflect"
	"testing"
)

func TestReadingClassificationKeepsAxesAndUserAssertions(t *testing.T) {
	service := &ingestService{}
	request := map[string]any{"kind": "meme_hashtag", "metadata": map[string]any{
		"form": "book", "content_kind": "research", "topics": []string{"physics"},
		"entities": []map[string]any{{"name": "Example Lab", "type": "organization"}}, "date_anchor": "2026-10-19",
	}}
	if _, err := New(service).StoreReading(context.Background(), reading(request, ReadDocument{Title: "Study", Text: "Evidence"})); err != nil {
		t.Fatal(err)
	}
	payload := service.payloads[0]
	meta := payload["metadata"].(map[string]any)
	if meta["form"] != "book" || meta["content_kind"] != "research" || payload["kind"] != "meme_hashtag" {
		t.Fatalf("payload=%#v", payload)
	}
	for _, axis := range []string{"form_choice", "kind_choice"} {
		if meta[axis].(map[string]any)["origin"] != "user" {
			t.Fatalf("metadata=%#v", meta)
		}
	}
	provenance := meta["reading"].(map[string]any)
	if provenance["format"] != "pdf" || provenance["reader_version"] != "0.1.0" {
		t.Fatalf("reading=%#v", provenance)
	}
	if !reflect.DeepEqual(request["metadata"].(map[string]any)["topics"], []string{"physics"}) {
		t.Fatal("request was mutated")
	}
}

func TestReadingDefaultsDoNotInferSubjectFromFormat(t *testing.T) {
	for _, tc := range []struct{ format, form, origin string }{
		{".pdf", "general", "general"}, {".epub", "book", "source_default"}, {".rss", "article", "source_default"},
	} {
		service := &ingestService{}
		input := reading(nil, ReadDocument{Title: "T", Text: "body"})
		input.Format = tc.format
		if _, err := New(service).StoreReading(context.Background(), input); err != nil {
			t.Fatal(err)
		}
		meta := service.payloads[0]["metadata"].(map[string]any)
		if meta["form"] != tc.form || meta["content_kind"] != "general" || meta["form_choice"].(map[string]any)["origin"] != tc.origin {
			t.Fatalf("%s: %#v", tc.format, meta)
		}
		patch := service.payloads[0]["metadata_if_unchanged"].(map[string]any)
		if _, exists := patch["form"]; exists {
			t.Fatal("reread must preserve the existing form")
		}
		if _, exists := patch["content_kind"]; exists {
			t.Fatal("reread must preserve the existing content kind")
		}
	}
}

func TestInvalidMetadataIsRefusedBeforeStorage(t *testing.T) {
	for _, raw := range []any{nil, "book", []string{"book"}, map[string]any{"form": "pdf"},
		map[string]any{"form": nil}, map[string]any{"topics": []string{""}},
		map[string]any{"entities": []map[string]any{{"type": "person"}}},
		map[string]any{"date_anchor": "2026-02-30"},
		map[string]any{"form_choice": map[string]any{"origin": "classifier"}},
	} {
		service := &ingestService{}
		payload := map[string]any{"title": "T", "body": "body", "metadata": raw}
		if _, err := New(service).Handle(context.Background(), Request{Action: ActionIngestKnowledge, Event: Event{Payload: payload}}); err == nil {
			t.Fatalf("accepted %#v", raw)
		}
		if len(service.payloads) != 0 {
			t.Fatal("invalid metadata reached storage")
		}
		payload["filename"] = "a.pdf"
		payload["content_b64"] = "eA=="
		delete(payload, "body")
		if err := ValidateRead(payload); err == nil {
			t.Fatalf("read accepted %#v", raw)
		}
	}
}

func TestIngestResolvesMetadataWithoutChangingLegacyKindOrRequest(t *testing.T) {
	service := &ingestService{}
	payload := map[string]any{"title": "T", "body": "body", "kind": "meme_hashtag", "metadata": map[string]any{"content_kind": "news"}}
	if _, err := New(service).Handle(context.Background(), Request{Action: ActionIngestKnowledge, Event: Event{Payload: payload}}); err != nil {
		t.Fatal(err)
	}
	meta := service.payloads[0]["metadata"].(map[string]any)
	if meta["form"] != "general" || meta["content_kind"] != "news" || service.payloads[0]["kind"] != "meme_hashtag" {
		t.Fatalf("payload=%#v", service.payloads[0])
	}
	if len(payload["metadata"].(map[string]any)) != 1 {
		t.Fatal("input mutated")
	}
}
