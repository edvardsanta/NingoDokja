package memory

import (
	"context"
	"strings"
	"testing"
	"time"
)

func dispatch(t *testing.T, s *Service, eventType string, payload map[string]any) map[string]any {
	t.Helper()
	result, err := s.Dispatch(context.Background(), eventType, payload)
	if err != nil {
		t.Fatalf("%s: %v", eventType, err)
	}
	return result
}

func record(t *testing.T, s *Service, ref, action, content string, outcome string) {
	t.Helper()
	dispatch(t, s, "memory.record", map[string]any{"ref": ref, "action": action, "context": content})
	if outcome != "" {
		dispatch(t, s, "memory.resolve", map[string]any{"ref": ref, "outcome": outcome})
	}
}

func TestRecordEmbedsOnceAndLeavesAStoredRefAlone(t *testing.T) {
	service, embedder, _ := newTestService(t, 0)

	first := dispatch(t, service, "memory.record", map[string]any{
		"ref": "e1", "action": "tag.suggest", "context": "quarterly report on rates", "predicted_p": 0.7, "baseline_p": 0.5,
	})
	if first["created"] != true || first["embedded"] != true || first["degraded"] != false {
		t.Fatalf("first=%v", first)
	}
	callsAfterFirst := embedder.calls

	again := dispatch(t, service, "memory.record", map[string]any{
		"ref": "e1", "action": "tag.suggest", "context": "something else entirely", "predicted_p": 0.1, "baseline_p": 0.9,
	})
	if again["created"] != false {
		t.Fatalf("again=%v", again)
	}
	if embedder.calls != callsAfterFirst {
		t.Fatal("a ref that is already stored must not be embedded again")
	}

	dispatch(t, service, "memory.resolve", map[string]any{"ref": "e1", "outcome": "accepted"})
	rows := dispatch(t, service, "memory.resolved", map[string]any{})["experiences"].([]any)
	row := rows[0].(map[string]any)
	if row["predicted_p"] != 0.7 || row["baseline_p"] != 0.5 {
		t.Fatalf("the first prediction must be the one kept: %v", row)
	}
}

func TestRecordWhileTheEmbedderIsDownDegradesAndReindexCatchesUp(t *testing.T) {
	service, embedder, _ := newTestService(t, 0)
	embedder.setDown(true)

	result := dispatch(t, service, "memory.record", map[string]any{"ref": "e1", "action": "a", "context": "words about rates"})
	if result["created"] != true || result["embedded"] != false || result["degraded"] != true || result["reason"] == nil {
		t.Fatalf("recording must still work, just without a vector: %v", result)
	}
	if status := dispatch(t, service, "memory.status", nil); status["needs_reindex"] != 1 || status["degraded"] != true {
		t.Fatalf("status=%v", status)
	}
	if _, err := service.Dispatch(context.Background(), "memory.reindex", map[string]any{}); err == nil {
		t.Fatal("an explicit reindex with the embedder down should say so")
	}

	embedder.setDown(false)
	done := dispatch(t, service, "memory.reindex", map[string]any{})
	if done["embedded"] != 1 || done["remaining"] != 0 {
		t.Fatalf("done=%v", done)
	}
	if status := dispatch(t, service, "memory.status", nil); status["needs_reindex"] != 0 || status["degraded"] != false {
		t.Fatalf("status=%v", status)
	}
}

func TestReindexWorksInBoundedBatches(t *testing.T) {
	service, embedder, _ := newTestService(t, 0)
	embedder.setDown(true)
	for _, ref := range []string{"e1", "e2", "e3"} {
		record(t, service, ref, "a", "text "+ref, "")
	}
	embedder.setDown(false)

	first := dispatch(t, service, "memory.reindex", map[string]any{"limit": 2})
	if first["embedded"] != 2 || first["remaining"] != 1 {
		t.Fatalf("first=%v", first)
	}
	second := dispatch(t, service, "memory.reindex", map[string]any{"limit": 2})
	if second["embedded"] != 1 || second["remaining"] != 0 {
		t.Fatalf("second=%v", second)
	}
}

