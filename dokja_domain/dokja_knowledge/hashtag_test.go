package knowledge

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type hashtagStoreStub struct {
	reply map[string]any
	err   error
	calls int
	kind  string
	input map[string]any
}

func (s *hashtagStoreStub) Dispatch(_ context.Context, kind string, input map[string]any) (map[string]any, error) {
	s.calls++
	s.kind, s.input = kind, input
	return s.reply, s.err
}

func TestHashtagPreparationPreservesUnicodeLabelsAndRejectsInvalidIntent(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{" Humor ", "#Humor"}, {"#Foo_1", "#Foo_1"}, {"café_2026", "#café_2026"}, {"中文", "#中文"},
	} {
		example, err := PrepareHashtagExample(" https://media.example/a.png ", tc.raw)
		if err != nil || example.SourceURL() != "https://media.example/a.png" {
			t.Fatalf("%q: example=%+v error=%v", tc.raw, example, err)
		}
		store := &hashtagStoreStub{}
		_, err = New(store).StoreHashtagExample(context.Background(), example, "example text")
		if err != nil || store.input["title"] != tc.want {
			t.Fatalf("%q: stored label=%v error=%v", tc.raw, store.input["title"], err)
		}
	}
	for _, raw := range []string{"", " ", "#", "two words", "#two-tags", "##tag", "emoji🙂"} {
		if _, err := PrepareHashtagExample("https://media.example/a.png", raw); err == nil {
			t.Fatalf("invalid label %q accepted", raw)
		}
	}
	if _, err := PrepareHashtagExample(" ", ""); err == nil || err.Error() != "meme hashtag tag requires a url" {
		t.Fatalf("URL validation must precede label validation: %v", err)
	}
}

func TestHashtagStorageIdentityAndPayloadRemainCompatible(t *testing.T) {
	const url = "https://media.example/a.png"
	const id = "meme-hashtag:4286467e0b88473247929fa128ef8c24"
	if HashtagKnowledgeID(" "+url+" ") != id || HashtagKnowledgeID(url+"?v=2") == id {
		t.Fatal("legacy source identity changed")
	}
	example, _ := PrepareHashtagExample(url, "Humor")
	store := &hashtagStoreStub{reply: map[string]any{"changed": true, "embedded": 1, "degraded": false, "reason": "indexed"}}
	domain := New(store)
	result, err := domain.StoreHashtagExample(context.Background(), example, "labelled text")
	want := map[string]any{"source_id": id, "source_ref": url, "kind": "meme_hashtag", "title": "#Humor", "body": "labelled text", "tags": []string{"meme-hashtag"}}
	if err != nil || store.kind != "knowledge.ingest" || !reflect.DeepEqual(store.input, want) {
		t.Fatalf("payload=%v error=%v", store.input, err)
	}
	if result["source_url"] != url || result["hashtag"] != "#Humor" || result["text"] != "labelled text" || result["embedded"] != true || result["reason"] != "indexed" || result["degraded"] != false {
		t.Fatal(result)
	}
	deleted, err := domain.DeleteHashtagExample(context.Background(), " "+url+" ")
	if err != nil || store.kind != "knowledge.delete" || !reflect.DeepEqual(store.input, map[string]any{"source_id": id}) || deleted["source_url"] != url {
		t.Fatalf("delete=%v payload=%v error=%v", deleted, store.input, err)
	}
}

func TestHashtagEmbeddingReceiptPreservesUnchangedAndPendingStates(t *testing.T) {
	example, _ := PrepareHashtagExample("https://media.example/a.png", "Humor")
	for _, tc := range []struct {
		reply map[string]any
		want  bool
	}{
		{map[string]any{"changed": true, "embedded": float64(1)}, true},
		{map[string]any{"changed": true, "embedded": 0}, false},
		{map[string]any{"changed": false, "embedded": float32(0), "pending_embeddings": int64(0)}, true},
		{map[string]any{"changed": false, "embedded": 0, "pending_embeddings": 2}, false},
	} {
		result, err := New(&hashtagStoreStub{reply: tc.reply}).StoreHashtagExample(context.Background(), example, "words")
		if err != nil || result["embedded"] != tc.want {
			t.Fatalf("reply=%v result=%v error=%v", tc.reply, result, err)
		}
	}
}

