package knowledge

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

var intakeNow = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

func intakeItem(plugin, id string, age time.Duration) map[string]any {
	return map[string]any{
		"plugin": plugin, "id": id, "title": "Title " + id, "body": "Text of " + id,
		"url": "https://example.com/" + id, "published": intakeNow.Add(-age).Format(time.RFC3339),
	}
}

func TestReadingSourceIDMatchesTheDesktop(t *testing.T) {
	// Pinned in dokja_interfaces/desktop/tests/actions.test.ts: the two must agree, or saving a
	// reading intake already kept makes a copy.
	got, err := ReadingSourceID("example-blog", "https://example.com/feed?a=1&b=<2>")
	if err != nil {
		t.Fatal(err)
	}
	const want = "reading:7ee828ae5b30c5673e46f73a29490312dfd301cfc57580e7d96d067f075e47de"
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestReadingSourceIDCutsLongIDsLikeTheDesktop(t *testing.T) {
	long := strings.Repeat("a", 200)
	cut := strings.Repeat("a", 119) + "…"
	first, _ := ReadingSourceID("example-blog", long)
	second, _ := ReadingSourceID(" example-blog ", cut)
	if first != second {
		t.Fatalf("%s != %s", first, second)
	}
}

func TestParseIntakeSources(t *testing.T) {
	got := ParseIntakeSources(" daily-reading, ,example-blog,daily-reading,")
	if want := []string{"daily-reading", "example-blog"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
	if got := ParseIntakeSources("  "); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}

func TestSelectIntakeTakesOnlyChosenRecentItemsWithTheirText(t *testing.T) {
	excerpt := intakeItem("example-blog", "excerpt", time.Hour)
	delete(excerpt, "body")
	undated := intakeItem("example-blog", "undated", time.Hour)
	delete(undated, "published")
	selection := SelectIntake([]any{
		intakeItem("other-source", "ignored", time.Hour),
		intakeItem("example-blog", "kept", time.Hour),
		intakeItem("example-blog", "old", MaxIntakeAge+time.Minute),
		excerpt, undated, "not an item",
	}, []string{"example-blog"}, intakeNow)

	if selection.WithoutBody != 1 || selection.OutOfWindow != 2 || selection.OverLimit != 0 {
		t.Fatalf("counts: %+v", selection)
	}
	if len(selection.Documents) != 1 {
		t.Fatalf("documents: %+v", selection.Documents)
	}
	id, _ := ReadingSourceID("example-blog", "kept")
	want := map[string]any{
		"title": "Title kept", "body": "Text of kept", "kind": "article",
		"source_id": id, "source_ref": "https://example.com/kept",
	}
	if !reflect.DeepEqual(selection.Documents[0], want) {
		t.Fatalf("got %+v", selection.Documents[0])
	}
}

func TestSelectIntakeWithNoSourcesTakesNothing(t *testing.T) {
	selection := SelectIntake([]any{intakeItem("example-blog", "kept", time.Hour)}, nil, intakeNow)
	if len(selection.Documents) != 0 {
		t.Fatalf("got %+v", selection)
	}
}

func TestSelectIntakeKeepsTheNewestWhenOverTheLimit(t *testing.T) {
	items := []any{}
	for n := 0; n < MaxIntakeDocuments+3; n++ {
		items = append(items, intakeItem("example-blog", fmt.Sprintf("n%d", n), time.Duration(n)*time.Minute))
	}
	selection := SelectIntake(items, []string{"example-blog"}, intakeNow)
	if len(selection.Documents) != MaxIntakeDocuments || selection.OverLimit != 3 {
		t.Fatalf("documents=%d over=%d", len(selection.Documents), selection.OverLimit)
	}
	if selection.Documents[0]["title"] != "Title n0" {
		t.Fatalf("first: %v", selection.Documents[0]["title"])
	}
}

type intakeService struct {
	payloads []map[string]any
	failAt   int
	changed  bool
}

func (s *intakeService) Dispatch(_ context.Context, eventType string, payload map[string]any) (map[string]any, error) {
	if eventType != "knowledge.ingest" {
		return nil, fmt.Errorf("unexpected %s", eventType)
	}
	s.payloads = append(s.payloads, payload)
	if s.failAt == len(s.payloads) {
		return nil, errors.New("refused")
	}
	return map[string]any{"changed": s.changed}, nil
}

func TestStoreIntakeSurvivesOneRefusalAndCountsChanges(t *testing.T) {
	service := &intakeService{failAt: 1, changed: true}
	documents := SelectIntake([]any{
		intakeItem("example-blog", "a", time.Hour), intakeItem("example-blog", "b", 2*time.Hour),
	}, []string{"example-blog"}, intakeNow).Documents

	outcome, err := New(service).StoreIntake(context.Background(), documents)
	if err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("error: %v", err)
	}
	if outcome != (IntakeOutcome{Stored: 1, Changed: 1, Failed: 1}) {
		t.Fatalf("outcome: %+v", outcome)
	}
}

func TestStoreIntakeOfAnUnchangedReadingChangesNothing(t *testing.T) {
	documents := SelectIntake([]any{intakeItem("example-blog", "a", time.Hour)}, []string{"example-blog"}, intakeNow).Documents
	outcome, err := New(&intakeService{}).StoreIntake(context.Background(), documents)
	if err != nil || outcome != (IntakeOutcome{Stored: 1}) {
		t.Fatalf("outcome=%+v err=%v", outcome, err)
	}
}

func TestStoreIntakeStopsWhenItsTimeIsUp(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	service := &intakeService{}
	documents := SelectIntake([]any{
		intakeItem("example-blog", "a", time.Hour), intakeItem("example-blog", "b", 2*time.Hour),
	}, []string{"example-blog"}, intakeNow).Documents
	outcome, err := New(service).StoreIntake(ctx, documents)
	if !errors.Is(err, context.Canceled) || outcome.Failed != 2 || len(service.payloads) != 0 {
		t.Fatalf("outcome=%+v err=%v calls=%d", outcome, err, len(service.payloads))
	}
}
