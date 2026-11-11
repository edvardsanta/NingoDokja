package handlers

import (
	"context"
	"fmt"
	"strings"
	"testing"

	communications "read_books/dokja_domain/dokja_communications"
	quiz "read_books/dokja_domain/dokja_quiz"
)

type workflowTaskModel struct {
	text                 string
	json                 string
	textCalls, jsonCalls int
	messages             []map[string]string
}

func (*workflowTaskModel) GenerateMessages(context.Context, []map[string]string) (map[string]any, error) {
	return nil, fmt.Errorf("legacy generation should not run")
}
func (m *workflowTaskModel) GenerateTaskText(_ context.Context, profile string, messages []map[string]string, accept func(string) error) (map[string]any, error) {
	if profile != "lightweight" {
		return nil, fmt.Errorf("wrong profile")
	}
	m.textCalls++
	m.messages = messages
	if accept != nil {
		if err := accept(m.text); err != nil {
			return nil, err
		}
	}
	return map[string]any{"reply": m.text}, nil
}
func (m *workflowTaskModel) GenerateTaskJSON(_ context.Context, profile string, _ map[string]any, messages []map[string]string, accept func(map[string]any) error) (map[string]any, error) {
	if profile != "lightweight" {
		return nil, fmt.Errorf("wrong profile")
	}
	m.jsonCalls++
	m.messages = messages
	reply := map[string]any{"reply": m.json}
	if err := accept(reply); err != nil {
		return nil, err
	}
	return reply, nil
}

func TestMusicRemarksUseTheSharedTaskAPI(t *testing.T) {
	model := &workflowTaskModel{text: "Uma boa escolha musical."}
	desk := &musicDesk{comments: model}
	got := desk.remark(t.Context(), communications.QueuedTrack{Track: communications.Track{Title: "Example Song"}}, "pt-BR")
	if got == "" || model.textCalls != 1 || !strings.Contains(model.messages[0]["content"], "pt-BR") {
		t.Fatalf("remark %q calls %d", got, model.textCalls)
	}
}

func TestQuizAnswersAndCommentsUseSharedTasksWithTheWorldview(t *testing.T) {
	model := &workflowTaskModel{json: `{"answers":[{"id":"q1","option":"Agree"}]}`, text: "Comentário final."}
	handler := NewQuizDomainHandler(nil, model, nil).WithVisions("Configured conviction.")
	questions := []quiz.Question{{ID: "q1", Text: "Example question", Options: []string{"Agree", "Disagree"}}}
	answers, refusal := handler.answer(t.Context(), questions)
	if refusal != "" || answers["q1"] != "Agree" || model.jsonCalls != 1 || !strings.Contains(model.messages[0]["content"], "Configured conviction.") {
		t.Fatalf("answers %v refusal %s", answers, refusal)
	}
	if got := handler.comment(t.Context(), quiz.Result{}, "pt-BR"); got != "Comentário final." || model.textCalls != 1 {
		t.Fatalf("comment %q calls %d", got, model.textCalls)
	}
	model.json = `{"answers":[{"id":"q1","option":"Invented"}]}`
	if _, reason := handler.answer(t.Context(), questions); reason == "" {
		t.Fatal("invalid quiz answer accepted")
	}
}