func TestRecallFindsTheClosestResolvedExperiencesWithTheirOutcomes(t *testing.T) {
	service, _, _ := newTestService(t, 0)
	record(t, service, "rates", "tag.suggest", "central bank raises interest rates again", "accepted")
	record(t, service, "cake", "tag.suggest", "chocolate cake recipe with carrots", "replaced")
	record(t, service, "rates2", "tag.suggest", "interest rates and inflation outlook", "accepted")
	record(t, service, "pending", "tag.suggest", "central bank interest rates pending", "")
	record(t, service, "elsewhere", "other.action", "central bank interest rates", "accepted")

	result := dispatch(t, service, "memory.recall", map[string]any{
		"context": "the central bank and interest rates", "action": "tag.suggest", "k": 3,
	})

	neighbors := result["neighbors"].([]any)
	if len(neighbors) != 3 {
		t.Fatalf("neighbors=%v", neighbors)
	}
	first := neighbors[0].(map[string]any)
	if first["ref"] != "rates" || first["outcome"] != "accepted" || first["action"] != "tag.suggest" {
		t.Fatalf("closest=%v", first)
	}
	for _, item := range neighbors {
		ref := item.(map[string]any)["ref"]
		if ref == "pending" || ref == "elsewhere" {
			t.Fatalf("a pending experience or another action's must not be a neighbour: %v", ref)
		}
	}
	if neighbors[2].(map[string]any)["ref"] != "cake" {
		t.Fatalf("the unrelated experience should rank last: %v", neighbors)
	}
	outcomes := result["outcomes"].(map[string]any)
	if outcomes["accepted"] != 2 || outcomes["replaced"] != 1 || result["degraded"] != false {
		t.Fatalf("outcomes=%v degraded=%v", outcomes, result["degraded"])
	}
}

func TestRecallWithoutTheEmbedderStillGivesTheCountsForABaseline(t *testing.T) {
	service, embedder, _ := newTestService(t, 0)
	record(t, service, "e1", "a", "some words here", "accepted")
	record(t, service, "e2", "a", "other words there", "replaced")
	embedder.setDown(true)

	result := dispatch(t, service, "memory.recall", map[string]any{"context": "words", "action": "a"})

	if result["degraded"] != true || result["reason"] == nil || len(result["neighbors"].([]any)) != 0 {
		t.Fatalf("result=%v", result)
	}
	if outcomes := result["outcomes"].(map[string]any); outcomes["accepted"] != 1 || outcomes["replaced"] != 1 {
		t.Fatalf("the counts are still needed for the baseline: %v", outcomes)
	}
}

func TestServiceWithEmbeddingsSwitchedOffStillStoresAndCounts(t *testing.T) {
	store, _ := newTestStore(t, 0)
	service := NewService(store, nil, "m")

	result := dispatch(t, service, "memory.record", map[string]any{"ref": "e1", "action": "a", "context": "words"})
	if result["created"] != true || result["embedded"] != false || result["degraded"] != true {
		t.Fatalf("result=%v", result)
	}
	dispatch(t, service, "memory.resolve", map[string]any{"ref": "e1", "outcome": "accepted"})
	recalled := dispatch(t, service, "memory.recall", map[string]any{"context": "words", "action": "a"})
	if recalled["degraded"] != true || recalled["outcomes"].(map[string]any)["accepted"] != 1 {
		t.Fatalf("recalled=%v", recalled)
	}
	if status := dispatch(t, service, "memory.status", nil); status["embeddings"] != false || status["degraded"] != true {
		t.Fatalf("status=%v", status)
	}
	if _, err := service.Dispatch(context.Background(), "memory.reindex", nil); err == nil || !strings.Contains(err.Error(), "switched off") {
		t.Fatalf("reindex needs an embedder: %v", err)
	}
}

func TestResolveReportsUnknownRefsAndKeepsTheFirstVerdict(t *testing.T) {
	service, _, _ := newTestService(t, 0)
	record(t, service, "e1", "a", "words", "")

	if missing := dispatch(t, service, "memory.resolve", map[string]any{"ref": "nope", "outcome": "accepted"}); missing["found"] != false {
		t.Fatalf("missing=%v", missing)
	}
	first := dispatch(t, service, "memory.resolve", map[string]any{"ref": "e1", "outcome": "accepted"})
	second := dispatch(t, service, "memory.resolve", map[string]any{"ref": "e1", "outcome": "replaced"})
	if first["resolved"] != true || second["resolved"] != false || second["outcome"] != "accepted" {
		t.Fatalf("first=%v second=%v", first, second)
	}
}

