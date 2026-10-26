package knowledge

import (
	"encoding/json"
	"strings"
	"testing"
)

func providerFixture(t *testing.T) (ApplicationRequest, map[string]any, ProviderProposal) {
	t.Helper()
	request, application := assessmentFixture(t)
	quote := func(c SourceCitation) QuotedCitation {
		return QuotedCitation{SourceID: c.SourceID, TextVersion: c.TextVersion, Quote: c.Quote, Relation: c.Relation}
	}
	proposal := &QuotedProposal{}
	for _, claim := range request.Proposal.Claims {
		citations := []QuotedCitation{}
		for _, c := range claim.Citations {
			citations = append(citations, quote(c))
		}
		proposal.Claims = append(proposal.Claims, QuotedClaim{ID: claim.ID, Statement: claim.Statement, Citations: citations})
	}
	m := request.Proposal.Method
	proposal.Method = QuotedMethod{ID: m.ID, Description: m.Description, Prerequisites: m.Prerequisites}
	for _, c := range m.Citations {
		proposal.Method.Citations = append(proposal.Method.Citations, quote(c))
	}
	return request.Application, application, ProviderProposal{Contract: ProposalContract, TaskID: "current-task", Revision: 1, Disposition: "propose", Summary: "The proposed method depends on the API guarantee and operational eligibility", Proposal: proposal}
}
func proposalBody(t *testing.T, reply ProviderProposal) string {
	t.Helper()
	raw, err := json.Marshal(reply)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
func TestProviderProposalAnchorsQuotesAndRemainsUnverified(t *testing.T) {
	request, application, reply := providerFixture(t)
	for i := range request.Sources {
		request.Sources[i].TextVersion = ""
	}
	prompt, err := BuildProposalPrompt("current-task", application)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt.Data, "current-task") || !strings.Contains(prompt.Instructions, "untrusted observations") {
		t.Fatal("missing task binding/data boundary")
	}
	result, err := DecodeProviderProposal("current-task", request, application, proposalBody(t, reply))
	if err != nil {
		t.Fatal(err)
	}
	assessment := result["assessment"].(map[string]any)
	if assessment["applicability"] != "conditional" || result["semantic_status"] != "unverified" || result["can_execute"] != false {
		t.Fatalf("overstated proposal: %v", result)
	}
	for _, claim := range assessment["claims"].([]any) {
		for _, c := range claim.(map[string]any)["citations"].([]any) {
			citation := c.(map[string]any)
			if citation["start"].(float64) < 0 || citation["end"].(float64) <= citation["start"].(float64) {
				t.Fatal("quotation not anchored")
			}
		}
	}
	for _, source := range request.Sources {
		if source.TextVersion != "" {
			t.Fatal("decoder mutated caller-owned selection")
		}
	}
}
func TestProviderMayClarifyOrAbstainWithoutInventingMethod(t *testing.T) {
	for _, disposition := range []string{"clarify", "abstain"} {
		request, application, reply := providerFixture(t)
		reply.Disposition = disposition
		reply.Proposal = nil
		result, err := DecodeProviderProposal("current-task", request, application, proposalBody(t, reply))
		if err != nil {
			t.Fatal(err)
		}
		if result["disposition"] != disposition || result["assessment"] != nil || result["can_execute"] != false {
			t.Fatalf("invented method: %v", result)
		}
	}
}
func TestProviderRejectsInvalidAndStaleReplies(t *testing.T) {
	mutations := []func(*ProviderProposal){
		func(p *ProviderProposal) { p.TaskID = "foreign-task" }, func(p *ProviderProposal) { p.Revision = 2 },
		func(p *ProviderProposal) { p.Contract = "v0" }, func(p *ProviderProposal) { p.Disposition = "execute" },
		func(p *ProviderProposal) { p.Summary = " " }, func(p *ProviderProposal) { p.Proposal = nil },
		func(p *ProviderProposal) { p.Disposition = "clarify" },
		func(p *ProviderProposal) { p.Proposal.Claims[0].Citations[0].Quote = "Invented quotation" },
		func(p *ProviderProposal) { p.Proposal.Claims[0].Citations[0].SourceID = "foreign" },
		func(p *ProviderProposal) { p.Proposal.Claims[0].Citations[0].TextVersion = "stale" },
	}
	for _, mutate := range mutations {
		request, application, reply := providerFixture(t)
		mutate(&reply)
		if _, err := DecodeProviderProposal("current-task", request, application, proposalBody(t, reply)); err == nil {
			t.Fatal("accepted invalid provider proposal")
		}
	}
	request, application, reply := providerFixture(t)
	valid := proposalBody(t, reply)
	for _, body := range []string{"not JSON", valid + " {}", strings.Repeat("x", MaxProposalReplyBytes+1), strings.Replace(valid, "\"revision\":1", "\"revision\":1,\"can_execute\":true", 1), strings.Replace(valid, "\"proposal\":", "\"unexpected\":", 1)} {
		if _, err := DecodeProviderProposal("current-task", request, application, body); err == nil {
			t.Fatal("accepted malformed provider output")
		}
	}
}
func TestProviderRejectsAmbiguousAndOverlappingQuotations(t *testing.T) {
	for _, quote := range []string{"the", "aa"} {
		request, application, reply := providerFixture(t)
		c := &reply.Proposal.Claims[0].Citations[0]
		c.Quote = quote
		if quote == "aa" {
			application["sources"].([]any)[1].(map[string]any)["text"] = "aaa"
		}
		if _, err := DecodeProviderProposal("current-task", request, application, proposalBody(t, reply)); err == nil {
			t.Fatal("accepted ambiguous quotation")
		}
	}
}
func TestProviderPromptBudgetDoesNotTruncateEvidence(t *testing.T) {
	_, application, _ := providerFixture(t)
	application["padding"] = strings.Repeat("x", MaxProposalPromptBytes)
	if _, err := BuildProposalPrompt("current-task", application); err == nil {
		t.Fatal("accepted oversized prompt")
	}
}

func TestProviderProposalPreservesExplicitSelectionPins(t *testing.T) {
	request, application, reply := providerFixture(t)
	request.Sources[0].TextVersion = "sha256:" + strings.Repeat("0", 64)
	if _, err := DecodeProviderProposal("current-task", request, application, proposalBody(t, reply)); err == nil {
		t.Fatal("accepted a different explicitly pinned source")
	}
}
