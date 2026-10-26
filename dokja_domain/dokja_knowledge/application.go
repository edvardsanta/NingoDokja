package knowledge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

const ActionPrepareApplication = "prepare-knowledge-application"
const ApplicationContract = "knowledge.application.context.v1"
const MaxApplicationSources = 8
const MaxApplicationSourceBytes = 16 * 1024
const MaxApplicationTextBytes = 64 * 1024

var applicationSourceID = regexp.MustCompile(`^[A-Za-z0-9._:/#@-]{1,240}$`)
var applicationVersion = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

// ApplicationSelection pins the exact text returned by knowledge.list, when a
// previous preparation supplied a version. It is not an authorization grant.
type ApplicationSelection struct {
	SourceID    string `json:"source_id"`
	TextVersion string `json:"text_version,omitempty"`
}

type ApplicationRequest struct {
	Contract    string                 `json:"contract"`
	Goal        string                 `json:"goal"`
	Constraints []string               `json:"constraints"`
	Sources     []ApplicationSelection `json:"sources"`
}

// ParseApplicationRequest validates before any source is read. Caller-supplied
// evidence, scope, outcomes and conclusions are intentionally not accepted.
func ParseApplicationRequest(payload map[string]any) (ApplicationRequest, error) {
	var request ApplicationRequest
	raw, err := json.Marshal(payload)
	if err != nil || len(raw) > 8192 {
		return request, fmt.Errorf("application request exceeds the 8192-byte JSON budget or is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return request, fmt.Errorf("invalid application request: %w", err)
	}
	if request.Contract != ApplicationContract {
		return request, fmt.Errorf("unsupported application contract")
	}
	if strings.TrimSpace(request.Goal) == "" || len(request.Goal) > 2000 {
		return request, fmt.Errorf("goal must contain 1..2000 bytes")
	}
	if len(request.Constraints) > 16 {
		return request, fmt.Errorf("at most 16 constraints")
	}
	for _, constraint := range request.Constraints {
		if strings.TrimSpace(constraint) == "" || len(constraint) > 500 {
			return request, fmt.Errorf("constraints must contain 1..500 bytes")
		}
	}
	if len(request.Sources) == 0 || len(request.Sources) > MaxApplicationSources {
		return request, fmt.Errorf("select 1..%d sources", MaxApplicationSources)
	}
	seen := map[string]bool{}
	for _, source := range request.Sources {
		if !applicationSourceID.MatchString(source.SourceID) || seen[source.SourceID] {
			return request, fmt.Errorf("sources need distinct valid source_id values")
		}
		seen[source.SourceID] = true
		if source.TextVersion != "" && !applicationVersion.MatchString(source.TextVersion) {
			return request, fmt.Errorf("invalid text_version")
		}
	}
	return request, nil
}

// LoadApplicationSource uses the existing store port. Knowledge remains unscoped:
// evidence must not be relabelled as private to the requesting task. Content is
// data, including any embedded instructions; no extraction or execution occurs.
func (d *Domain) LoadApplicationSource(ctx context.Context, selected ApplicationSelection) (map[string]any, error) {
	result, err := d.Handle(ctx, Request{Action: ActionListKnowledge, Event: Event{Payload: map[string]any{
		"source_id": selected.SourceID, "include_text": true, "limit": 1,
	}}})
	if err != nil {
		return nil, fmt.Errorf("application source read failed")
	}
	// Normalize the transport's map/slice representation, including test adapters.
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("invalid application source response")
	}
	var response struct {
		Documents []struct {
			SourceID  string `json:"source_id"`
			SourceRef string `json:"source_ref"`
			Title     string `json:"title"`
			Text      string `json:"text"`
		} `json:"documents"`
	}
	if err := json.Unmarshal(raw, &response); err != nil || len(response.Documents) != 1 {
		return nil, fmt.Errorf("application source unavailable or ambiguous")
	}
	document := response.Documents[0]
	if document.SourceID != selected.SourceID {
		return nil, fmt.Errorf("application source identity mismatch")
	}
	if strings.TrimSpace(document.Text) == "" || len(document.Text) > MaxApplicationSourceBytes {
		return nil, fmt.Errorf("application source text is empty or exceeds the %d-byte budget", MaxApplicationSourceBytes)
	}
	if len(document.SourceRef) > 2048 || len(document.Title) > 1000 {
		return nil, fmt.Errorf("application source provenance exceeds budget")
	}
	digest := sha256.Sum256([]byte(document.Text))
	version := "sha256:" + hex.EncodeToString(digest[:])
	if selected.TextVersion != "" && selected.TextVersion != version {
		return nil, fmt.Errorf("application source text version changed")
	}
	return map[string]any{
		"source_id": document.SourceID, "source_ref": document.SourceRef, "title": document.Title,
		"text": document.Text, "text_version": version, "trust": "unassessed_source_content",
	}, nil
}

// AssembleApplicationContext does not infer truth, applicability or a procedure.
// It exposes a bounded set of observations for a subsequent proposal/assessment.
func AssembleApplicationContext(request ApplicationRequest, sources []map[string]any) (map[string]any, error) {
	if len(sources) != len(request.Sources) {
		return nil, fmt.Errorf("incomplete application sources")
	}
	total := 0
	observations := make([]any, 0, len(sources))
	for i, source := range sources {
		if source["source_id"] != request.Sources[i].SourceID {
			return nil, fmt.Errorf("application source order or identity mismatch")
		}
		text, _ := source["text"].(string)
		total += len(text)
		observations = append(observations, source)
	}
	if total > MaxApplicationTextBytes {
		return nil, fmt.Errorf("application text exceeds the %d-byte aggregate budget", MaxApplicationTextBytes)
	}
	return map[string]any{
		"contract": ApplicationContract, "goal": request.Goal, "constraints": request.Constraints,
		"sources": observations, "applicability": "unassessed", "experience_status": "not_consulted",
	}, nil
}