func TestHashtagSearchScopesExamplesAndKeepsIrrelevantNearestCandidate(t *testing.T) {
	store := &hashtagStoreStub{reply: map[string]any{
		"threshold": 0.8, "degraded": false,
		"hits": []any{map[string]any{"title": "#Humor", "score": 0.4, "relevant": false, "text": "earlier text", "source_ref": "https://media.example/a.png"}},
	}}
	result, err := New(store).SearchHashtagExamples(context.Background(), "new text", 0.8)
	want := map[string]any{"query": "new text", "k": 1, "kind": "meme_hashtag", "min_score": 0.8}
	if err != nil || store.kind != "knowledge.search" || !reflect.DeepEqual(store.input, want) {
		t.Fatalf("input=%v error=%v", store.input, err)
	}
	if result["hashtag"] != "#Humor" || result["relevant"] != false || result["score"] != 0.4 || result["matched_text"] != "earlier text" || result["matched_source_url"] != "https://media.example/a.png" {
		t.Fatal(result)
	}
	if _, exists := result["reason"]; exists {
		t.Fatal("a below-threshold candidate must keep the existing reason-free response")
	}
	for _, reply := range []map[string]any{
		{"hits": []any{}},
		{"hits": []any{}, "degraded": true, "reason": "keyword search only"},
	} {
		store.reply = reply
		result, err := New(store).SearchHashtagExamples(context.Background(), "new text", 0.6)
		if err != nil || result["hashtag"] != nil || result["relevant"] != false {
			t.Fatalf("result=%v error=%v", result, err)
		}
		wantReason := "no tagged meme examples in knowledge"
		if reason, exists := reply["reason"]; exists {
			wantReason = reason.(string)
		}
		if result["reason"] != wantReason {
			t.Fatal(result)
		}
	}
}

func TestHashtagListRetainsNamespacePagingAndExampleProjection(t *testing.T) {
	store := &hashtagStoreStub{reply: map[string]any{"total": 3, "offset": 2, "documents": []any{
		"malformed", map[string]any{"source_ref": "https://media.example/a.png", "title": "#Humor", "text": "words", "embedded": int64(1), "updated_at": "now", "private": "excluded"},
	}}}
	result, err := New(store).ListHashtagExamples(context.Background(), 20, 2)
	want := map[string]any{"kind": "meme_hashtag", "include_text": true, "limit": 20, "offset": 2}
	if err != nil || store.kind != "knowledge.list" || !reflect.DeepEqual(store.input, want) {
		t.Fatalf("input=%v error=%v", store.input, err)
	}
	examples := result["examples"].([]any)
	wantExample := map[string]any{"source_url": "https://media.example/a.png", "hashtag": "#Humor", "text": "words", "embedded": true, "updated_at": "now"}
	if result["total"] != 3 || result["offset"] != 2 || len(examples) != 1 || !reflect.DeepEqual(examples[0], wantExample) {
		t.Fatal(result)
	}
}

func TestHashtagInvalidWritesAndServiceFailuresDoNotInventReceipts(t *testing.T) {
	store := &hashtagStoreStub{}
	domain := New(store)
	if _, err := domain.StoreHashtagExample(context.Background(), HashtagExample{}, "words"); err == nil {
		t.Fatal("zero-value example reached storage")
	}
	if _, err := domain.DeleteHashtagExample(context.Background(), " "); err == nil || !strings.Contains(err.Error(), "untag requires a url") {
		t.Fatal(err)
	}
	if store.calls != 0 {
		t.Fatal("invalid writes reached storage")
	}
	example, _ := PrepareHashtagExample("https://media.example/a.png", "Humor")
	if _, err := domain.StoreHashtagExample(context.Background(), example, " "); err == nil || store.calls != 0 {
		t.Fatal("empty example text reached storage")
	}
	store.err = errors.New("unavailable")
	for _, call := range []func() (map[string]any, error){
		func() (map[string]any, error) {
			return domain.StoreHashtagExample(context.Background(), example, "words")
		},
		func() (map[string]any, error) {
			return domain.SearchHashtagExamples(context.Background(), "words", 0.6)
		},
		func() (map[string]any, error) { return domain.ListHashtagExamples(context.Background(), 10, 0) },
		func() (map[string]any, error) {
			return domain.DeleteHashtagExample(context.Background(), example.SourceURL())
		},
	} {
		if result, err := call(); result != nil || !errors.Is(err, store.err) {
			t.Fatalf("result=%v error=%v", result, err)
		}
	}
}
