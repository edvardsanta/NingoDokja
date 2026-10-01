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
