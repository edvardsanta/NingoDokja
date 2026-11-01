package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	knowledge "read_books/dokja_domain/dokja_knowledge"
	"read_books/internal/core"
	"time"
)

const reportContract = "knowledge.application.comparison.run.v1"
const protocolRevision = 2
const frozenRuns = 2
const maxStrategyCalls = 64

// Digests of the fixtures frozen in 8b9f331. A changed fixture is a new corpus or case
// version with separate results, never a silent input to this harness.
const frozenCasesDigest = "sha256:b14281583d3dfb877cbbee3205684eb59b67fa904c745dcbe4f613823d1f7197"
const frozenCorpusDigest = "sha256:ceb5ec1712d6440352f59975d1cd87e58ad319f12139aa0245b52233d7d69b5e"

type options struct {
	fixtures, split, strategy, out, caseID string
	runs                                   int
	review, allowHeldOut                   bool
}

type comparisonCase struct {
	ID       string   `json:"id"`
	Sources  []string `json:"sources"`
	Goal     string   `json:"goal"`
	Expected string   `json:"expected"`
}

type fixtures struct {
	cases     []comparisonCase
	documents map[string]fixtureDocument
}

type fixtureDocument struct {
	SourceID  string `json:"source_id"`
	SourceRef string `json:"source_ref"`
	Text      string `json:"text"`
}

// fixtureStore serves the frozen corpus at the Knowledge port, so no run can read or
// change the operator's Library.
type fixtureStore struct{ documents map[string]fixtureDocument }

func (s *fixtureStore) Dispatch(_ context.Context, event string, payload map[string]any) (map[string]any, error) {
	if event != "knowledge.list" {
		return nil, fmt.Errorf("comparison refuses Knowledge operation %s", event)
	}
	id, _ := payload["source_id"].(string)
	document, ok := s.documents[id]
	if !ok {
		return map[string]any{"documents": []any{}}, nil
	}
	return map[string]any{"documents": []any{map[string]any{"source_id": id, "source_ref": document.SourceRef, "title": id, "text": document.Text}}}, nil
}

// runRecord keeps failures as complete as successes. An unknown count stays null.
type runRecord struct {
	Contract         string         `json:"contract"`
	ProtocolRevision int            `json:"protocol_revision"`
	Split            string         `json:"split"`
	Strategy         string         `json:"strategy"`
	CaseID           string         `json:"case_id"`
	Run              int            `json:"run"`
	CasesDigest      string         `json:"cases_digest"`
	CorpusDigest     string         `json:"corpus_digest"`
	StartedAt        time.Time      `json:"started_at"`
	ElapsedMS        int64          `json:"elapsed_ms"`
	Status           string         `json:"status"`
	Reason           string         `json:"reason,omitempty"`
	Error            string         `json:"error,omitempty"`
	Inference        any            `json:"inference"`
	Proposal         map[string]any `json:"proposal,omitempty"`
	Review           *reviewRecord  `json:"review,omitempty"`
	Rubric           string         `json:"rubric"`
}

type reviewRecord struct {
	Status    string         `json:"status"`
	Reason    string         `json:"reason,omitempty"`
	Error     string         `json:"error,omitempty"`
	ElapsedMS int64          `json:"elapsed_ms"`
	Inference any            `json:"inference"`
	Review    map[string]any `json:"review,omitempty"`
}

type caseSummary struct {
	Runs          int            `json:"runs"`
	Statuses      map[string]int `json:"statuses"`
	Dispositions  map[string]int `json:"dispositions"`
	Applicability map[string]int `json:"applicability"`
	Reviews       map[string]int `json:"reviews"`
}

type summary struct {
	ProtocolRevision int                     `json:"protocol_revision"`
	Split            string                  `json:"split"`
	Strategy         string                  `json:"strategy"`
	Report           string                  `json:"report"`
	Runs             int                     `json:"runs"`
	StrategyCalls    int                     `json:"strategy_calls"`
	ReviewCalls      int                     `json:"review_calls"`
	UnknownUsage     int                     `json:"unknown_usage_calls"`
	InputTokens      int64                   `json:"input_tokens"`
	OutputTokens     int64                   `json:"output_tokens"`
	Cases            map[string]*caseSummary `json:"cases"`
	Rubric           string                  `json:"rubric"`
}

type processor func(context.Context, *fixtureStore, core.Event) (map[string]any, error)

