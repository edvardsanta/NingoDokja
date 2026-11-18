package handlers

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	knowledgedomain "read_books/dokja_domain/dokja_knowledge"
	"read_books/dokja_store"
	"read_books/internal/core"
)

// proposalsKnowledgeStub records ingests and serves a canned knowledge.list, so
// current-record lookups have something to find.
type proposalsKnowledgeStub struct {
	payloads  []map[string]any
	documents []any
}

func (s *proposalsKnowledgeStub) Dispatch(_ context.Context, eventType string, payload map[string]any) (map[string]any, error) {
	switch eventType {
	case "knowledge.ingest":
		s.payloads = append(s.payloads, payload)
		return map[string]any{"source_id": payload["source_id"], "changed": true, "metadata": payload["metadata"]}, nil
	case "knowledge.list":
		return map[string]any{"documents": s.documents, "total": len(s.documents)}, nil
	default:
		return map[string]any{"ok": true}, nil
	}
}

func newProposalsHandler(t *testing.T, knowledge *proposalsKnowledgeStub, web *webReadingService, reader *fakeReader) *KnowledgeProposalsHandler {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "dokja.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return NewKnowledgeProposalsHandler(db, knowledge, web, reader)
}

func TestProposalApproveStoresProvenanceAndRemovesIt(t *testing.T) {
	knowledge := &proposalsKnowledgeStub{}
	h := newProposalsHandler(t, knowledge, &webReadingService{}, &fakeReader{answer: readAnswer("bounded text")})

	id, duplicate, err := h.Propose(context.Background(), "web_article", map[string]any{"url": "https://example.com/news"},
		knowledgedomain.ProposalSource{ChannelID: "memes", MessageID: "m1", Permalink: "https://example.com/ch/1/m1"}, "sam <1>", "sam <1>")
	if err != nil || duplicate {
		t.Fatalf("proposal was not held: %v %v", id, err)
	}
	again, duplicate, err := h.Propose(context.Background(), "web_article", map[string]any{"url": "https://example.com/news"},
		knowledgedomain.ProposalSource{}, "sam <1>", "sam <1>")
	if err != nil || !duplicate || again != id {
		t.Fatalf("a repeated submission did not reopen the same question: %v %v", again, err)
	}

	queued, err := h.Handle(context.Background(), chatEvent("unused"), coreStep("list-knowledge-proposals"))
	if err != nil {
		t.Fatal(err)
	}
	proposals := queued["proposals"].([]map[string]any)
	if len(proposals) != 1 || proposals[0]["proposed_by"] != "sam <1>" {
		t.Fatalf("the queue lost the proposal: %v", queued)
	}
	if _, exists := proposals[0]["current"]; exists {
		t.Fatalf("nothing is stored yet, the queue must not show a current record: %v", proposals[0])
	}

	approved, err := h.Handle(context.Background(), proposalsQueueEvent(map[string]any{"proposal_id": id}), coreStep("approve-knowledge-proposal"))
	if err != nil {
		t.Fatal(err)
	}
	if approved["approved"] != true || approved["stored"] != 1 {
		t.Fatalf("approval did not store the article: %v", approved)
	}
	if len(knowledge.payloads) != 1 {
		t.Fatalf("no ingest happened: %v", knowledge.payloads)
	}
	payload := knowledge.payloads[0]
	if payload["source_id"] != webArticleIdentity("https://example.com/news") || payload["kind"] != "article" {
		t.Fatalf("the stored article lost its identity: %v", payload)
	}
	metadata := payload["metadata"].(map[string]any)
	provenance := metadata["provenance"].(knowledgedomain.Provenance)
	if provenance.Origin != knowledgedomain.OriginGroup || provenance.ProposedBy != "sam <1>" || provenance.Channel != "memes" {
		t.Fatalf("the stored article lost its provenance: %+v", provenance)
	}

	gone, err := h.Handle(context.Background(), chatEvent("unused"), coreStep("list-knowledge-proposals"))
	if err != nil || len(gone["proposals"].([]map[string]any)) != 0 {
		t.Fatalf("an approved proposal must be gone: %v", gone)
	}
}