func TestForgetAndStatus(t *testing.T) {
	service, _, _ := newTestService(t, 24*time.Hour)
	record(t, service, "e1", "a", "words", "accepted")
	record(t, service, "e2", "a", "more words", "")

	status := dispatch(t, service, "memory.status", nil)
	if status["experiences"] != 2 || status["resolved"] != 1 || status["pending"] != 1 || status["embedded"] != 2 ||
		status["embed_model"] != "test-model" || status["embedder_reachable"] != true {
		t.Fatalf("status=%v", status)
	}

	if forgotten := dispatch(t, service, "memory.forget", map[string]any{"ref": "e1"}); forgotten["deleted"] != true {
		t.Fatalf("forgotten=%v", forgotten)
	}
	if forgotten := dispatch(t, service, "memory.forget", map[string]any{"ref": "e1"}); forgotten["deleted"] != false {
		t.Fatalf("forgotten=%v", forgotten)
	}
}

func TestDispatchRejectsMalformedRequests(t *testing.T) {
	service, _, _ := newTestService(t, 0)
	long := strings.Repeat("x", maxContextRunes+1)
	cases := []struct {
		eventType string
		payload   map[string]any
		mention   string
	}{
		{"memory.record", map[string]any{"action": "a", "context": "c"}, "ref"},
		{"memory.record", map[string]any{"ref": "r", "action": "Bad Name", "context": "c"}, "action"},
		{"memory.record", map[string]any{"ref": "r", "action": "a"}, "context"},
		{"memory.record", map[string]any{"ref": "r", "action": "a", "context": long}, "context"},
		{"memory.record", map[string]any{"ref": "bad\nref", "action": "a", "context": "c"}, "ref"},
		{"memory.record", map[string]any{"ref": "r", "action": "a", "context": "c", "detail": strings.Repeat("d", maxDetailRunes+1)}, "detail"},
		{"memory.record", map[string]any{"ref": "r", "action": "a", "context": "c", "detail": "bad\x00detail"}, "detail"},
		{"memory.record", map[string]any{"ref": "r", "action": "a", "context": "c", "predicted_p": 0.5}, "go together"},
		{"memory.record", map[string]any{"ref": "r", "action": "a", "context": "c", "predicted_p": 2.0, "baseline_p": 0.5}, "between 0 and 1"},
		{"memory.record", map[string]any{"ref": "r", "action": "a", "context": "c", "matched_score": 1.5}, "matched_score"},
		{"memory.record", map[string]any{"ref": "r", "action": "a", "context": "c", "matched_context": strings.Repeat("m", maxMatchedRunes+1)}, "matched_context"},
		{"memory.resolve", map[string]any{"ref": "r", "outcome": "Not A Word"}, "outcome"},
		{"memory.resolve", map[string]any{"outcome": "accepted"}, "ref"},
		{"memory.recall", map[string]any{"action": "a"}, "context"},
		{"memory.recall", map[string]any{"context": "c", "k": 0}, "k must"},
		{"memory.recall", map[string]any{"context": "c", "action": "Bad Name"}, "action"},
		{"memory.resolved", map[string]any{"limit": 0}, "limit"},
		{"memory.get", nil, "ref"},
		{"memory.forget", nil, "ref"},
		{"memory.reindex", map[string]any{"limit": maxReindex + 1}, "limit"},
		{"memory.unknown", nil, "unsupported"},
	}
	for _, tc := range cases {
		_, err := service.Dispatch(context.Background(), tc.eventType, tc.payload)
		if err == nil || !strings.Contains(err.Error(), tc.mention) {
			t.Fatalf("%s %v: expected an error mentioning %q, got %v", tc.eventType, tc.payload, tc.mention, err)
		}
	}
	if status := dispatch(t, service, "memory.status", nil); status["experiences"] != 0 {
		t.Fatalf("a rejected request must store nothing: %v", status)
	}
}

