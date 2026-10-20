package handlers

import (
	"context"
	"encoding/base64"
	"errors"
	"read_books/internal/core"
	"reflect"
	"strings"
	"sync"
	"testing"
)

type fakeReader struct {
	mu       sync.Mutex
	calls    int
	filename string
	content  string
	answer   map[string]any
	err      error
}

func (f *fakeReader) Extract(_ context.Context, filename, contentB64 string) (map[string]any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.filename, f.content = filename, contentB64
	return f.answer, f.err
}

// ingestRecorder is Knowledge: it records each knowledge.ingest and can refuse the nth.
type ingestRecorder struct {
	payloads []map[string]any
	others   []string
	failAt   int
	changed  bool
}

func (r *ingestRecorder) Dispatch(_ context.Context, eventType string, payload map[string]any) (map[string]any, error) {
	if eventType != "knowledge.ingest" {
		r.others = append(r.others, eventType)
		return map[string]any{"ok": true}, nil
	}
	r.payloads = append(r.payloads, payload)
	if r.failAt == len(r.payloads) {
		return nil, errors.New("body is larger than 2000000 characters")
	}
	return map[string]any{"source_id": payload["source_id"], "changed": r.changed, "metadata": payload["metadata"]}, nil
}

func readAnswer(texts ...string) map[string]any {
	documents := make([]any, len(texts))
	for i, text := range texts {
		documents[i] = map[string]any{"title": "Part " + string(rune('A'+i)), "text": text, "source_ref": "", "key": string(rune('a' + i))}
	}
	return map[string]any{"documents": documents, "count": float64(len(texts)), "format": ".pdf",
		"plugin": "knowledge-extractors", "version": "0.1.0"}
}

// bigFile is larger than the task workspace budget, as most real files are.
var bigFile = base64.StdEncoding.EncodeToString([]byte(strings.Repeat("page ", 40_000)))

func readEvent(payload map[string]any) core.Event {
	return core.Event{EventID: "evt-read", Source: core.SourceCLI, Type: "knowledge.read", Payload: payload}
}

func readingOrchestrator(reader DocumentReader, knowledge *ingestRecorder, enabled func() bool) *core.Service {
	return core.DefaultService(NewKnowledgeDomainHandler(knowledge).WithReading(NewReadingTask(reader, knowledge, enabled)))
}