func validate(o options) error {
	if o.strategy != "fixed" {
		return fmt.Errorf("strategy %q does not exist yet; compare only implemented strategies", o.strategy)
	}
	if o.split != "development" && o.split != "held_out" {
		return fmt.Errorf("unknown split %q", o.split)
	}
	if o.split == "held_out" && !o.allowHeldOut {
		return fmt.Errorf("held_out cases are for the frozen comparison only; pass -allow-held-out to run them")
	}
	if o.split == "held_out" && (o.runs != frozenRuns || o.caseID != "") {
		return fmt.Errorf("held_out runs are every case, %d times each", frozenRuns)
	}
	if o.out == "" {
		return fmt.Errorf("-out is required")
	}
	if o.runs < 1 {
		return fmt.Errorf("-runs must be positive")
	}
	return nil
}

func loadFixtures(dir, split, caseID string) (fixtures, string, string, error) {
	read := func(name, frozen string) ([]byte, string, error) {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, "", err
		}
		sum := sha256.Sum256(raw)
		digest := "sha256:" + hex.EncodeToString(sum[:])
		if digest != frozen {
			return nil, "", fmt.Errorf("%s differs from the frozen fixture", name)
		}
		return raw, digest, nil
	}
	rawCases, casesDigest, err := read("cases_v1.json", frozenCasesDigest)
	if err != nil {
		return fixtures{}, "", "", err
	}
	rawCorpus, corpusDigest, err := read("corpus_v1.json", frozenCorpusDigest)
	if err != nil {
		return fixtures{}, "", "", err
	}
	var splits struct {
		Development []comparisonCase `json:"development"`
		HeldOut     []comparisonCase `json:"held_out"`
	}
	if err := json.Unmarshal(rawCases, &splits); err != nil {
		return fixtures{}, "", "", err
	}
	var corpus struct {
		Sources []fixtureDocument `json:"sources"`
	}
	if err := json.Unmarshal(rawCorpus, &corpus); err != nil {
		return fixtures{}, "", "", err
	}
	loaded := fixtures{cases: splits.Development, documents: map[string]fixtureDocument{}}
	if split == "held_out" {
		loaded.cases = splits.HeldOut
	}
	if caseID != "" {
		selected := []comparisonCase{}
		for _, c := range loaded.cases {
			if c.ID == caseID {
				selected = append(selected, c)
			}
		}
		if len(selected) == 0 {
			return fixtures{}, "", "", fmt.Errorf("case %q is not in split %s", caseID, split)
		}
		loaded.cases = selected
	}
	for _, document := range corpus.Sources {
		loaded.documents[document.SourceID] = document
	}
	return loaded, casesDigest, corpusDigest, nil
}

func run(ctx context.Context, o options, process processor) (summary, error) {
	if err := validate(o); err != nil {
		return summary{}, err
	}
	loaded, casesDigest, corpusDigest, err := loadFixtures(o.fixtures, o.split, o.caseID)
	if err != nil {
		return summary{}, err
	}
	if len(loaded.cases)*o.runs > maxStrategyCalls {
		return summary{}, fmt.Errorf("run plan exceeds the %d provider call ceiling", maxStrategyCalls)
	}
	// O_EXCL keeps an earlier report: results are never overwritten or merged.
	report, err := os.OpenFile(o.out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return summary{}, err
	}
	defer report.Close()
	encoder := json.NewEncoder(report)
	store := &fixtureStore{documents: loaded.documents}
	result := summary{ProtocolRevision: protocolRevision, Split: o.split, Strategy: o.strategy, Report: o.out, Cases: map[string]*caseSummary{}, Rubric: "pending"}
	for _, c := range loaded.cases {
		tally := &caseSummary{Statuses: map[string]int{}, Dispositions: map[string]int{}, Applicability: map[string]int{}, Reviews: map[string]int{}}
		result.Cases[c.ID] = tally
		for number := 1; number <= o.runs; number++ {
			record := runCase(ctx, o, store, process, c, number)
			record.CasesDigest, record.CorpusDigest = casesDigest, corpusDigest
			// Written per run, so an interrupted comparison still accounts for what ran.
			if err := encoder.Encode(record); err != nil {
				return result, err
			}
			account(&result, tally, record)
		}
	}
	return result, nil
}

func runCase(ctx context.Context, o options, store *fixtureStore, process processor, c comparisonCase, number int) runRecord {
	started := time.Now()
	record := runRecord{Contract: reportContract, ProtocolRevision: protocolRevision, Split: o.split, Strategy: o.strategy, CaseID: c.ID, Run: number, StartedAt: started.UTC(), Rubric: "pending"}
	sources := make([]any, 0, len(c.Sources))
	for _, id := range c.Sources {
		sources = append(sources, map[string]any{"source_id": id})
	}
	event := comparisonEvent(fmt.Sprintf("%s-%d", c.ID, number), "knowledge.application.propose.measured", map[string]any{
		"contract": knowledge.ApplicationContract, "goal": c.Goal, "constraints": []any{}, "sources": sources,
	})
	output, err := process(ctx, store, event)
	record.ElapsedMS = time.Since(started).Milliseconds()
	if err != nil {
		record.Status, record.Error = "error", err.Error()
		return record
	}
	record.Status, _ = output["status"].(string)
	record.Reason = taskReason(output)
	record.Inference = output["inference"]
	proposal, _ := output["proposal"].(map[string]any)
	record.Proposal = proposal
	if proposal != nil {
		record.Inference = proposal["inference"]
	}
	if o.review {
		record.Review = reviewCase(ctx, store, process, c, number, proposal)
	}
	return record
}