func TestGetReturnsWhatTheBotDidButNeverTheContext(t *testing.T) {
	service, _, _ := newTestService(t, 0)
	dispatch(t, service, "memory.record", map[string]any{
		"ref": "e1", "action": "tag.suggest", "context": "private words", "detail": "#Label", "predicted_p": 0.7, "baseline_p": 0.5,
	})

	pending := dispatch(t, service, "memory.get", map[string]any{"ref": "e1"})
	if pending["found"] != true || pending["detail"] != "#Label" || pending["outcome"] != "" ||
		pending["predicted_p"] != 0.7 || pending["baseline_p"] != 0.5 || pending["action"] != "tag.suggest" {
		t.Fatalf("pending=%v", pending)
	}
	for key, value := range pending {
		if text, ok := value.(string); ok && strings.Contains(text, "private") {
			t.Fatalf("the context is the user's own text and must not come back (%s=%q)", key, text)
		}
	}

	dispatch(t, service, "memory.resolve", map[string]any{"ref": "e1", "outcome": "replaced"})
	if resolved := dispatch(t, service, "memory.get", map[string]any{"ref": "e1"}); resolved["outcome"] != "replaced" || resolved["resolved_at"] == "" {
		t.Fatalf("resolved=%v", resolved)
	}
	if missing := dispatch(t, service, "memory.get", map[string]any{"ref": "nope"}); missing["found"] != false {
		t.Fatalf("missing=%v", missing)
	}
}

func TestListReturnsTheNewestFirstAndNeverTheContextUnlessAsked(t *testing.T) {
	service, _, _ := newTestService(t, 0)
	for _, ref := range []string{"e1", "e2", "e3"} {
		dispatch(t, service, "memory.record", map[string]any{
			"ref": ref, "action": "tag.suggest", "context": "private words " + ref, "detail": "#" + ref, "predicted_p": 0.7, "baseline_p": 0.5,
		})
	}
	dispatch(t, service, "memory.resolve", map[string]any{"ref": "e2", "outcome": "accepted"})

	plain := dispatch(t, service, "memory.list", map[string]any{})
	rows := plain["experiences"].([]any)
	if plain["total"] != 3 || plain["limit"] != defaultList || plain["offset"] != 0 || len(rows) != 3 {
		t.Fatalf("plain=%v", plain)
	}
	first := rows[0].(map[string]any)
	if first["ref"] != "e3" || first["detail"] != "#e3" || first["outcome"] != "" || first["predicted_p"] != 0.7 || first["baseline_p"] != 0.5 {
		t.Fatalf("newest first, with what the bot did: %v", first)
	}
	if _, has := first["context_snippet"]; has {
		t.Fatalf("a snippet is opt-in: %v", first)
	}
	if rows[1].(map[string]any)["outcome"] != "accepted" {
		t.Fatalf("rows=%v", rows)
	}
	for _, raw := range rows {
		for key, value := range raw.(map[string]any) {
			if text, ok := value.(string); ok && strings.Contains(text, "private") {
				t.Fatalf("the context must not come back unless asked (%s=%q)", key, text)
			}
		}
	}

	asked := dispatch(t, service, "memory.list", map[string]any{"include_context": true, "limit": 1})
	only := asked["experiences"].([]any)[0].(map[string]any)
	if only["context_snippet"] != "private words e3" || asked["total"] != 3 || asked["limit"] != 1 {
		t.Fatalf("asked=%v", asked)
	}
}

func TestListFiltersAndPagesAndKeepsTheSnippetShort(t *testing.T) {
	service, _, c := newTestService(t, 24*time.Hour)
	long := strings.Repeat("word ", 100)
	dispatch(t, service, "memory.record", map[string]any{"ref": "old", "action": "a", "context": long})
	c.Advance(25 * time.Hour)
	dispatch(t, service, "memory.record", map[string]any{"ref": "waiting", "action": "a", "context": "x"})
	dispatch(t, service, "memory.record", map[string]any{"ref": "other", "action": "b", "context": "x"})

	if pending := dispatch(t, service, "memory.list", map[string]any{"state": "pending"}); pending["total"] != 2 {
		t.Fatalf("pending=%v", pending)
	}
	resolved := dispatch(t, service, "memory.list", map[string]any{"state": "resolved", "include_context": true})
	if resolved["total"] != 1 {
		t.Fatalf("an expired experience lists as resolved: %v", resolved)
	}
	row := resolved["experiences"].([]any)[0].(map[string]any)
	if row["outcome"] != "expired" || len([]rune(row["context_snippet"].(string))) > SnippetRunes {
		t.Fatalf("row=%v", row)
	}
	if byAction := dispatch(t, service, "memory.list", map[string]any{"action": "b", "state": "all"}); byAction["total"] != 1 {
		t.Fatalf("byAction=%v", byAction)
	}
	page := dispatch(t, service, "memory.list", map[string]any{"limit": 1, "offset": 2})
	if len(page["experiences"].([]any)) != 1 || page["experiences"].([]any)[0].(map[string]any)["ref"] != "old" || page["total"] != 3 {
		t.Fatalf("page=%v", page)
	}
}

