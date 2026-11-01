package knowledge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

// The fixture mirrors the shape of the development cases only. Held-out cases stay
// untouched until the frozen comparison runs.
const reviewPolicy = "East policy, revision 2. Retries are allowed for east only, within 24 hours of the first attempt."
const reviewGuide = "Use bounded backoff with jitter."

func reviewFixture(t *testing.T) (ReviewRequest, map[string]any) {
	t.Helper()
	version := func(text string) string {
		sum := sha256.Sum256([]byte(text))
		return "sha256:" + hex.EncodeToString(sum[:])
	}
	cite := func(id, text, quote string) SourceCitation {
		start := strings.Index(text, quote)
		return SourceCitation{SourceID: id, TextVersion: version(text), Start: start, End: start + len(quote), Quote: quote, Relation: "supports"}
	}
	request := ReviewRequest{Contract: ReviewRequestContract, Assessment: AssessmentRequest{
		Contract: AssessmentContract,
		Application: ApplicationRequest{Contract: ApplicationContract, Goal: "Decide whether east may retry", Sources: []ApplicationSelection{
			{SourceID: "policy", TextVersion: version(reviewPolicy)}, {SourceID: "guide", TextVersion: version(reviewGuide)},
		}},
		Proposal: ApplicationProposal{
			Claims: []CandidateClaim{
				{ID: "window", Statement: "The retry is inside the window", Citations: []SourceCitation{cite("policy", reviewPolicy, "within 24 hours")}},
				{ID: "note", Statement: "Jitter is mentioned", Citations: []SourceCitation{cite("guide", reviewGuide, "jitter")}},
			},
			Method: CandidateMethod{ID: "retry", Description: "Retry with backoff", Prerequisites: []string{"window"}, Citations: []SourceCitation{cite("guide", reviewGuide, "bounded backoff")}},
		},
	}}
	application := map[string]any{"contract": ApplicationContract, "sources": []any{
		map[string]any{"source_id": "policy", "text": reviewPolicy, "text_version": version(reviewPolicy)},
		map[string]any{"source_id": "guide", "text": reviewGuide, "text_version": version(reviewGuide)},
	}}
	return request, application
}

func acceptedReview() ProviderReview {
	return ProviderReview{
		Contract: ReviewContract, TaskID: "task", Revision: 1,
		RequiredConditions: []ReviewCondition{{ID: "in-window", Statement: "The retry happens within 24 hours", CoveredBy: []string{"window"}}},
		Claims:             []ReviewFinding{{ID: "window", Verdict: "supported", Reason: "stated"}, {ID: "note", Verdict: "supported", Reason: "stated"}},
		Method:             ReviewFinding{ID: "retry", Verdict: "supported", Reason: "stated"},
		Conflicts:          []ReviewConflict{}, Verdict: "accepted", Reason: "all conditions hold",
	}
}

func decodeReview(t *testing.T, reply ProviderReview, applicability string) (map[string]any, error) {
	t.Helper()
	request, application := reviewFixture(t)
	body, err := json.Marshal(reply)
	if err != nil {
		t.Fatal(err)
	}
	return DecodeProviderReview("task", request, application, map[string]any{"applicability": applicability}, ProposalResponse{Body: string(body), Provider: "fixture", RequestedModel: "model"})
}

func TestReviewAcceptsOnlyWithoutOpenFindings(t *testing.T) {
	result, err := decodeReview(t, acceptedReview(), "conditional")
	if err != nil {
		t.Fatal(err)
	}
	if result["outcome"] != "accepted" || result["semantic_status"] != "model_reviewed" || result["can_execute"] != false || result["experience_status"] != "not_consulted" {
		t.Fatal(result)
	}
}