func TestProposalApprovalRefusalLeavesItPending(t *testing.T) {
	knowledge := &proposalsKnowledgeStub{}
	blocked := &webReadingService{fail: true, failureReason: "web_blocked: private diagnostic"}
	h := newProposalsHandler(t, knowledge, blocked, &fakeReader{})
	id, _, err := h.Propose(context.Background(), "web_article", map[string]any{"url": "https://example.com/news"}, knowledgedomain.ProposalSource{}, "sam", "sam")
	if err != nil {
		t.Fatal(err)
	}
	answer, err := h.Handle(context.Background(), proposalsQueueEvent(map[string]any{"proposal_id": id}), coreStep("approve-knowledge-proposal"))
	if err != nil {
		t.Fatal(err)
	}
	if answer["approved"] != false || answer["reason"] != "blocked" {
		t.Fatalf("a refusal must keep the proposal pending with its fixed reason: %v", answer)
	}
	if strings.Contains(answer["reason"].(string), "private") {
		t.Fatal("the refusal leaked transport detail")
	}
	queued, _ := h.Handle(context.Background(), chatEvent("unused"), coreStep("list-knowledge-proposals"))
	if len(queued["proposals"].([]map[string]any)) != 1 {
		t.Fatal("a refused approval removed the proposal")
	}
}

func TestProposalApprovalReportsWhatAnOwnerRecordItReplaces(t *testing.T) {
	knowledge := &proposalsKnowledgeStub{documents: []any{
		map[string]any{"source_id": webArticleIdentity("https://example.com/news"), "title": "The law",
			"metadata": map[string]any{"provenance": map[string]any{"origin": "owner"}}},
	}}
	h := newProposalsHandler(t, knowledge, &webReadingService{}, &fakeReader{})
	id, _, err := h.Propose(context.Background(), "web_article", map[string]any{"url": "https://example.com/news"}, knowledgedomain.ProposalSource{}, "sam", "sam")
	if err != nil {
		t.Fatal(err)
	}
	queued, err := h.Handle(context.Background(), chatEvent("unused"), coreStep("list-knowledge-proposals"))
	if err != nil {
		t.Fatal(err)
	}
	current := queued["proposals"].([]map[string]any)[0]["current"].(map[string]any)
	if current["origin"] != "owner" {
		t.Fatalf("the queue must show the owner record next to the proposal: %v", current)
	}
	approved, err := h.Handle(context.Background(), proposalsQueueEvent(map[string]any{"proposal_id": id}), coreStep("approve-knowledge-proposal"))
	if err != nil || approved["replaced_origin"] != "owner" {
		t.Fatalf("an explicit replacement of an owner record must say so: %v", approved)
	}
}

func TestDocumentProposalApprovesThroughTheReadingHost(t *testing.T) {
	knowledge := &proposalsKnowledgeStub{}
	h := newProposalsHandler(t, knowledge, &webReadingService{}, &fakeReader{answer: readAnswer("The experiment showed a stable improvement.")})
	id, _, err := h.Propose(context.Background(), "document",
		map[string]any{"filename": "memo.pdf", "content_b64": "ZmlsZQ=="},
		knowledgedomain.ProposalSource{ChannelID: "notes"}, "tiago", "tiago")
	if err != nil {
		t.Fatal(err)
	}
	approved, err := h.Handle(context.Background(), proposalsQueueEvent(map[string]any{"proposal_id": id}), coreStep("approve-knowledge-proposal"))
	if err != nil || approved["approved"] != true {
		t.Fatalf("document approval failed: %v", approved)
	}
	payload := knowledge.payloads[0]
	if payload["source_id"] != "file:memo.pdf" {
		t.Fatalf("the document lost its file identity: %v", payload["source_id"])
	}
	metadata := payload["metadata"].(map[string]any)
	if metadata["provenance"].(knowledgedomain.Provenance).Origin != knowledgedomain.OriginGroup {
		t.Fatalf("the document lost its provenance: %v", metadata)
	}
}