func TestRecallCarriesWhatTheBotDidAndOnlyAsksForSnippetsWhenTold(t *testing.T) {
	service, _, _ := newTestService(t, 0)
	dispatch(t, service, "memory.record", map[string]any{"ref": "e1", "action": "a", "context": "central bank rates outlook", "detail": "#Rates"})
	dispatch(t, service, "memory.resolve", map[string]any{"ref": "e1", "outcome": "accepted"})

	plain := dispatch(t, service, "memory.recall", map[string]any{"context": "central bank rates", "action": "a"})
	first := plain["neighbors"].([]any)[0].(map[string]any)
	if first["detail"] != "#Rates" {
		t.Fatalf("first=%v", first)
	}
	if _, has := first["context_snippet"]; has {
		t.Fatalf("a snippet is opt-in: %v", first)
	}

	asked := dispatch(t, service, "memory.recall", map[string]any{"context": "central bank rates", "action": "a", "include_context": true})
	if got := asked["neighbors"].([]any)[0].(map[string]any)["context_snippet"]; got != "central bank rates outlook" {
		t.Fatalf("snippet=%v", got)
	}
}

func TestListRejectsMalformedRequests(t *testing.T) {
	service, _, _ := newTestService(t, 0)
	for name, payload := range map[string]map[string]any{
		"unknown state":  {"state": "everything"},
		"zero limit":     {"limit": 0},
		"huge limit":     {"limit": maxList + 1},
		"negative start": {"offset": -1},
		"bad action":     {"action": "Bad Name"},
	} {
		if _, err := service.Dispatch(context.Background(), "memory.list", payload); err == nil {
			t.Fatalf("%s: expected an error", name)
		}
	}
}

func TestRecordKeepsWhatTheChoiceRestedOnAndShowsItsTextOnlyWhenAsked(t *testing.T) {
	service, _, _ := newTestService(t, 0)
	dispatch(t, service, "memory.record", map[string]any{
		"ref": "with", "action": "tag.suggest", "context": "private words", "detail": "#Label",
		"matched_score": 0.83, "matched_context": "similar words " + strings.Repeat("y", 300)[:150],
	})
	dispatch(t, service, "memory.record", map[string]any{
		"ref": "without", "action": "tag.suggest", "context": "other private words", "detail": "#Label",
	})

	got := dispatch(t, service, "memory.get", map[string]any{"ref": "with"})
	if got["matched_score"] != 0.83 {
		t.Fatalf("get=%v", got)
	}
	if none := dispatch(t, service, "memory.get", map[string]any{"ref": "without"}); none["matched_score"] != nil {
		t.Fatalf("an experience recorded without a match has none: %v", none)
	}

	plain := dispatch(t, service, "memory.list", map[string]any{})
	for _, raw := range plain["experiences"].([]any) {
		for key, value := range raw.(map[string]any) {
			if text, ok := value.(string); ok && strings.Contains(text, "similar") {
				t.Fatalf("the earlier example is the user's text and must not come back unless asked (%s=%q)", key, text)
			}
		}
	}
	scores := map[any]any{}
	for _, raw := range plain["experiences"].([]any) {
		row := raw.(map[string]any)
		scores[row["ref"]] = row["matched_score"]
	}
	if scores["with"] != 0.83 || scores["without"] != nil {
		t.Fatalf("the score is not text and comes with every listing: %v", scores)
	}

	asked := dispatch(t, service, "memory.list", map[string]any{"include_context": true})
	for _, raw := range asked["experiences"].([]any) {
		row := raw.(map[string]any)
		snippet, has := row["matched_snippet"].(string)
		switch row["ref"] {
		case "with":
			if !has || !strings.HasPrefix(snippet, "similar words ") || len([]rune(snippet)) != SnippetRunes {
				t.Fatalf("with=%v", row)
			}
		case "without":
			if has {
				t.Fatalf("nothing to show for an experience without a match: %v", row)
			}
		}
	}
}