// Each mutation is an error an argument-integrity check cannot see: every quotation is
// real, so only the reviewer's findings and the outcome rule can stop it.
func TestReviewOutcomeIsStricterThanAnAcceptingVerdict(t *testing.T) {
	cases := []struct {
		name          string
		mutate        func(*ProviderReview)
		applicability string
		outcome       string
		reason        string
	}{
		{"omitted prerequisite", func(r *ProviderReview) { r.RequiredConditions[0].CoveredBy = nil }, "conditional", "needs_revision", "condition_uncovered"},
		{"covered beside the argument", func(r *ProviderReview) { r.RequiredConditions[0].CoveredBy = []string{"note"} }, "conditional", "needs_revision", "condition_uncovered"},
		{"wrong region", func(r *ProviderReview) { r.Claims[0].Verdict = "out_of_scope" }, "conditional", "rejected", "prerequisite_refuted"},
		{"date out of window", func(r *ProviderReview) { r.Claims[0].Verdict = "stale" }, "conditional", "rejected", "prerequisite_refuted"},
		{"misread method", func(r *ProviderReview) { r.Method.Verdict = "misread" }, "conditional", "rejected", "method_refuted"},
		{"unsupported method", func(r *ProviderReview) { r.Method.Verdict = "unsupported" }, "conditional", "needs_revision", "method_unsupported"},
		{"side claim misread", func(r *ProviderReview) { r.Claims[1].Verdict = "misread" }, "conditional", "needs_revision", "claim_not_supported"},
		{"hidden contradiction", func(r *ProviderReview) {
			r.Conflicts = []ReviewConflict{{Statement: "Policy limits retries", Citations: []QuotedCitation{{SourceID: "policy", TextVersion: sourceVersion(reviewPolicy), Quote: "east only", Relation: "contradicts"}}}}
		}, "conditional", "needs_revision", "conflict_unaddressed"},
		{"assessment blocked", func(*ProviderReview) {}, "blocked", "rejected", "prerequisite_contradicted"},
		{"assessment missing evidence", func(*ProviderReview) {}, "needs_evidence", "needs_revision", "assessment_incomplete"},
		{"reviewer asks revision", func(r *ProviderReview) { r.Verdict = "needs_revision" }, "conditional", "needs_revision", "reviewer_requested_revision"},
		{"reviewer rejects", func(r *ProviderReview) { r.Verdict = "rejected" }, "conditional", "rejected", "reviewer_rejected"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reply := acceptedReview()
			c.mutate(&reply)
			result, err := decodeReview(t, reply, c.applicability)
			if err != nil {
				t.Fatal(err)
			}
			reasons, _ := result["outcome_reasons"].([]any)
			found := false
			for _, reason := range reasons {
				found = found || reason == c.reason
			}
			if result["outcome"] != c.outcome || !found {
				t.Fatal(result["outcome"], reasons)
			}
		})
	}
}

func sourceVersion(text string) string {
	sum := sha256.Sum256([]byte(text))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func TestReviewRefusesRepliesThatDoNotFitTheCandidate(t *testing.T) {
	cases := map[string]func(*ProviderReview){
		"other task":         func(r *ProviderReview) { r.TaskID = "other" },
		"other contract":     func(r *ProviderReview) { r.Contract = ProposalContract },
		"later revision":     func(r *ProviderReview) { r.Revision = 2 },
		"unknown verdict":    func(r *ProviderReview) { r.Verdict = "maybe" },
		"unknown finding":    func(r *ProviderReview) { r.Claims[0].Verdict = "fine" },
		"blank reason":       func(r *ProviderReview) { r.Reason = " " },
		"skipped claim":      func(r *ProviderReview) { r.Claims = r.Claims[:1] },
		"repeated claim":     func(r *ProviderReview) { r.Claims[1] = r.Claims[0] },
		"foreign claim":      func(r *ProviderReview) { r.Claims[1].ID = "foreign" },
		"other method":       func(r *ProviderReview) { r.Method.ID = "foreign" },
		"no condition":       func(r *ProviderReview) { r.RequiredConditions = nil },
		"unknown coverage":   func(r *ProviderReview) { r.RequiredConditions[0].CoveredBy = []string{"foreign"} },
		"repeated condition": func(r *ProviderReview) { r.RequiredConditions = append(r.RequiredConditions, r.RequiredConditions[0]) },
		"uncited conflict":   func(r *ProviderReview) { r.Conflicts = []ReviewConflict{{Statement: "Conflict"}} },
		"fabricated quote": func(r *ProviderReview) {
			r.RequiredConditions[0].Citations = []QuotedCitation{{SourceID: "policy", TextVersion: sourceVersion(reviewPolicy), Quote: "west is allowed", Relation: "supports"}}
		},
		"bad relation": func(r *ProviderReview) {
			r.RequiredConditions[0].Citations = []QuotedCitation{{SourceID: "policy", TextVersion: sourceVersion(reviewPolicy), Quote: "east only", Relation: "implies"}}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			reply := acceptedReview()
			mutate(&reply)
			if result, err := decodeReview(t, reply, "conditional"); err == nil {
				t.Fatal(result)
			}
		})
	}
}

