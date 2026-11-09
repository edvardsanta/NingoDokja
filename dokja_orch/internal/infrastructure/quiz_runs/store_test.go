package quizruns

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	quiz "read_books/dokja_domain/dokja_quiz"
)

func run(label string, at time.Time) quiz.Run {
	return quiz.Run{Site: "quiz.example.com", Label: label, URL: "https://quiz.example.com/r", FinishedAt: at,
		Items: []quiz.AnsweredItem{{Question: "Tradition matters.", Answer: "Agree"}}}
}

func TestRunsSurviveAReopenNewestFirstAndTrimmed(t *testing.T) {
	path := PathBeside(filepath.Join(t.TempDir(), "dokja.db"))
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	for i := range kept + 5 {
		if err := store.Save(context.Background(), run(string(rune('A'+i%26)), start.Add(time.Duration(i)*time.Hour))); err != nil {
			t.Fatal(err)
		}
	}
	store.Close()

	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	all, err := store.Recent(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != kept || !all[0].FinishedAt.Equal(start.Add(time.Duration(kept+4)*time.Hour)) || all[0].Items[0].Answer != "Agree" {
		t.Fatalf("got %d runs, newest %v", len(all), all[0].FinishedAt)
	}
	recent, _ := store.Recent(context.Background(), quiz.RememberedRuns)
	if len(recent) != quiz.RememberedRuns {
		t.Fatalf("recent = %d", len(recent))
	}
}

func TestAnInvalidRunIsNotStored(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), FileName))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	bad := run("x", time.Now())
	bad.URL = "https://elsewhere.example/r"
	if err := store.Save(context.Background(), bad); err == nil {
		t.Fatal("a result link off the quiz host was stored")
	}
	if runs, _ := store.Recent(context.Background(), 5); len(runs) != 0 {
		t.Fatal(runs)
	}
}
