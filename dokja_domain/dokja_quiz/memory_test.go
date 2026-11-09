package quiz

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func sampleQuiz() Quiz {
	return Quiz{Title: "Example test", Questions: []Question{
		{ID: "q1", Text: "The state should plan the economy.", Options: []string{"Agree", "Disagree"}},
		{ID: "q2", Text: "Tradition matters.", Options: []string{"Agree", "Disagree"}},
		{ID: "q3", Text: "Never shown.", Options: []string{"Agree", "Disagree"}},
	}}
}

func TestOnlyDistinctQuizIDsCountAsAsked(t *testing.T) {
	quiz := sampleQuiz()
	if asked, err := ParseAsked(nil, quiz); err != nil || asked != nil {
		t.Fatal("an absent list means unknown, not an error")
	}
	if asked, err := ParseAsked([]any{"q2", "q1"}, quiz); err != nil || strings.Join(asked, ",") != "q2,q1" {
		t.Fatal(asked, err)
	}
	for _, bad := range []any{"q1", []any{"q1", "q1"}, []any{"q9"}, []any{1}} {
		if _, err := ParseAsked(bad, quiz); err == nil {
			t.Fatalf("accepted %v", bad)
		}
	}
}

func TestARunRemembersTheAskedQuestionsWithNingosAnswersInOrder(t *testing.T) {
	at := time.Date(2026, 11, 9, 15, 12, 0, 0, time.FixedZone("BRT", -3*3600))
	run := NewRun("https://quiz.example.com/start", sampleQuiz(), Result{URL: "https://quiz.example.com/r?a=1", Label: "A verdict"},
		map[string]string{"q1": "Disagree", "q2": "Agree", "q3": "Agree"}, []string{"q2", "q1"}, "A\ncomment", at)
	if run.Site != "quiz.example.com" || run.Label != "A verdict" || run.Comment != "A comment" || !run.FinishedAt.Equal(at) || run.FinishedAt.Location() != time.UTC {
		t.Fatalf("%#v", run)
	}
	if len(run.Items) != 2 || run.Items[0] != (AnsweredItem{"Tradition matters.", "Agree"}) || run.Items[1].Answer != "Disagree" {
		t.Fatalf("items = %#v", run.Items)
	}
	if err := ValidateRun(run); err != nil {
		t.Fatal(err)
	}
}

func TestARememberedRunReadBackIsCheckedLikeSiteData(t *testing.T) {
	good := NewRun("https://quiz.example.com/start", sampleQuiz(), Result{URL: "https://quiz.example.com/r"}, nil, nil, "", time.Now())
	for name, mutate := range map[string]func(*Run){
		"no site":        func(r *Run) { r.Site = "" },
		"no time":        func(r *Run) { r.FinishedAt = time.Time{} },
		"foreign link":   func(r *Run) { r.URL = "https://elsewhere.example/r" },
		"long label":     func(r *Run) { r.Label = strings.Repeat("x", MaxLabelRunes+1) },
		"empty answer":   func(r *Run) { r.Items = []AnsweredItem{{Question: "Q", Answer: ""}} },
		"too many items": func(r *Run) { r.Items = make([]AnsweredItem, MaxRememberedItems+1) },
	} {
		run := good
		mutate(&run)
		if err := ValidateRun(run); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
}

func TestTheConversationSeesRecentRunsAndOnlyTheNewestsAnswers(t *testing.T) {
	var runs []Run
	for i := range 4 {
		run := NewRun("https://quiz.example.com/s", sampleQuiz(), Result{URL: "https://quiz.example.com/r", Label: string(rune('A' + i))},
			map[string]string{"q1": "Agree"}, []string{"q1"}, "", time.Date(2026, 11, 9-i, 0, 0, 0, 0, time.UTC))
		runs = append(runs, run)
	}
	var views []map[string]any
	if err := json.Unmarshal([]byte(MemoryData(runs, true)), &views); err != nil {
		t.Fatal(err)
	}
	if len(views) != RememberedRuns || views[0]["label"] != "A" || views[0]["date"] != "2026-11-09" || views[0]["result_link"] != "https://quiz.example.com/r" {
		t.Fatalf("views = %v", views)
	}
	if views[0]["your_answers"] == nil || views[1]["your_answers"] != nil {
		t.Fatal("only the newest run carries its answers")
	}
	if strings.Contains(MemoryData(runs, false), "your_answers") {
		t.Fatal("a voice turn gets the results without answers")
	}
	if MemoryData(nil, true) != "" {
		t.Fatal("no runs, no record")
	}
}