func TestReviewRefusesUnknownFieldsAndTrailingData(t *testing.T) {
	request, application := reviewFixture(t)
	valid, _ := json.Marshal(acceptedReview())
	for _, body := range []string{string(valid) + "{}", strings.Replace(string(valid), `{"contract"`, `{"proof":true,"contract"`, 1), "not json"} {
		if _, err := DecodeProviderReview("task", request, application, map[string]any{"applicability": "conditional"}, ProposalResponse{Body: body}); err == nil {
			t.Fatal("accepted", body)
		}
	}
}

func TestReviewAcceptsOneEnclosingFenceOnly(t *testing.T) {
	request, application := reviewFixture(t)
	valid, _ := json.Marshal(acceptedReview())
	decode := func(body string) error {
		_, err := DecodeProviderReview("task", request, application, map[string]any{"applicability": "conditional"}, ProposalResponse{Body: body})
		return err
	}
	for _, body := range []string{"```json\n" + string(valid) + "\n```", "```\n" + string(valid) + "\n```\n"} {
		if err := decode(body); err != nil {
			t.Fatal(body, err)
		}
	}
	for _, body := range []string{"Here it is:\n```json\n" + string(valid) + "\n```", "```python\n" + string(valid) + "\n```", "```json\n" + string(valid) + "\n```\n```\n{}\n```"} {
		if decode(body) == nil {
			t.Fatal("accepted", body)
		}
	}
}

func TestReviewCountsAConditionTheMethodItselfApplies(t *testing.T) {
	reply := acceptedReview()
	reply.RequiredConditions[0].CoveredBy = []string{"retry"}
	result, err := decodeReview(t, reply, "conditional")
	if err != nil || result["outcome"] != "accepted" {
		t.Fatal(result, err)
	}
}

func TestReviewStatesHowIndependentItIs(t *testing.T) {
	cases := []struct {
		origin *ProposalOrigin
		want   string
	}{
		{nil, "unknown"},
		{&ProposalOrigin{Provider: "fixture", RequestedModel: "model"}, "same_model"},
		{&ProposalOrigin{Provider: "fixture", RequestedModel: "other"}, "different_model"},
	}
	for _, c := range cases {
		request, application := reviewFixture(t)
		request.ProposedBy = c.origin
		body, _ := json.Marshal(acceptedReview())
		result, err := DecodeProviderReview("task", request, application, map[string]any{"applicability": "conditional"}, ProposalResponse{Body: string(body), Provider: "fixture", RequestedModel: "model"})
		if err != nil || result["independence"] != c.want {
			t.Fatal(result, err)
		}
	}
}

func TestReviewPromptLeavesOutTheProposerAccount(t *testing.T) {
	request, application := reviewFixture(t)
	prompt, err := BuildReviewPrompt("task", application, request.Assessment.Proposal)
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(prompt.Data), &data); err != nil {
		t.Fatal(err)
	}
	candidate := data["candidate"].(map[string]any)
	if len(candidate) != 2 || candidate["claims"] == nil || candidate["method"] == nil || strings.Contains(prompt.Data, "summary") {
		t.Fatal(prompt.Data)
	}
	if !strings.Contains(prompt.Instructions, "before weighing the candidate") {
		t.Fatal("reviewer no longer derives conditions first")
	}
}

func TestReviewRequestIsValidatedBeforeAnyRead(t *testing.T) {
	request, _ := reviewFixture(t)
	parse := func(mutate func(map[string]any)) error {
		raw, _ := json.Marshal(request)
		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatal(err)
		}
		mutate(payload)
		_, err := ParseReviewRequest(payload)
		return err
	}
	if err := parse(func(map[string]any) {}); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(map[string]any){
		"contract": func(p map[string]any) { p["contract"] = AssessmentContract },
		"field":    func(p map[string]any) { p["verified"] = true },
		"origin":   func(p map[string]any) { p["proposed_by"] = map[string]any{"provider": " "} },
		"unpinned": func(p map[string]any) {
			assessment := p["assessment"].(map[string]any)["application"].(map[string]any)
			assessment["sources"].([]any)[0].(map[string]any)["text_version"] = ""
		},
	} {
		if parse(mutate) == nil {
			t.Fatal("accepted", name)
		}
	}
}
