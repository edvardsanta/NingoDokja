package memory

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func unit(vector ...float32) []float32 { return normalize(vector) }

func TestInsertIsImmutableAndResolveFirstVerdictWins(t *testing.T) {
	store, _ := newTestStore(t, 0)

	created, err := store.Insert(NewExperience{Ref: "e1", Action: "tag.suggest", Context: "first", Predicted: ptr(0.7), Baseline: ptr(0.5)}, nil, "m")
	if err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	created, err = store.Insert(NewExperience{Ref: "e1", Action: "tag.suggest", Context: "second", Predicted: ptr(0.1), Baseline: ptr(0.9)}, nil, "m")
	if err != nil || created {
		t.Fatalf("a stored ref must be left alone: created=%v err=%v", created, err)
	}

	found, resolved, outcome, err := store.Resolve("e1", "accepted")
	if err != nil || !found || !resolved || outcome != "accepted" {
		t.Fatalf("found=%v resolved=%v outcome=%q err=%v", found, resolved, outcome, err)
	}
	found, resolved, outcome, err = store.Resolve("e1", "replaced")
	if err != nil || !found || resolved || outcome != "accepted" {
		t.Fatalf("the first verdict must stand: found=%v resolved=%v outcome=%q err=%v", found, resolved, outcome, err)
	}
	if found, _, _, _ := store.Resolve("missing", "accepted"); found {
		t.Fatal("an unknown ref must not be found")
	}

	rows, err := store.Resolved("", 10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
	if rows[0].Predicted == nil || *rows[0].Predicted != 0.7 || *rows[0].Baseline != 0.5 {
		t.Fatalf("the prediction stored at decision time must survive: %+v", rows[0])
	}
}

func TestOutcomeCountsSplitByActionAndLeavePendingOut(t *testing.T) {
	store, _ := newTestStore(t, 0)
	for _, row := range []struct{ ref, action, outcome string }{
		{"a1", "tag.suggest", "accepted"}, {"a2", "tag.suggest", "accepted"}, {"a3", "tag.suggest", "replaced"},
		{"a4", "tag.suggest", ""}, {"b1", "other.action", "ignored"},
	} {
		store.Insert(NewExperience{Ref: row.ref, Action: row.action, Context: "x"}, nil, "m")
		if row.outcome != "" {
			store.Resolve(row.ref, row.outcome)
		}
	}

	one, err := store.OutcomeCounts("tag.suggest")
	if err != nil || len(one) != 2 || one["accepted"] != 2 || one["replaced"] != 1 {
		t.Fatalf("counts=%v err=%v", one, err)
	}
	all, _ := store.OutcomeCounts("")
	if all["accepted"] != 2 || all["ignored"] != 1 {
		t.Fatalf("counts across actions=%v", all)
	}
}

func TestUnresolvedExperiencesExpireAsAViewAndALateVerdictStillCounts(t *testing.T) {
	store, c := newTestStore(t, 24*time.Hour)
	store.Insert(NewExperience{Ref: "old", Action: "a", Context: "x"}, unit(1, 0), "m")
	store.Insert(NewExperience{Ref: "late", Action: "a", Context: "y"}, unit(0, 1), "m")

	c.Advance(23 * time.Hour)
	if counts, _ := store.OutcomeCounts("a"); len(counts) != 0 {
		t.Fatalf("nothing has an outcome before the window ends: %v", counts)
	}
	if near, _ := store.Nearest(unit(1, 0), "a", "m", 5); len(near) != 0 {
		t.Fatalf("a pending experience taught nothing yet: %v", near)
	}

	c.Advance(2 * time.Hour)
	if counts, _ := store.OutcomeCounts("a"); counts[ExpiredOutcome] != 2 {
		t.Fatalf("both should read as expired: %v", counts)
	}
	rows, _ := store.Resolved("a", 10)
	if len(rows) != 2 || rows[0].Outcome != ExpiredOutcome {
		t.Fatalf("rows=%+v", rows)
	}
	stats, _ := store.Stats("m")
	if stats.Expired != 2 || stats.Pending != 0 {
		t.Fatalf("stats=%+v", stats)
	}

	// Expiry is a view, not a write: a verdict that finally arrives replaces it.
	if _, resolved, _, _ := store.Resolve("late", "accepted"); !resolved {
		t.Fatal("a late verdict must still be accepted")
	}
	counts, _ := store.OutcomeCounts("a")
	if counts["accepted"] != 1 || counts[ExpiredOutcome] != 1 {
		t.Fatalf("counts=%v", counts)
	}
}

func TestNearestRanksFiltersAndSkipsWhatItCannotCompare(t *testing.T) {
	store, _ := newTestStore(t, 0)
	add := func(ref, action, outcome, model string, vector []float32) {
		store.Insert(NewExperience{Ref: ref, Action: action, Context: ref}, vector, model)
		if outcome != "" {
			store.Resolve(ref, outcome)
		}
	}
	add("close", "a", "accepted", "m", unit(1, 0.1))
	add("middle", "a", "replaced", "m", unit(1, 1))
	add("far", "a", "accepted", "m", unit(0, 1))
	add("other-action", "b", "accepted", "m", unit(1, 0.1))
	add("other-model", "a", "accepted", "old", unit(1, 0.1))
	add("other-dimension", "a", "accepted", "m", unit(1, 0.1, 0))
	add("pending", "a", "", "m", unit(1, 0.1))
	store.Insert(NewExperience{Ref: "no-vector", Action: "a", Context: "z"}, nil, "m")
	store.Resolve("no-vector", "accepted")

	got, err := store.Nearest(unit(1, 0), "a", "m", 10)
	if err != nil {
		t.Fatal(err)
	}
	var refs []string
	for _, n := range got {
		refs = append(refs, n.Ref)
	}
	if len(refs) != 3 || refs[0] != "close" || refs[1] != "middle" || refs[2] != "far" {
		t.Fatalf("expected close, middle, far for action a: %v", refs)
	}
	if got[0].Outcome != "accepted" || got[0].Similarity < 0.99 || got[2].Similarity > 0.01 {
		t.Fatalf("outcomes and similarities: %+v", got)
	}

	all, _ := store.Nearest(unit(1, 0), "", "m", 10)
	if len(all) != 4 {
		t.Fatalf("with no action, the other action's row joins in: %d", len(all))
	}
	if top, _ := store.Nearest(unit(1, 0), "a", "m", 2); len(top) != 2 || top[1].Ref != "middle" {
		t.Fatalf("k must bound the answer: %+v", top)
	}
}

func TestResolvedReturnsTheLatestRowsOldestFirst(t *testing.T) {
	store, _ := newTestStore(t, 0)
	for _, ref := range []string{"r1", "r2", "r3", "r4"} {
		store.Insert(NewExperience{Ref: ref, Action: "a", Context: "x"}, nil, "m")
		store.Resolve(ref, "accepted")
	}
	store.Insert(NewExperience{Ref: "still-pending", Action: "a", Context: "x"}, nil, "m")

	rows, err := store.Resolved("a", 3)
	if err != nil || len(rows) != 3 || rows[0].Ref != "r2" || rows[2].Ref != "r4" {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	if rows[0].Predicted != nil || rows[0].Baseline != nil {
		t.Fatal("an experience recorded without a prediction has no chances")
	}
}

func TestEmbeddingsNeedingAWriteAreFoundAndStored(t *testing.T) {
	store, _ := newTestStore(t, 0)
	store.Insert(NewExperience{Ref: "none", Action: "a", Context: "no vector"}, nil, "m")
	store.Insert(NewExperience{Ref: "old", Action: "a", Context: "old model"}, unit(1, 0), "old-model")
	store.Insert(NewExperience{Ref: "current", Action: "a", Context: "current"}, unit(1, 0), "m")

	pending, err := store.NeedingEmbedding("m", 10)
	if err != nil || len(pending) != 2 {
		t.Fatalf("pending=%v err=%v", pending, err)
	}
	ids := []int64{pending[0].ID, pending[1].ID}
	if err := store.SetEmbeddings(ids, [][]float32{unit(1, 0), unit(0, 1)}, "m"); err != nil {
		t.Fatal(err)
	}
	if left, _ := store.NeedingEmbedding("m", 10); len(left) != 0 {
		t.Fatalf("everything is embedded now: %v", left)
	}
	if stats, _ := store.Stats("m"); stats.Embedded != 3 || stats.NeedsReindex != 0 {
		t.Fatalf("stats=%+v", stats)
	}
}

func TestStoreForgetDeletes(t *testing.T) {
	store, _ := newTestStore(t, 0)
	store.Insert(NewExperience{Ref: "gone", Action: "a", Context: "x"}, nil, "m")
	if deleted, err := store.Forget("gone"); err != nil || !deleted {
		t.Fatalf("deleted=%v err=%v", deleted, err)
	}
	if deleted, _ := store.Forget("gone"); deleted {
		t.Fatal("forgetting twice deletes nothing")
	}
	if exists, _ := store.Exists("gone"); exists {
		t.Fatal("the experience must be gone")
	}
}

func TestOpenStoreCreatesAPrivateFileAndReopensIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "memory.db")

	store, err := OpenStore(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	store.Insert(NewExperience{Ref: "kept", Action: "a", Context: "x"}, nil, "m")
	store.Close()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("the database holds the user's messages and must be private, mode is %o", mode)
	}

	reopened, err := OpenStore(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if exists, _ := reopened.Exists("kept"); !exists {
		t.Fatal("data must survive a reopen, and migrations must be safe to rerun")
	}
}

func TestOpenStoreRefusesADatabaseFromTheFuture(t *testing.T) {
	path := filepath.Join(t.TempDir(), "future.db")
	store, err := OpenStore(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec("PRAGMA user_version = 99"); err != nil {
		t.Fatal(err)
	}
	store.Close()

	if _, err := OpenStore(path, 0); err == nil {
		t.Fatal("a database newer than this build must be refused, not misread")
	}
	if _, err := OpenStore("", 0); err == nil {
		t.Fatal("an empty path must be refused")
	}
}

func TestGetReadsAnExperienceBackWithoutItsContext(t *testing.T) {
	store, c := newTestStore(t, 24*time.Hour)
	store.Insert(NewExperience{Ref: "e1", Action: "tag.suggest", Context: "private words", Detail: "#Label",
		Predicted: ptr(0.7), Baseline: ptr(0.5)}, nil, "m")

	stored, ok, err := store.Get("e1")
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if stored.Action != "tag.suggest" || stored.Detail != "#Label" || stored.Outcome != "" || stored.ResolvedAt != "" ||
		stored.Predicted == nil || *stored.Predicted != 0.7 || *stored.Baseline != 0.5 {
		t.Fatalf("pending: %+v", stored)
	}

	c.Advance(25 * time.Hour)
	if expired, _, _ := store.Get("e1"); expired.Outcome != ExpiredOutcome {
		t.Fatalf("an unresolved experience past the window reads as expired: %+v", expired)
	}

	store.Resolve("e1", "accepted")
	if resolved, _, _ := store.Get("e1"); resolved.Outcome != "accepted" || resolved.ResolvedAt == "" {
		t.Fatalf("resolved: %+v", resolved)
	}
	if _, ok, err := store.Get("missing"); ok || err != nil {
		t.Fatalf("an unknown ref is not found and not an error: ok=%v err=%v", ok, err)
	}
}
