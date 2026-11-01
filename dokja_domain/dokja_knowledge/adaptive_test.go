package knowledge

import (
	"context"
	"strings"
	"testing"
)

type searchService struct {
	hits      []any
	documents map[string]string
	reads     []string
}

func (s *searchService) Dispatch(_ context.Context, event string, payload map[string]any) (map[string]any, error) {
	if event == "knowledge.search" {
		return map[string]any{"hits": s.hits}, nil
	}
	id, _ := payload["source_id"].(string)
	s.reads = append(s.reads, id)
	text, ok := s.documents[id]
	if !ok {
		return map[string]any{"documents": []any{}}, nil
	}
	return map[string]any{"documents": []any{map[string]any{"source_id": id, "source_ref": "file:" + id, "title": id, "text": text}}}, nil
}

func hit(id string, relevant bool) map[string]any {
	return map[string]any{"source_id": id, "kind": "note", "relevant": relevant}
}

func TestFindSourcesKeepsRankAndDropsWhatItCannotUse(t *testing.T) {
	service := &searchService{
		hits:      []any{hit("a", true), hit("a", true), hit("loaded", true), hit("weak", false), hit("gone", true), hit("big", true), hit("b", true), hit("c", true), hit("d", true), map[string]any{"source_id": "tag", "kind": HashtagKnowledgeKind, "relevant": true}},
		documents: map[string]string{"a": "alpha", "b": "beta", "c": "gamma", "d": "delta", "big": strings.Repeat("x", 500), "loaded": "seen", "weak": "weak", "tag": "tag"},
	}
	found, err := New(service).FindApplicationSources(t.Context(), "goal", map[string]bool{"loaded": true}, 3, 100)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, source := range found.Sources {
		ids = append(ids, source["source_id"].(string))
	}
	if strings.Join(ids, ",") != "a,b,c" || strings.Join(found.Skipped, ",") != "gone,big" {
		t.Fatal(ids, found.Skipped, service.reads)
	}
}

func TestAdaptiveGapIsARuleOverTheAssessment(t *testing.T) {
	missing := map[string]any{"disposition": "propose", "summary": "fallback", "assessment": map[string]any{
		"applicability": "needs_evidence", "missing_prerequisites": []any{"open"},
		"claims": []any{map[string]any{"id": "open", "statement": "West retention is unknown"}, map[string]any{"id": "done", "statement": "East is settled"}},
	}}
	if query, gap := AdaptiveGap(missing); !gap || query != "West retention is unknown" {
		t.Fatal(query, gap)
	}
	if query, gap := AdaptiveGap(map[string]any{"disposition": "clarify", "summary": "Key age is missing"}); !gap || query != "Key age is missing" {
		t.Fatal(query, gap)
	}
	for name, proposal := range map[string]map[string]any{
		"abstained":   {"disposition": "abstain", "summary": "No"},
		"conditional": {"disposition": "propose", "assessment": map[string]any{"applicability": "conditional"}},
		"conflicted":  {"disposition": "propose", "assessment": map[string]any{"applicability": "conflicted"}},
		"blocked":     {"disposition": "propose", "assessment": map[string]any{"applicability": "blocked"}},
		"no summary":  {"disposition": "clarify", "summary": " "},
	} {
		if query, gap := AdaptiveGap(proposal); gap {
			t.Fatal(name, query)
		}
	}
	long, gap := AdaptiveGap(map[string]any{"disposition": "clarify", "summary": strings.Repeat("é", 800)})
	if !gap || len(long) > maxAdaptiveQueryBytes || !strings.HasSuffix(long, "é") {
		t.Fatal(len(long), gap)
	}
}

func TestAdaptiveRequestTakesNoSources(t *testing.T) {
	valid := map[string]any{"contract": AdaptiveContract, "goal": "Decide"}
	if _, err := ParseAdaptiveRequest(valid); err != nil {
		t.Fatal(err)
	}
	for name, payload := range map[string]map[string]any{
		"sources":  {"contract": AdaptiveContract, "goal": "Decide", "sources": []any{}},
		"contract": {"contract": ApplicationContract, "goal": "Decide"},
		"goal":     {"contract": AdaptiveContract, "goal": " "},
		"long":     {"contract": AdaptiveContract, "goal": strings.Repeat("g", 2001)},
	} {
		if _, err := ParseAdaptiveRequest(payload); err == nil {
			t.Fatal(name)
		}
	}
}

func TestRevisionIsBoundToItsAttempt(t *testing.T) {
	application := map[string]any{"contract": ApplicationContract}
	prompt, err := BuildProposalRevisionPrompt("task", 2, application)
	if err != nil || !strings.Contains(prompt.Data, `"revision":2`) {
		t.Fatal(prompt.Data, err)
	}
	if _, err := BuildProposalRevisionPrompt("task", AdaptiveMaxProposals+1, application); err == nil {
		t.Fatal("a third revision was built")
	}
	stale := `{"contract":"knowledge.application.proposal.v1","task_id":"task","revision":1,"disposition":"abstain","summary":"No","proposal":null}`
	if _, err := DecodeProposalRevision("task", 2, ApplicationRequest{}, application, stale); err == nil {
		t.Fatal("a stale reply was accepted")
	}
	if _, err := DecodeProposalRevision("task", 1, ApplicationRequest{}, application, stale); err != nil {
		t.Fatal(err)
	}
}