func reviewCase(ctx context.Context, store *fixtureStore, process processor, c comparisonCase, number int, proposal map[string]any) *reviewRecord {
	assessment, ok := proposal["assessment"].(map[string]any)
	if !ok {
		return nil
	}
	sources := []any{}
	observed, _ := assessment["sources"].([]any)
	for _, source := range observed {
		item, _ := source.(map[string]any)
		sources = append(sources, map[string]any{"source_id": item["source_id"], "text_version": item["text_version"]})
	}
	inference, _ := proposal["inference"].(map[string]any)
	started := time.Now()
	event := comparisonEvent(fmt.Sprintf("%s-%d-review", c.ID, number), "knowledge.application.review", map[string]any{
		"contract": knowledge.ReviewRequestContract,
		"assessment": map[string]any{
			"contract":    knowledge.AssessmentContract,
			"application": map[string]any{"contract": knowledge.ApplicationContract, "goal": c.Goal, "constraints": []any{}, "sources": sources},
			"proposal":    map[string]any{"claims": candidateClaims(assessment), "method": assessment["method"]},
		},
		"proposed_by": map[string]any{"provider": inference["provider"], "requested_model": inference["requested_model"]},
	})
	output, err := process(ctx, store, event)
	record := &reviewRecord{ElapsedMS: time.Since(started).Milliseconds()}
	if err != nil {
		record.Status, record.Error = "error", err.Error()
		return record
	}
	record.Status, _ = output["status"].(string)
	record.Reason = taskReason(output)
	record.Inference = output["inference"]
	record.Review, _ = output["review"].(map[string]any)
	if record.Review != nil {
		record.Inference = record.Review["inference"]
	}
	return record
}

// candidateClaims drops what the assessor derived, keeping the claim as it was proposed.
func candidateClaims(assessment map[string]any) []any {
	claims := []any{}
	listed, _ := assessment["claims"].([]any)
	for _, claim := range listed {
		item, _ := claim.(map[string]any)
		claims = append(claims, map[string]any{"id": item["id"], "statement": item["statement"], "citations": item["citations"]})
	}
	return claims
}

func comparisonEvent(id, eventType string, payload map[string]any) core.Event {
	return core.Event{
		EventID: "comparison-" + id, Timestamp: time.Now().UTC(), Source: core.SourceCLI, Type: eventType,
		User: core.EventUser{ID: "comparison"}, Channel: core.EventChannel{ID: "comparison"}, Payload: payload,
	}
}

func taskReason(output map[string]any) string {
	task, ok := output["task"].(core.TaskResult)
	if !ok {
		return ""
	}
	return task.Reason
}

func account(total *summary, tally *caseSummary, record runRecord) {
	total.Runs++
	tally.Runs++
	tally.Statuses[record.Status]++
	total.StrategyCalls += countCalls(record.Inference)
	countUsage(total, record.Inference)
	if record.Proposal != nil {
		disposition, _ := record.Proposal["disposition"].(string)
		tally.Dispositions[disposition]++
		if assessment, ok := record.Proposal["assessment"].(map[string]any); ok {
			applicability, _ := assessment["applicability"].(string)
			tally.Applicability[applicability]++
		}
	}
	if record.Review == nil {
		return
	}
	outcome := record.Review.Status
	if record.Review.Review != nil {
		outcome, _ = record.Review.Review["outcome"].(string)
	}
	tally.Reviews[outcome]++
	total.ReviewCalls += countCalls(record.Review.Inference)
	countUsage(total, record.Review.Inference)
}

func countCalls(inference any) int {
	metadata, _ := inference.(map[string]any)
	calls, _ := metadata["calls"].(int)
	return calls
}

// countUsage never adds an unknown count as zero: it is reported on its own.
func countUsage(total *summary, inference any) {
	metadata, _ := inference.(map[string]any)
	input, inputKnown := metadata["input_tokens"].(int64)
	output, outputKnown := metadata["output_tokens"].(int64)
	if !inputKnown || !outputKnown {
		total.UnknownUsage++
		return
	}
	total.InputTokens += input
	total.OutputTokens += output
}
