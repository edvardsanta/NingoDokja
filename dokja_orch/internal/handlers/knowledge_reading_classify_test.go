package handlers

import (
	"context"
	"errors"
	"read_books/internal/core"
	"strings"
	"sync"
	"testing"
)

// classifyingReader is a reading host with a document.classify.v1 plugin.
type classifyingReader struct {
	fakeReader
	mu        sync.Mutex
	sent      [][]map[string]any
	format    string
	proposals []any
	err       error
}

func (c *classifyingReader) Classify(_ context.Context, format string, documents []map[string]any) (map[string]any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = append(c.sent, documents)
	c.format = format
	if c.err != nil {
		return nil, c.err
	}
	return map[string]any{"classifications": c.proposals, "plugin": "kinds", "version": "1"}, nil
}

// knowledgeStore is Knowledge with content identity: an ingest of the same body is unchanged
// and keeps its metadata, and metadata patches are bound to a revision.
type knowledgeStore struct {
	mu       sync.Mutex
	bodies   map[string]string
	metadata map[string]map[string]any
	revision map[string]int
	patches  int
	answerAt int // a user answer lands just before the nth patch
}

func newKnowledgeStore() *knowledgeStore {
	return &knowledgeStore{bodies: map[string]string{}, metadata: map[string]map[string]any{}, revision: map[string]int{}}
}

func (k *knowledgeStore) Dispatch(_ context.Context, eventType string, payload map[string]any) (map[string]any, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	id, _ := payload["source_id"].(string)
	switch eventType {
	case "knowledge.ingest":
		body := payload["body"].(string)
		if previous, ok := k.bodies[id]; ok && previous == body {
			return map[string]any{"source_id": id, "changed": false, "metadata": k.metadata[id]}, nil
		}
		k.bodies[id] = body
		k.metadata[id] = payload["metadata"].(map[string]any)
		k.revision[id]++
		return map[string]any{"source_id": id, "changed": true, "metadata": k.metadata[id]}, nil
	case "knowledge.metadata.get":
		return map[string]any{"source_id": id, "metadata": k.metadata[id], "revision": k.revision[id]}, nil
	case "knowledge.metadata.patch":
		k.patches++
		if k.patches == k.answerAt {
			k.revision[id]++
		}
		if payload["revision"] != k.revision[id] {
			return nil, errors.New("classification_conflict: document changed; reload the questions")
		}
		merged := map[string]any{}
		for key, value := range k.metadata[id] {
			merged[key] = value
		}
		for key, value := range payload["patch"].(map[string]any) {
			merged[key] = value
		}
		k.metadata[id] = merged
		k.revision[id]++
		return map[string]any{"source_id": id, "metadata": merged}, nil
	}
	return nil, errors.New("unexpected " + eventType)
}

func classifyOrchestrator(reader DocumentReader, knowledge *knowledgeStore) *core.Service {
	return core.DefaultService(NewKnowledgeDomainHandler(knowledge).WithReading(NewReadingTask(reader, knowledge, nil)))
}

func readFile(t *testing.T, service *core.Service, payload map[string]any) map[string]any {
	t.Helper()
	base := map[string]any{"filename": "Deep Work.pdf", "content_b64": bigFile}
	for key, value := range payload {
		base[key] = value
	}
	result, err := service.ProcessWithResult(context.Background(), readEvent(base))
	if err != nil {
		t.Fatal(err)
	}
	return result.Result["knowledge"].(map[string]any)
}