func TestProposalsRejectDeletesThemAndEditsAreRefusedForUneditableKinds(t *testing.T) {
	knowledge := &proposalsKnowledgeStub{}
	h := newProposalsHandler(t, knowledge, &webReadingService{}, &fakeReader{answer: readAnswer("text")})
	id, _, _ := h.Propose(context.Background(), "web_article", map[string]any{"url": "https://example.com/news"}, knowledgedomain.ProposalSource{}, "sam", "sam")
	rejected, err := h.Handle(context.Background(), proposalsQueueEvent(map[string]any{"proposal_id": id}), coreStep("reject-knowledge-proposal"))
	if err != nil || rejected["rejected"] != true {
		t.Fatalf("rejection failed: %v", rejected)
	}
	queued, _ := h.Handle(context.Background(), chatEvent("unused"), coreStep("list-knowledge-proposals"))
	if len(queued["proposals"].([]map[string]any)) != 0 {
		t.Fatal("a rejected proposal was kept")
	}
	if len(knowledge.payloads) != 0 {
		t.Fatal("a rejection stored something")
	}

	id, _, _ = h.Propose(context.Background(), "web_article", map[string]any{"url": "https://example.com/other"}, knowledgedomain.ProposalSource{}, "sam", "sam")
	edited, err := h.Handle(context.Background(), proposalsQueueEvent(map[string]any{"proposal_id": id, "edit": map[string]any{"url": "https://example.com/changed"}}), coreStep("approve-knowledge-proposal"))
	if err != nil || edited["approved"] != false || edited["reason"] != "uneditable" {
		t.Fatalf("an edit of an uneditable kind must refuse cleanly: %v", edited)
	}
}

func TestProposalsBoundOnePersonsPendingSubmissions(t *testing.T) {
	knowledge := &proposalsKnowledgeStub{}
	h := newProposalsHandler(t, knowledge, &webReadingService{}, &fakeReader{})
	for i := 0; i < maxProposalsPerProposer; i++ {
		if _, _, err := h.Propose(context.Background(), "web_article", map[string]any{"url": fmt.Sprintf("https://example.com/n%d", i)}, knowledgedomain.ProposalSource{}, "sam", "sam"); err != nil {
			t.Fatalf("proposal %d was refused: %v", i, err)
		}
	}
	if _, _, err := h.Propose(context.Background(), "web_article", map[string]any{"url": "https://example.com/overflow"}, knowledgedomain.ProposalSource{}, "sam", "sam"); err == nil {
		t.Fatal("the per-proposer bound did not hold")
	}
	if _, _, err := h.Propose(context.Background(), "web_article", map[string]any{"url": "https://example.com/overflow"}, knowledgedomain.ProposalSource{}, "tiago", "tiago"); err != nil {
		t.Fatalf("the bound must be per proposer: %v", err)
	}
}

func coreStep(action string) core.WorkflowStep {
	return core.WorkflowStep{Domain: core.DomainKnowledge, Action: action}
}

// proposalsQueueEvent carries one proposals action the way the request ingress does.
func proposalsQueueEvent(payload map[string]any) core.Event {
	return core.Event{Source: core.SourceDesktop, Type: "knowledge.proposal.approve", Payload: payload}
}

func TestProposalApprovalStoresTheViaTags(t *testing.T) {
	knowledge := &proposalsKnowledgeStub{}
	h := newProposalsHandler(t, knowledge, &webReadingService{}, &fakeReader{answer: readAnswer("bounded text")})

	id, _, err := h.Propose(context.Background(), "web_article",
		map[string]any{"url": "https://example.com/news", "tags": []string{"via:search", strings.Repeat("x", 50)}},
		knowledgedomain.ProposalSource{ChannelID: "memes"}, "Ningo", "Ningo")
	if err != nil {
		t.Fatal(err)
	}
	approved, err := h.Handle(context.Background(), proposalsQueueEvent(map[string]any{"proposal_id": id}), coreStep("approve-knowledge-proposal"))
	if err != nil || approved["approved"] != true {
		t.Fatalf("approval did not store the article: %v %v", approved, err)
	}
	tags, _ := knowledge.payloads[0]["tags"].([]string)
	if len(tags) != 1 || tags[0] != "via:search" {
		t.Fatalf("the stored article lost its via tag or kept an oversized one: %v", knowledge.payloads[0]["tags"])
	}
}
