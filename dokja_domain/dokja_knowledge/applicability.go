package knowledge

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

const ActionAssessApplication = "assess-knowledge-application"
const AssessmentContract = "knowledge.application.assessment.v1"
const MaxAssessmentRequestBytes = 32 * 1024

var proposalID = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._-]{0,63}$`)

// SourceCitation verifies a span, not the meaning of its proposed relation.
type SourceCitation struct {
	SourceID    string `json:"source_id"`
	TextVersion string `json:"text_version"`
	Start       int    `json:"start"`
	End         int    `json:"end"`
	Quote       string `json:"quote"`
	Relation    string `json:"relation"`
}

type CandidateClaim struct {
	ID        string           `json:"id"`
	Statement string           `json:"statement"`
	Citations []SourceCitation `json:"citations"`
}

type CandidateMethod struct {
	ID            string           `json:"id"`
	Description   string           `json:"description"`
	Prerequisites []string         `json:"prerequisites"`
	Citations     []SourceCitation `json:"citations"`
}

type ApplicationProposal struct {
	Claims []CandidateClaim `json:"claims"`
	Method CandidateMethod  `json:"method"`
}

type AssessmentRequest struct {
	Contract    string              `json:"contract"`
	Application ApplicationRequest  `json:"application"`
	Proposal    ApplicationProposal `json:"proposal"`
}

func ParseAssessmentRequest(payload map[string]any) (AssessmentRequest, error) {
	var request AssessmentRequest
	raw, err := json.Marshal(payload)
	if err != nil || len(raw) > MaxAssessmentRequestBytes {
		return request, fmt.Errorf("invalid or oversized assessment request")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return request, fmt.Errorf("invalid assessment request")
	}
	if err := validateAssessmentRequest(request); err != nil {
		return request, err
	}
	return request, nil
}

func validateAssessmentRequest(request AssessmentRequest) error {
	if request.Contract != AssessmentContract {
		return fmt.Errorf("unsupported assessment contract")
	}
	raw, err := json.Marshal(request.Application)
	if err != nil {
		return fmt.Errorf("invalid application request")
	}
	var payload map[string]any
	if json.Unmarshal(raw, &payload) != nil {
		return fmt.Errorf("invalid application request")
	}
	if _, err := ParseApplicationRequest(payload); err != nil {
		return err
	}
	versions := map[string]string{}
	for _, source := range request.Application.Sources {
		if source.TextVersion == "" {
			return fmt.Errorf("assessment requires pinned source text versions")
		}
		versions[source.SourceID] = source.TextVersion
	}
	proposal := request.Proposal
	if len(proposal.Claims) < 1 || len(proposal.Claims) > 16 {
		return fmt.Errorf("assessment needs 1..16 candidate claims")
	}
	ids := map[string]bool{}
	count := 0
	validateCitations := func(citations []SourceCitation, method bool) error {
		if len(citations) > 8 {
			return fmt.Errorf("at most 8 citations per claim or method")
		}
		seen := map[SourceCitation]bool{}
		for _, citation := range citations {
			count++
			if count > 32 {
				return fmt.Errorf("at most 32 citations per assessment")
			}
			if versions[citation.SourceID] == "" || citation.TextVersion != versions[citation.SourceID] {
				return fmt.Errorf("citation source or version is not selected")
			}
			if citation.Start < 0 || citation.End <= citation.Start || citation.End > MaxApplicationSourceBytes || len(citation.Quote) == 0 || len(citation.Quote) > 1000 || !utf8.ValidString(citation.Quote) || citation.End-citation.Start != len(citation.Quote) {
				return fmt.Errorf("invalid citation span")
			}
			if citation.Relation != "supports" && citation.Relation != "contradicts" {
				return fmt.Errorf("invalid proposed citation relation")
			}
			if method && citation.Relation != "supports" {
				return fmt.Errorf("method citations must propose support")
			}
			if seen[citation] {
				return fmt.Errorf("duplicate citation")
			}
			seen[citation] = true
		}
		return nil
	}
	for _, claim := range proposal.Claims {
		if !proposalID.MatchString(claim.ID) || ids[claim.ID] || strings.TrimSpace(claim.Statement) == "" || len(claim.Statement) > 1200 {
			return fmt.Errorf("invalid candidate claim")
		}
		ids[claim.ID] = true
		if err := validateCitations(claim.Citations, false); err != nil {
			return err
		}
	}
	method := proposal.Method
	if !proposalID.MatchString(method.ID) || ids[method.ID] || strings.TrimSpace(method.Description) == "" || len(method.Description) > 2000 || len(method.Prerequisites) < 1 || len(method.Prerequisites) > 16 {
		return fmt.Errorf("invalid candidate method")
	}
	seen := map[string]bool{}
	for _, id := range method.Prerequisites {
		if !ids[id] || seen[id] {
			return fmt.Errorf("invalid or duplicate prerequisite reference")
		}
		seen[id] = true
	}
	return validateCitations(method.Citations, true)
}

// AssessApplication checks citations against freshly read pinned observations and
// evaluates the proposed argument. It cannot validate semantic entailment or
// prerequisite completeness and never authorizes execution or records success.
func AssessApplication(request AssessmentRequest, application map[string]any) (map[string]any, error) {
	raw, err := json.Marshal(request)
	if err != nil || len(raw) > MaxAssessmentRequestBytes {
		return nil, fmt.Errorf("invalid or oversized assessment request")
	}
	if err := validateAssessmentRequest(request); err != nil {
		return nil, err
	}
	observations, ok := application["sources"].([]any)
	if !ok || application["contract"] != ApplicationContract || len(observations) != len(request.Application.Sources) {
		return nil, fmt.Errorf("invalid prepared application context")
	}
	texts := map[string]string{}
	totalText := 0
	provenance := make([]any, 0, len(observations))
	for i, observation := range observations {
		source, ok := observation.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid prepared source")
		}
		selected := request.Application.Sources[i]
		text, ok := source["text"].(string)
		if !ok || text == "" || len(text) > MaxApplicationSourceBytes || source["source_id"] != selected.SourceID || source["text_version"] != selected.TextVersion {
			return nil, fmt.Errorf("prepared source identity or version mismatch")
		}
		sum := sha256.Sum256([]byte(text))
		if "sha256:"+hex.EncodeToString(sum[:]) != selected.TextVersion {
			return nil, fmt.Errorf("prepared source fingerprint mismatch")
		}
		totalText += len(text)
		if totalText > MaxApplicationTextBytes {
			return nil, fmt.Errorf("prepared source text exceeds aggregate budget")
		}
		texts[selected.SourceID] = text
		provenance = append(provenance, map[string]any{"source_id": selected.SourceID, "text_version": selected.TextVersion, "source_ref": source["source_ref"], "title": source["title"]})
	}
	verify := func(citations []SourceCitation) error {
		for _, citation := range citations {
			text := texts[citation.SourceID]
			if citation.End > len(text) || text[citation.Start:citation.End] != citation.Quote {
				return fmt.Errorf("citation does not match the selected source text")
			}
		}
		return nil
	}
	states := map[string]string{}
	claims := make([]any, 0, len(request.Proposal.Claims))
	for _, claim := range request.Proposal.Claims {
		if err := verify(claim.Citations); err != nil {
			return nil, err
		}
		support, contradict := false, false
		for _, citation := range claim.Citations {
			if citation.Relation == "supports" {
				support = true
			} else {
				contradict = true
			}
		}
		state := "unknown"
		switch {
		case support && contradict:
			state = "conflicted"
		case support:
			state = "supported"
		case contradict:
			state = "contradicted"
		}
		states[claim.ID] = state
		claims = append(claims, map[string]any{"id": claim.ID, "statement": claim.Statement, "citations": claim.Citations, "citation_state": state})
	}
	if err := verify(request.Proposal.Method.Citations); err != nil {
		return nil, err
	}
	missing, conflicted, blocked := []string{}, []string{}, []string{}
	for _, id := range request.Proposal.Method.Prerequisites {
		switch states[id] {
		case "unknown":
			missing = append(missing, id)
		case "conflicted":
			conflicted = append(conflicted, id)
		case "contradicted":
			blocked = append(blocked, id)
		}
	}
	applicability := "conditional"
	switch {
	case len(blocked) > 0:
		applicability = "blocked"
	case len(conflicted) > 0:
		applicability = "conflicted"
	case len(missing) > 0 || len(request.Proposal.Method.Citations) == 0:
		applicability = "needs_evidence"
	}
	result := map[string]any{
		"contract": AssessmentContract, "goal": request.Application.Goal, "constraints": request.Application.Constraints,
		"sources": provenance, "claims": claims, "method": request.Proposal.Method, "applicability": applicability,
		"missing_prerequisites": missing, "conflicted_prerequisites": conflicted, "blocked_prerequisites": blocked,
		"method_cited": len(request.Proposal.Method.Citations) > 0, "semantic_status": "unverified",
		"requires_semantic_review": true, "can_execute": false, "experience_status": "not_consulted",
	}
	// Normalize typed slices/structs before the workspace takes an isolated copy.
	raw, err = json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("invalid assessment result")
	}
	var normalized map[string]any
	if json.Unmarshal(raw, &normalized) != nil {
		return nil, fmt.Errorf("invalid assessment result")
	}
	return normalized, nil
}
