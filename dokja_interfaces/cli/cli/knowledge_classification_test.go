package cli

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func pendingQuestion(field string) any {
	values := []any{"research", "reference", "news", "narrative"}
	if field == "form" {
		values = []any{"book", "article", "note"}
	}
	return map[string]any{"field": field, "question": "Choose " + field, "allowed_values": values}
}
func classificationSnapshot(fields ...string) map[string]any {
	questions := []any{}
	for _, field := range fields {
		questions = append(questions, pendingQuestion(field))
	}
	return map[string]any{"source_id": "study", "revision": strings.Repeat("a", 64), "classification": map[string]any{"needs_user_input": len(fields) > 0, "questions": questions}}
}
func TestClassificationCommandCollectsOnlyUserAnswersAndResumesOnce(t *testing.T) {
	payloads := []map[string]any{}
	call := func(_ context.Context, eventType string, payload map[string]any) (map[string]any, error) {
		if eventType != "knowledge.classify" {
			t.Fatalf("unexpected event %s", eventType)
		}
		payloads = append(payloads, payload)
		if len(payloads) == 1 {
			return classificationSnapshot("form", "content_kind"), nil
		}
		return classificationSnapshot(), nil
	}
	command := newKnowledgeClassifyCommand(context.Background(), call)
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetIn(strings.NewReader("guess\nbook\nresearch\n"))
	command.SetArgs([]string{"study"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if len(payloads) != 2 || !reflect.DeepEqual(payloads[1], map[string]any{"source_id": "study", "revision": strings.Repeat("a", 64), "answers": map[string]any{"form": "book", "content_kind": "research"}}) {
		t.Fatalf("payloads=%#v", payloads)
	}
	if !strings.Contains(output.String(), "no default") || !strings.Contains(output.String(), "Classification saved") {
		t.Fatalf("output=%s", output.String())
	}
}
func TestClassificationCommandFlagsNeverPromptOrFillMissingAxes(t *testing.T) {
	calls := 0
	call := func(_ context.Context, _ string, payload map[string]any) (map[string]any, error) {
		calls++
		if calls == 1 {
			return classificationSnapshot("form", "content_kind"), nil
		}
		if !reflect.DeepEqual(payload["answers"], map[string]any{"form": "book"}) {
			t.Fatalf("answers=%#v", payload)
		}
		return classificationSnapshot("content_kind"), nil
	}
	command := newKnowledgeClassifyCommand(context.Background(), call)
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetIn(strings.NewReader("research\n"))
	command.SetArgs([]string{"study", "--form", "book"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || !strings.Contains(output.String(), "still needs your input") || strings.Contains(output.String(), "Choose content_kind") {
		t.Fatalf("output=%s", output.String())
	}
}
func TestClassificationCommandEmptyAnswersAndEOFDoNotWrite(t *testing.T) {
	for _, input := range []string{"\n\n", "", "book\n"} {
		calls := 0
		command := newKnowledgeClassifyCommand(context.Background(), func(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
			calls++
			return classificationSnapshot("form", "content_kind"), nil
		})
		var output bytes.Buffer
		command.SetOut(&output)
		command.SetErr(&output)
		command.SetIn(strings.NewReader(input))
		command.SetArgs([]string{"study"})
		err := command.Execute()
		if calls != 1 {
			t.Fatalf("input=%q wrote answers without complete user input", input)
		}
		if input != "\n\n" && err == nil {
			t.Fatalf("EOF should leave the decision pending: %s", output.String())
		}
		if input == "\n\n" && (err != nil || !strings.Contains(output.String(), "remains pending")) {
			t.Fatalf("err=%v output=%s", err, output.String())
		}
	}
}
func TestClassificationCommandDoesNotRetryAConflict(t *testing.T) {
	calls := 0
	command := newKnowledgeClassifyCommand(context.Background(), func(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
		calls++
		if calls == 1 {
			return classificationSnapshot("content_kind"), nil
		}
		return nil, errors.New("classification_conflict: document changed")
	})
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SetArgs([]string{"study", "--content-kind", "research"})
	err := command.Execute()
	if err == nil || !strings.Contains(err.Error(), "classification_conflict") || calls != 2 || strings.Contains(output.String(), "Classification saved") {
		t.Fatalf("calls=%d err=%v output=%s", calls, err, output.String())
	}
}
func TestKnowledgeOutputPointsPendingDocumentsToClassification(t *testing.T) {
	answer := classificationSnapshot("content_kind")
	answer["created"] = true
	answer["changed"] = true
	answer["chunks"] = 1.0
	if out := FormatIngest("study.pdf", answer); !strings.Contains(out, "knowledge classify study") {
		t.Fatalf("output=%s", out)
	}
	if out := FormatList(map[string]any{"documents": []any{answer}, "total": 1.0}); !strings.Contains(out, "knowledge classify study") {
		t.Fatalf("output=%s", out)
	}
}

func TestReclassifySendsOnlyTheDocumentIDAndNeverRetries(t *testing.T) {
	for _, fail := range []bool{false, true} {
		calls := 0
		command := newKnowledgeReclassifyCommand(context.Background(), func(_ context.Context, event string, payload map[string]any) (map[string]any, error) {
			calls++
			if event != "knowledge.reclassify" || !reflect.DeepEqual(payload, map[string]any{"source_id": "study"}) {
				t.Fatalf("%s %#v", event, payload)
			}
			if fail {
				return nil, errors.New("classification_conflict")
			}
			return map[string]any{"reclassified": true}, nil
		})
		command.SetOut(&bytes.Buffer{})
		command.SetArgs([]string{"study"})
		err := command.Execute()
		if (err != nil) != fail || calls != 1 {
			t.Fatalf("calls=%d err=%v", calls, err)
		}
	}
}
