package knowledge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

func assessmentFixture(t *testing.T) (AssessmentRequest, map[string]any) {
	t.Helper()
	raw, err := os.ReadFile("testdata/application/corpus_v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Sources []struct {
			SourceID  string `json:"source_id"`
			SourceRef string `json:"source_ref"`
			Text      string `json:"text"`
		}
	}
	if err = json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	request := AssessmentRequest{Contract: AssessmentContract, Application: ApplicationRequest{Contract: ApplicationContract, Goal: "Explain whether to apply the retry procedure under the API and operations constraints", Constraints: []string{"Do not execute requests"}}}
	sources := []map[string]any{}
	for _, doc := range corpus.Sources[:3] {
		sum := sha256.Sum256([]byte(doc.Text))
		version := "sha256:" + hex.EncodeToString(sum[:])
		request.Application.Sources = append(request.Application.Sources, ApplicationSelection{SourceID: doc.SourceID, TextVersion: version})
		sources = append(sources, map[string]any{"source_id": doc.SourceID, "source_ref": doc.SourceRef, "title": doc.SourceID, "text": doc.Text, "text_version": version})
	}
	citation := func(index int, quote string) SourceCitation {
		text := sources[index]["text"].(string)
		start := strings.Index(text, quote)
		if start < 0 {
			t.Fatal("fixture quotation missing")
		}
		return SourceCitation{SourceID: request.Application.Sources[index].SourceID, TextVersion: request.Application.Sources[index].TextVersion, Start: start, End: start + len(quote), Quote: quote, Relation: "supports"}
	}
	request.Proposal = ApplicationProposal{
		Claims: []CandidateClaim{
			{ID: "idempotent", Statement: "The operation satisfies the idempotency contract", Citations: []SourceCitation{citation(1, "In the east deployment, POST /entries is idempotent when a nonempty request key and identical payload are reused.")}},
			{ID: "policy_eligible", Statement: "The request satisfies operations policy prerequisites", Citations: []SourceCitation{citation(2, "Retry only transient responses while the caller deadline remains open.")}},
		},
		Method: CandidateMethod{ID: "retry", Description: "Use bounded exponential backoff with jitter and preserve the logical request", Prerequisites: []string{"idempotent", "policy_eligible"}, Citations: []SourceCitation{citation(0, "Use bounded exponential backoff with jitter.")}},
	}
	application, err := AssembleApplicationContext(request.Application, sources)
	if err != nil {
		t.Fatal(err)
	}
	return request, application
}
func assessmentPayload(t *testing.T, request AssessmentRequest) map[string]any {
	t.Helper()
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err = json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	return payload
}
func TestAssessmentDerivesProvisionalApplicability(t *testing.T) {
	cases := []struct {
		name, want string
		mutate     func(*AssessmentRequest)
	}{
		{"cited", "conditional", func(*AssessmentRequest) {}},
		{"missing_prerequisite", "needs_evidence", func(r *AssessmentRequest) { r.Proposal.Claims[0].Citations = nil }},
		{"uncited_method", "needs_evidence", func(r *AssessmentRequest) { r.Proposal.Method.Citations = nil }},
		{"contradicted", "blocked", func(r *AssessmentRequest) { r.Proposal.Claims[0].Citations[0].Relation = "contradicts" }},
		{"conflicting", "conflicted", func(r *AssessmentRequest) {
			c := r.Proposal.Claims[0].Citations[0]
			c.Relation = "contradicts"
			r.Proposal.Claims[0].Citations = append(r.Proposal.Claims[0].Citations, c)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request, application := assessmentFixture(t)
			tc.mutate(&request)
			result, err := AssessApplication(request, application)
			if err != nil {
				t.Fatal(err)
			}
			if result["applicability"] != tc.want {
				t.Fatalf("got %v want %s", result, tc.want)
			}
			if result["semantic_status"] != "unverified" || result["requires_semantic_review"] != true || result["can_execute"] != false || result["experience_status"] != "not_consulted" {
				t.Fatalf("overstated assessment: %v", result)
			}
			if tc.name == "missing_prerequisite" && len(result["missing_prerequisites"].([]any)) != 1 {
				t.Fatal("missing prerequisite was hidden")
			}
			if tc.name == "conflicting" && len(result["conflicted_prerequisites"].([]any)) != 1 {
				t.Fatal("conflict was hidden")
			}
		})
	}
}
func TestAssessmentCitationIntegrityDoesNotEstablishMeaning(t *testing.T) {
	request, application := assessmentFixture(t)
	request.Proposal.Claims[0].Statement = "This unrelated quotation guarantees success for every future operation"
	result, err := AssessApplication(request, application)
	if err != nil {
		t.Fatal(err)
	}
	if result["semantic_status"] != "unverified" || result["can_execute"] != false || result["requires_semantic_review"] != true {
		t.Fatalf("citation integrity promoted to truth: %v", result)
	}
}
func TestAssessmentRejectsMalformedProposalBeforeReads(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*AssessmentRequest)
	}{
		{"contract", func(r *AssessmentRequest) { r.Contract = "v0" }},
		{"unpinned", func(r *AssessmentRequest) { r.Application.Sources[0].TextVersion = "" }},
		{"duplicate_claim", func(r *AssessmentRequest) { r.Proposal.Claims = append(r.Proposal.Claims, r.Proposal.Claims[0]) }},
		{"method_collision", func(r *AssessmentRequest) { r.Proposal.Method.ID = r.Proposal.Claims[0].ID }},
		{"unknown_prerequisite", func(r *AssessmentRequest) { r.Proposal.Method.Prerequisites = []string{"unknown"} }},
		{"duplicate_prerequisite", func(r *AssessmentRequest) { r.Proposal.Method.Prerequisites = []string{"idempotent", "idempotent"} }},
		{"vacuous_method", func(r *AssessmentRequest) { r.Proposal.Method.Prerequisites = nil }},
		{"empty_claims", func(r *AssessmentRequest) { r.Proposal.Claims = nil }},
		{"empty_statement", func(r *AssessmentRequest) { r.Proposal.Claims[0].Statement = " " }},
		{"foreign_source", func(r *AssessmentRequest) { r.Proposal.Claims[0].Citations[0].SourceID = "foreign" }},
		{"foreign_version", func(r *AssessmentRequest) {
			r.Proposal.Claims[0].Citations[0].TextVersion = "sha256:" + strings.Repeat("0", 64)
		}},
		{"negative_offset", func(r *AssessmentRequest) { r.Proposal.Claims[0].Citations[0].Start = -1 }},
		{"offset_length", func(r *AssessmentRequest) { r.Proposal.Claims[0].Citations[0].End++ }},
		{"invalid_relation", func(r *AssessmentRequest) { r.Proposal.Claims[0].Citations[0].Relation = "confirmed" }},
		{"contradicted_method", func(r *AssessmentRequest) { r.Proposal.Method.Citations[0].Relation = "contradicts" }},
		{"duplicate_citation", func(r *AssessmentRequest) {
			r.Proposal.Claims[0].Citations = append(r.Proposal.Claims[0].Citations, r.Proposal.Claims[0].Citations[0])
		}},
		{"too_many_citations", func(r *AssessmentRequest) { r.Proposal.Claims[0].Citations = make([]SourceCitation, 9) }},
		{"too_many_claims", func(r *AssessmentRequest) { r.Proposal.Claims = make([]CandidateClaim, 17) }},
		{"oversized_statement", func(r *AssessmentRequest) { r.Proposal.Claims[0].Statement = strings.Repeat("x", 1201) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request, _ := assessmentFixture(t)
			tc.mutate(&request)
			if _, err := ParseAssessmentRequest(assessmentPayload(t, request)); err == nil {
				t.Fatal("accepted invalid proposal")
			}
		})
	}
	request, _ := assessmentFixture(t)
	payload := assessmentPayload(t, request)
	payload["scope"] = "foreign"
	if _, err := ParseAssessmentRequest(payload); err == nil {
		t.Fatal("accepted caller scope")
	}
	payload = assessmentPayload(t, request)
	payload["proposal"].(map[string]any)["execute"] = true
	if _, err := ParseAssessmentRequest(payload); err == nil {
		t.Fatal("accepted execution instruction")
	}
	payload = assessmentPayload(t, request)
	payload["padding"] = strings.Repeat("x", MaxAssessmentRequestBytes)
	if _, err := ParseAssessmentRequest(payload); err == nil {
		t.Fatal("accepted oversized request")
	}
}
func TestAssessmentRejectsInventedOrStaleSourceSpans(t *testing.T) {
	for _, mutate := range []func(*AssessmentRequest, map[string]any){
		func(r *AssessmentRequest, _ map[string]any) {
			r.Proposal.Claims[0].Citations[0].Quote = strings.Repeat("x", len(r.Proposal.Claims[0].Citations[0].Quote))
		},
		func(r *AssessmentRequest, _ map[string]any) {
			r.Proposal.Claims[0].Citations[0].Start = 16000
			r.Proposal.Claims[0].Citations[0].End = 16000 + len(r.Proposal.Claims[0].Citations[0].Quote)
		},
		func(_ *AssessmentRequest, a map[string]any) {
			a["sources"].([]any)[0].(map[string]any)["text"] = "Changed after preparation"
		},
		func(_ *AssessmentRequest, a map[string]any) {
			a["sources"].([]any)[0].(map[string]any)["text_version"] = "stale"
		},
		func(_ *AssessmentRequest, a map[string]any) { a["sources"] = []any{} },
	} {
		request, application := assessmentFixture(t)
		mutate(&request, application)
		if _, err := AssessApplication(request, application); err == nil {
			t.Fatal("accepted unverifiable evidence")
		}
	}
}
func TestFrozenApplicationCorpusAndCasesRemainWellFormed(t *testing.T) {
	raw, err := os.ReadFile("testdata/application/corpus_v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Sources []struct {
			ID   string `json:"source_id"`
			Text string `json:"text"`
		}
	}
	if err = json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, source := range corpus.Sources {
		if ids[source.ID] || source.ID == "" || source.Text == "" || len(source.Text) > MaxApplicationSourceBytes {
			t.Fatal("invalid corpus source")
		}
		ids[source.ID] = true
	}
	raw, err = os.ReadFile("testdata/application/cases_v1.json")
	if err != nil {
		t.Fatal(err)
	}
	type testCase struct {
		ID       string   `json:"id"`
		Sources  []string `json:"sources"`
		Goal     string   `json:"goal"`
		Expected string   `json:"expected"`
	}
	var cases struct {
		Development []testCase `json:"development"`
		HeldOut     []testCase `json:"held_out"`
	}
	if err = json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases.Development) != 2 || len(cases.HeldOut) != 6 {
		t.Fatal("case split changed")
	}
	seen := map[string]bool{}
	for _, c := range append(cases.Development, cases.HeldOut...) {
		if seen[c.ID] || c.Goal == "" || c.Expected == "" || len(c.Sources) < 2 || len(c.Sources) > MaxApplicationSources {
			t.Fatal("invalid case")
		}
		seen[c.ID] = true
		for _, id := range c.Sources {
			if !ids[id] {
				t.Fatal("case references missing source")
			}
		}
	}
}

func TestAssessmentBoundsTheTotalCitationCount(t *testing.T) {
	request, application := assessmentFixture(t)
	text := application["sources"].([]any)[0].(map[string]any)["text"].(string)
	template := request.Proposal.Method.Citations[0]
	request.Proposal.Claims = nil
	for i := 0; i < 4; i++ {
		citations := []SourceCitation{}
		for offset := 0; offset < 8; offset++ {
			c := template
			c.Start = offset
			c.End = offset + 10
			c.Quote = text[c.Start:c.End]
			citations = append(citations, c)
		}
		request.Proposal.Claims = append(request.Proposal.Claims, CandidateClaim{ID: fmt.Sprintf("claim%d", i), Statement: "Candidate statement", Citations: citations})
	}
	request.Proposal.Method.Prerequisites = []string{"claim0"}
	_, err := ParseAssessmentRequest(assessmentPayload(t, request))
	if err == nil || !strings.Contains(err.Error(), "32 citations") {
		t.Fatalf("global citation budget not enforced: %v", err)
	}
}