func TestKnowledgeReadClassifiesNewContentOnceAndKeepsItOnRereads(t *testing.T) {
	reader := &classifyingReader{fakeReader: fakeReader{answer: readAnswer("Focus is a skill.")},
		proposals: []any{map[string]any{"form": "book", "content_kind": "research", "topics": []any{"attention"}}}}
	knowledge := newKnowledgeStore()
	service := classifyOrchestrator(reader, knowledge)

	answer := readFile(t, service, nil)
	metadata := knowledge.metadata["file:Deep-Work.pdf"]
	choice := metadata["kind_choice"].(map[string]any)
	if metadata["form"] != "book" || metadata["content_kind"] != "research" || choice["origin"] != "classifier" ||
		choice["classifier_id"] != "kinds" || choice["classifier_version"] != "1" {
		t.Fatalf("the proposal must be stored with its provenance: %#v", metadata)
	}
	if reader.format != ".pdf" || len(reader.sent) != 1 || reader.sent[0][0]["text"] != "Focus is a skill." {
		t.Fatalf("the classifier must get the documents read: %#v", reader.sent)
	}
	documents := answer["documents"].([]map[string]any)
	if documents[0]["classification"].(map[string]any)["status"] != "classified" ||
		answer["classification"].(map[string]any)["classifier"] != "kinds@1" {
		t.Fatalf("the reply must show the classification: %#v", answer)
	}

	again := readFile(t, service, nil)
	if len(reader.sent) != 1 {
		t.Fatal("an unchanged reread must not ask the classifier again")
	}
	if again["classification"].(map[string]any)["candidates"] != 0 ||
		again["documents"].([]map[string]any)[0]["metadata"].(map[string]any)["content_kind"] != "research" {
		t.Fatalf("an unchanged reread keeps the earlier classification: %#v", again)
	}

	reader.answer = readAnswer("Deep work, second edition.")
	readFile(t, service, nil)
	if len(reader.sent) != 2 {
		t.Fatal("changed content is classified again")
	}
}

func TestKnowledgeReadSendsNotesButNothingTheUserAlreadyDecided(t *testing.T) {
	reader := &classifyingReader{fakeReader: fakeReader{answer: readAnswer("Shopping list.")},
		proposals: []any{map[string]any{"content_kind": "reference"}}}
	knowledge := newKnowledgeStore()
	service := classifyOrchestrator(reader, knowledge)

	readFile(t, service, map[string]any{"filename": "b.pdf", "metadata": map[string]any{"form": "book", "content_kind": "narrative"}})
	if len(reader.sent) != 0 {
		t.Fatalf("a fully decided document never reaches the classifier: %#v", reader.sent)
	}
	readFile(t, service, map[string]any{"metadata": map[string]any{"form": "note"}})
	metadata := knowledge.metadata["file:Deep-Work.pdf"]
	if len(reader.sent) != 1 || metadata["form"] != "note" || metadata["content_kind"] != "reference" {
		t.Fatalf("a note is classified like any document, keeping the user's form: sent=%d metadata=%v", len(reader.sent), metadata)
	}
}

func TestKnowledgeReadKeepsTheDocumentsWhenClassificationFails(t *testing.T) {
	cases := map[string]struct {
		err       error
		proposals []any
		answerAt  int
		reason    string
	}{
		"no classifier":  {err: errors.New(`unavailable: no enabled provider of document.classify.v1 for "documents"`), reason: "classifier_unavailable"},
		"timeout":        {err: errors.New("timeout: timed out after 1m0s"), reason: "classifier_timeout"},
		"broken plugin":  {err: errors.New("failed: output is not a classifications object"), reason: "classifier_failed"},
		"host down":      {err: errors.New("feeds service request failed: resource temporarily unavailable"), reason: "classifier_host_unavailable"},
		"wrong count":    {proposals: []any{}, reason: "classifier_invalid_result"},
		"user answered":  {proposals: []any{map[string]any{"form": "book"}}, answerAt: 1, reason: "knowledge_classify_failed"},
		"not a proposal": {proposals: []any{"book"}, reason: "classifier_invalid_result"},
	}
	for name, tc := range cases {
		reader := &classifyingReader{fakeReader: fakeReader{answer: readAnswer("Focus is a skill.")}, err: tc.err, proposals: tc.proposals}
		knowledge := newKnowledgeStore()
		knowledge.answerAt = tc.answerAt
		answer := readFile(t, classifyOrchestrator(reader, knowledge), nil)

		task := answer["task"].(core.TaskResult)
		classification := answer["classification"].(map[string]any)
		metadata := knowledge.metadata["file:Deep-Work.pdf"]
		if task.Status != core.TaskCompleted || answer["count"] != 1 || classification["reason"] != tc.reason ||
			metadata["form"] != nil || metadata["form_choice"].(map[string]any)["origin"] != "user_required" {
			t.Errorf("%s: the document stays stored and pending: reason=%v status=%s metadata=%v",
				name, classification["reason"], task.Status, metadata)
		}
		if strings.Contains(task.Reason, "classif") {
			t.Errorf("%s: a classification problem is not a task failure", name)
		}
	}
}
