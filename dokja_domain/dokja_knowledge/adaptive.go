package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

const ActionProposeAdaptiveApplication = "propose-adaptive-knowledge-application"
const AdaptiveContract = "knowledge.application.adaptive.v1"

// AdaptiveMaxProposals stays below the protocol's three calls: real proposals take about
// 30 seconds, so a third would not fit the 90-second run.
const AdaptiveMaxProposals = 2
const AdaptiveInitialDocuments = 3
const AdaptiveExtraDocuments = 2

// MaxAdaptiveTextBytes leaves room for the instructions inside the proposal prompt.
const MaxAdaptiveTextBytes = 18 * 1024
const maxAdaptiveQueryBytes = 1000
const adaptiveSearchHits = 12

type AdaptiveRequest struct {
	Contract    string   `json:"contract"`
	Goal        string   `json:"goal"`
	Constraints []string `json:"constraints"`
}

// ParseAdaptiveRequest accepts no sources: finding them is the point of this strategy.
func ParseAdaptiveRequest(payload map[string]any) (AdaptiveRequest, error) {
	var request AdaptiveRequest
	raw, err := json.Marshal(payload)
	if err != nil || len(raw) > 8192 {
		return request, fmt.Errorf("adaptive request exceeds the 8192-byte JSON budget or is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return request, fmt.Errorf("invalid adaptive request")
	}
	if request.Contract != AdaptiveContract {
		return request, fmt.Errorf("unsupported adaptive contract")
	}
	return request, validateApplicationTask(request.Goal, request.Constraints)
}

// Application is the selection a found set of sources amounts to.
func (r AdaptiveRequest) Application(sources []map[string]any) ApplicationRequest {
	application := ApplicationRequest{Contract: ApplicationContract, Goal: r.Goal, Constraints: r.Constraints}
	for _, source := range sources {
		id, _ := source["source_id"].(string)
		application.Sources = append(application.Sources, ApplicationSelection{SourceID: id})
	}
	return application
}

// AdaptiveFound separates what a search led to from what could not be used, so a gap
// caused by a budget is not mistaken for evidence that does not exist.
type AdaptiveFound struct {
	Sources []map[string]any
	Skipped []string
}

// FindApplicationSources reads the most relevant documents not already loaded. A search
// rank is a retrieval signal only: it says nothing about reliability or applicability.
func (d *Domain) FindApplicationSources(ctx context.Context, query string, loaded map[string]bool, limit, textBudget int) (AdaptiveFound, error) {
	found := AdaptiveFound{}
	answer, err := d.Handle(ctx, Request{Action: ActionSearchKnowledge, Event: Event{Source: "orchestrator", Type: "knowledge.search", Payload: map[string]any{"query": query, "k": adaptiveSearchHits}}})
	if err != nil {
		return found, fmt.Errorf("application source search failed")
	}
	raw, err := json.Marshal(answer)
	if err != nil {
		return found, fmt.Errorf("invalid application search response")
	}
	var result struct {
		Hits []struct {
			SourceID string `json:"source_id"`
			Kind     string `json:"kind"`
			Relevant bool   `json:"relevant"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return found, fmt.Errorf("invalid application search response")
	}
	seen := map[string]bool{}
	for _, hit := range result.Hits {
		if len(found.Sources) == limit {
			break
		}
		if !hit.Relevant || hit.Kind == HashtagKnowledgeKind || seen[hit.SourceID] || loaded[hit.SourceID] {
			continue
		}
		seen[hit.SourceID] = true
		source, err := d.LoadApplicationSource(ctx, ApplicationSelection{SourceID: hit.SourceID})
		if ctx.Err() != nil {
			return found, ctx.Err()
		}
		text, _ := source["text"].(string)
		if err != nil || len(text) > textBudget {
			found.Skipped = append(found.Skipped, hit.SourceID)
			continue
		}
		textBudget -= len(text)
		found.Sources = append(found.Sources, source)
	}
	return found, nil
}

// AdaptiveGap decides by rule whether more retrieval could help, so that decision costs
// no provider call. Its query is the proposal's own statement of what is unresolved. A
// conflicted, blocked or abstained result is not a gap: more text must not paper over it.
func AdaptiveGap(proposal map[string]any) (string, bool) {
	summary, _ := proposal["summary"].(string)
	if proposal["disposition"] == "clarify" {
		return boundedQuery(summary)
	}
	assessment, ok := proposal["assessment"].(map[string]any)
	if !ok || assessment["applicability"] != "needs_evidence" {
		return "", false
	}
	missing := map[any]bool{}
	listed, _ := assessment["missing_prerequisites"].([]any)
	for _, id := range listed {
		missing[id] = true
	}
	statements := []string{}
	claims, _ := assessment["claims"].([]any)
	for _, claim := range claims {
		item, _ := claim.(map[string]any)
		statement, _ := item["statement"].(string)
		if missing[item["id"]] && statement != "" {
			statements = append(statements, statement)
		}
	}
	if len(statements) == 0 {
		return boundedQuery(summary)
	}
	return boundedQuery(strings.Join(statements, " "))
}

func boundedQuery(text string) (string, bool) {
	text = strings.TrimSpace(text)
	for len(text) > maxAdaptiveQueryBytes {
		_, size := utf8.DecodeLastRuneInString(text)
		text = text[:len(text)-size]
	}
	return text, text != ""
}