func TestKnowledgeReadStoresWhatTheReadingHostRead(t *testing.T) {
	reader := &fakeReader{answer: readAnswer("Focus is a skill.")}
	knowledge := &ingestRecorder{changed: true}

	result, err := readingOrchestrator(reader, knowledge, nil).ProcessWithResult(context.Background(), readEvent(map[string]any{
		"filename": "Deep Work.pdf", "content_b64": bigFile, "kind": "book", "title": "Deep Work",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if reader.calls != 1 || reader.filename != "Deep Work.pdf" || reader.content != bigFile {
		t.Fatalf("the reading host must get the file as sent: calls=%d name=%q", reader.calls, reader.filename)
	}
	want := map[string]any{
		"title": "Deep Work", "body": "Focus is a skill.", "kind": "book", "source_id": "file:Deep-Work.pdf",
		"tags": []string{"format:pdf", "reader:knowledge-extractors"},
	}
	if len(knowledge.payloads) != 1 {
		t.Fatalf("payloads=%#v", knowledge.payloads)
	}
	metadata := knowledge.payloads[0]["metadata"].(map[string]any)
	if metadata["form"] != nil || metadata["content_kind"] != nil {
		t.Fatalf("metadata=%#v", metadata)
	}
	delete(knowledge.payloads[0], "metadata")
	delete(knowledge.payloads[0], "metadata_if_unchanged")
	if !reflect.DeepEqual(knowledge.payloads[0], want) {
		t.Fatalf("ingest payloads=%#v", knowledge.payloads)
	}

	answer := result.Result["knowledge"].(map[string]any)
	documents := answer["documents"].([]map[string]any)
	classification := documents[0]["classification"].(map[string]any)
	if classification["status"] != "pending" || classification["needs_user_input"] != true || len(classification["questions"].([]map[string]any)) != 2 {
		t.Fatalf("reading must return unresolved questions to the user: %#v", classification)
	}

	task := answer["task"].(core.TaskResult)
	if answer["count"] != 1 || answer["reader"] != "knowledge-extractors" || answer["format"] != ".pdf" ||
		task.Status != core.TaskCompleted || len(task.Steps) != 2 || task.Steps[0].Name != "read" || task.Steps[1].Name != "ingest" {
		t.Fatalf("unexpected answer %#v", answer)
	}
	if len(task.Evidence) != 1 || !strings.HasPrefix(task.Evidence[0].Ref, "file:sha256:") ||
		task.Evidence[0].Version != "knowledge-extractors@0.1.0" || strings.Contains(task.Evidence[0].Ref, "Deep") {
		t.Fatalf("the evidence must name the reader and a hash of the file, never the file: %#v", task.Evidence)
	}
	if _, kept := task.Outputs["read"]["text"]; kept {
		t.Fatal("the text must stay out of the workspace")
	}
}

func TestKnowledgeReadGivesEachHostFailureAReasonAndStoresNothing(t *testing.T) {
	cases := map[string]struct {
		err    error
		answer map[string]any
		reason string
	}{
		"unavailable":     {err: errors.New(`unavailable: no enabled provider of document.extract.v1 for ".pdf"`), reason: "reading_unavailable"},
		"ambiguous":       {err: errors.New("ambiguous: document.extract.v1 for \".pdf\" is provided by a, b; bind one explicitly"), reason: "reading_ambiguous"},
		"timeout":         {err: errors.New("timeout: timed out after 1m30s"), reason: "reading_timeout"},
		"crash":           {err: errors.New("failed: exit status 3"), reason: "reading_failed"},
		"unreadable":      {err: errors.New("unreadable: the PDF is password protected"), reason: "reading_unreadable"},
		"host down":       {err: context.DeadlineExceeded, reason: "reading_host_unavailable"},
		"empty answer":    {answer: map[string]any{"documents": []any{}}, reason: "reading_invalid_result"},
		"malformed shape": {answer: map[string]any{"documents": "text"}, reason: "reading_invalid_result"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			knowledge := &ingestRecorder{}
			_, err := readingOrchestrator(&fakeReader{answer: tc.answer, err: tc.err}, knowledge, nil).
				ProcessWithResult(context.Background(), readEvent(map[string]any{"filename": "a.pdf", "content_b64": "eA=="}))
			if err == nil || !strings.Contains(err.Error(), "failed: "+tc.reason) {
				t.Fatalf("got %v, want reason %s", err, tc.reason)
			}
			if len(knowledge.payloads) != 0 {
				t.Fatal("nothing may be stored when the reading failed")
			}
		})
	}
}

func TestKnowledgeReadReportsAPartialIngest(t *testing.T) {
	knowledge := &ingestRecorder{failAt: 2, changed: true}
	_, err := readingOrchestrator(&fakeReader{answer: readAnswer("one", "two", "three")}, knowledge, nil).
		ProcessWithResult(context.Background(), readEvent(map[string]any{"filename": "a.epub", "content_b64": "eA=="}))
	if err == nil || !strings.Contains(err.Error(), "knowledge_ingest_failed") || !strings.Contains(err.Error(), "1 of 3 documents stored") {
		t.Fatalf("a refused document must end the task and say what was kept: %v", err)
	}
}

func TestReadingTheSameFileAgainUpdatesTheSameDocuments(t *testing.T) {
	knowledge := &ingestRecorder{changed: false}
	orchestrator := readingOrchestrator(&fakeReader{answer: readAnswer("one", "two")}, knowledge, nil)
	for range 2 {
		if _, err := orchestrator.ProcessWithResult(context.Background(),
			readEvent(map[string]any{"filename": "a.epub", "content_b64": "eA=="})); err != nil {
			t.Fatal(err)
		}
	}
	if len(knowledge.payloads) != 4 || knowledge.payloads[0]["source_id"] != knowledge.payloads[2]["source_id"] ||
		knowledge.payloads[1]["source_id"] != knowledge.payloads[3]["source_id"] {
		t.Fatalf("a second reading must address the same documents: %#v", knowledge.payloads)
	}
}

func TestKnowledgeReadIsRefusedBeforeReadingWhenItCannotRun(t *testing.T) {
	cases := map[string]struct {
		payload map[string]any
		enabled func() bool
		want    string
	}{
		"no file":      {map[string]any{"filename": "a.pdf"}, nil, "content_b64 is required"},
		"body instead": {map[string]any{"filename": "a.pdf", "content_b64": "eA==", "body": "x"}, nil, "knowledge.ingest"},
		"feeds is off": {map[string]any{"filename": "a.pdf", "content_b64": "eA=="}, func() bool { return false }, "switched off"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			reader := &fakeReader{answer: readAnswer("x")}
			knowledge := &ingestRecorder{}
			_, err := readingOrchestrator(reader, knowledge, tc.enabled).ProcessWithResult(context.Background(), readEvent(tc.payload))
			if err == nil || !strings.Contains(err.Error(), tc.want) || reader.calls != 0 || len(knowledge.payloads) != 0 {
				t.Fatalf("err=%v reads=%d ingests=%d", err, reader.calls, len(knowledge.payloads))
			}
		})
	}
}

func TestWithoutTheReadingHostKnowledgeReadIsRefusedAndIngestIsUnchanged(t *testing.T) {
	knowledge := &ingestRecorder{}
	orchestrator := core.DefaultService(NewKnowledgeDomainHandler(knowledge))

	if _, err := orchestrator.ProcessWithResult(context.Background(),
		readEvent(map[string]any{"filename": "a.pdf", "content_b64": "eA=="})); err == nil || len(knowledge.payloads) != 0 {
		t.Fatalf("knowledge.read must be refused without the reading host: %v", err)
	}
	upload := map[string]any{"filename": "a.pdf", "content_b64": "eA=="}
	if _, err := orchestrator.ProcessWithResult(context.Background(), core.Event{
		Source: core.SourceCLI, Type: "knowledge.ingest", Payload: upload,
	}); err != nil || len(knowledge.payloads) != 1 || !reflect.DeepEqual(knowledge.payloads[0], upload) {
		t.Fatalf("an uploaded file must still reach Knowledge as before: %v %#v", err, knowledge.payloads)
	}
}
