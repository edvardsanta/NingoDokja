package handlers

import (
	"context"
	"errors"
	"fmt"
	"read_books/internal/core"
	"strings"
	"testing"
)

type reclassificationStore struct {
	*knowledgeStore
	samples int
}

func (s *reclassificationStore) Dispatch(ctx context.Context, event string, payload map[string]any) (map[string]any, error) {
	if event == "knowledge.classification.sample" {
		s.samples++
		id := payload["source_id"].(string)
		return map[string]any{"source_id": id, "title": "Stored document", "text": "Private stored evidence.", "metadata": s.metadata[id], "revision": fmt.Sprint(s.revision[id])}, nil
	}
	if event == "knowledge.metadata.patch" {
		var revision int
		fmt.Sscan(payload["revision"].(string), &revision)
		payload["revision"] = revision
	}
	return s.knowledgeStore.Dispatch(ctx, event, payload)
}

func TestReclassifyRouteUsesStoredTextAndPreservesConcurrentAnswers(t *testing.T) {
	for _, scenario := range []string{"success", "conflict", "failure", "uncertain", "user", "invalid"} {
		t.Run(scenario, func(t *testing.T) {
			store := &reclassificationStore{knowledgeStore: newKnowledgeStore()}
			store.metadata["study"] = map[string]any{
				"form": "book", "form_choice": map[string]any{"origin": "user"},
				"content_kind": "news", "kind_choice": map[string]any{"origin": "classifier", "classifier_id": "old", "classifier_version": "0"},
			}
			store.revision["study"] = 1
			if scenario == "conflict" {
				store.answerAt = 1
			}
			if scenario == "user" {
				store.metadata["study"]["kind_choice"] = map[string]any{"origin": "user_required", "fallback_reason": "user_clarification_required"}
			}
			reader := &classifyingReader{proposals: []any{map[string]any{"form": "article", "content_kind": "research"}}}
			if scenario == "failure" {
				reader.err = errors.New("timeout: private provider output")
			}
			if scenario == "uncertain" {
				reader.proposals = []any{map[string]any{"uncertain_content_kind": true}}
			}
			if scenario == "invalid" {
				reader.proposals = []any{}
			}
			service := core.DefaultService(NewKnowledgeDomainHandler(store).WithReading(NewReadingTask(reader, store, nil)))
			response, err := service.ProcessWithResult(context.Background(), core.Event{Source: core.SourceCLI, Type: "knowledge.reclassify", Payload: map[string]any{"source_id": "study"}})
			wantFailure := scenario == "conflict" || scenario == "failure" || scenario == "invalid"
			if (err != nil) != wantFailure {
				t.Fatalf("response=%#v err=%v", response, err)
			}
			if store.samples != 1 || len(store.bodies) != 0 {
				t.Fatal("must sample once, never ingest")
			}
			if wantFailure {
				if store.metadata["study"]["content_kind"] != "news" {
					t.Fatal("failed proposal changed labels")
				}
				if err != nil && strings.Contains(err.Error(), "private provider output") {
					t.Fatal("provider output exposed")
				}
				return
			}
			if scenario == "user" {
				if len(reader.sent) != 0 || store.patches != 0 {
					t.Fatal("user question sent to classifier")
				}
				return
			}
			if len(reader.sent) != 1 || reader.sent[0][0]["text"] != "Private stored evidence." || reader.format != "" {
				t.Fatal("wrong classifier sample")
			}
			metadata := store.metadata["study"]
			if metadata["form"] != "book" {
				t.Fatal("user form overwritten")
			}
			if scenario == "uncertain" {
				if metadata["content_kind"] != nil {
					t.Fatal("uncertainty did not become a question")
				}
			} else if metadata["content_kind"] != "research" {
				t.Fatal("automatic axis not revised")
			}
			if strings.Contains(fmt.Sprintf("%#v", response), "Private stored evidence") {
				t.Fatal("sample leaked into response or trace")
			}
		})
	}
}
