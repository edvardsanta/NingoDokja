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
	"strings"
	"sync"
	"time"
)

const reportContract = "knowledge.application.comparison.run.v1"
const headerContract = "knowledge.application.comparison.header.v1"
const protocolRevision = 2
const frozenRuns = 2
const maxStrategyCalls = 64

// Digests of the fixtures frozen in 8b9f331. A changed fixture is a new corpus or case
// version with separate results, never a silent input to this harness.
const frozenCasesDigest = "sha256:b14281583d3dfb877cbbee3205684eb59b67fa904c745dcbe4f613823d1f7197"
const frozenCorpusDigest = "sha256:ceb5ec1712d6440352f59975d1cd87e58ad319f12139aa0245b52233d7d69b5e"

type options struct {
	fixtures, split, strategy, out, caseID string
	provider, model                        string
	runs                                   int
	review, allowHeldOut                   bool
}

// reportHeader is written before any provider call, so the binding a comparison claims
// to measure is on record first. Each run is then checked against it.
type reportHeader struct {
	Contract         string    `json:"contract"`
	ProtocolRevision int       `json:"protocol_revision"`
	Split            string    `json:"split"`
	Strategy         string    `json:"strategy"`
	Provider         string    `json:"provider"`
	Model            string    `json:"requested_model"`
	Cases            []string  `json:"cases"`
	Runs             int       `json:"runs_per_case"`
	CasesDigest      string    `json:"cases_digest"`
	CorpusDigest     string    `json:"corpus_digest"`
	StartedAt        time.Time `json:"started_at"`
}

type rawReply struct {
	Port  string `json:"port"`
	Body  string `json:"body"`
	Error string `json:"error,omitempty"`
}

// replyRecorder sits at the inference port, so a reply the workflow refused is kept too.
type replyRecorder struct {
	inner   inferencePort
	mu      sync.Mutex
	replies []rawReply
}

type inferencePort interface {
	knowledge.ApplicationProposer
	knowledge.ApplicationReviewer
}

func (r *replyRecorder) record(port string, response knowledge.ProposalResponse, err error) (knowledge.ProposalResponse, error) {
	reply := rawReply{Port: port, Body: response.Body}
	if err != nil {
		reply.Error = err.Error()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.replies = append(r.replies, reply)
	return response, err
}

func (r *replyRecorder) Propose(ctx context.Context, prompt knowledge.ProposalPrompt) (knowledge.ProposalResponse, error) {
	response, err := r.inner.Propose(ctx, prompt)
	return r.record("propose", response, err)
}

func (r *replyRecorder) Review(ctx context.Context, prompt knowledge.ProposalPrompt) (knowledge.ProposalResponse, error) {
	response, err := r.inner.Review(ctx, prompt)
	return r.record("review", response, err)
}

func (r *replyRecorder) drain() []rawReply {
	r.mu.Lock()
	defer r.mu.Unlock()
	replies := r.replies
	r.replies = nil
	return replies
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
	RawReplies       []rawReply     `json:"raw_replies"`
	BindingMismatch  bool           `json:"binding_mismatch,omitempty"`
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
	ReviewSheet      string                  `json:"review_sheet"`
	Runs             int                     `json:"runs"`
	StrategyCalls    int                     `json:"strategy_calls"`
	ReviewCalls      int                     `json:"review_calls"`
	UnknownUsage     int                     `json:"unknown_usage_calls"`
	BindingMismatch  int                     `json:"binding_mismatches"`
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
	if o.split == "held_out" && (o.provider == "" || o.model == "") {
		return fmt.Errorf("held_out runs need -provider and -model recorded before the first call")
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

func run(ctx context.Context, o options, process processor, recorder *replyRecorder) (summary, error) {
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
	header := reportHeader{Contract: headerContract, ProtocolRevision: protocolRevision, Split: o.split, Strategy: o.strategy, Provider: o.provider, Model: o.model, Runs: o.runs, CasesDigest: casesDigest, CorpusDigest: corpusDigest, StartedAt: time.Now().UTC()}
	for _, c := range loaded.cases {
		header.Cases = append(header.Cases, c.ID)
	}
	if err := encoder.Encode(header); err != nil {
		return summary{}, err
	}
	store := &fixtureStore{documents: loaded.documents}
	sheetPath := strings.TrimSuffix(o.out, filepath.Ext(o.out)) + ".review.md"
	result := summary{ProtocolRevision: protocolRevision, Split: o.split, Strategy: o.strategy, Report: o.out, ReviewSheet: sheetPath, Cases: map[string]*caseSummary{}, Rubric: "pending"}
	sheet := reviewSheet{}
	for _, c := range loaded.cases {
		tally := &caseSummary{Statuses: map[string]int{}, Dispositions: map[string]int{}, Applicability: map[string]int{}, Reviews: map[string]int{}}
		result.Cases[c.ID] = tally
		for number := 1; number <= o.runs; number++ {
			record := runCase(ctx, o, store, process, c, number)
			record.CasesDigest, record.CorpusDigest = casesDigest, corpusDigest
			record.RawReplies = recorder.drain()
			record.BindingMismatch = bindingMismatch(o, record)
			// Written per run, so an interrupted comparison still accounts for what ran.
			if err := encoder.Encode(record); err != nil {
				return result, err
			}
			account(&result, tally, record)
			sheet.add(c, record)
		}
	}
	// Exclusive for the same reason as the report: a sheet may already hold human scores.
	if err := writeNew(sheetPath, sheet.String()); err != nil {
		return result, err
	}
	return result, nil
}

func bindingMismatch(o options, record runRecord) bool {
	metadata, _ := record.Inference.(map[string]any)
	provider, _ := metadata["provider"].(string)
	model, _ := metadata["requested_model"].(string)
	if o.provider == "" || provider == "" {
		return false
	}
	return provider != o.provider || model != o.model
}

func writeNew(path, content string) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.WriteString(content)
	return err
}

// reviewSheet is for a scorer who did not write the proposals. It holds the expected
// answer, which never reaches a model, and leaves out the model reviewer's verdict.
type reviewSheet struct{ text strings.Builder }

func (s *reviewSheet) String() string {
	return `# Semantic review

Fill this in without having written the proposals. A run passes only with every box ticked.
` + s.text.String()
}

func (s *reviewSheet) add(c comparisonCase, record runRecord) {
	disposition, summaryText := "", ""
	if record.Proposal != nil {
		disposition, _ = record.Proposal["disposition"].(string)
		summaryText, _ = record.Proposal["summary"].(string)
	}
	fmt.Fprintf(&s.text, "\n## %s, %s, run %d\n\nExpected: %s\n\nStatus: %s %s\n\nDisposition: %s\n\nSummary: %s\n\n", c.ID, record.Strategy, record.Run, c.Expected, record.Status, record.Reason, disposition, summaryText)
	if assessment, ok := record.Proposal["assessment"].(map[string]any); ok {
		method, _ := assessment["method"].(map[string]any)
		fmt.Fprintf(&s.text, "Method: %v\n\n", method["description"])
		claims, _ := assessment["claims"].([]any)
		for _, claim := range claims {
			item, _ := claim.(map[string]any)
			fmt.Fprintf(&s.text, "- Claim (%v): %v\n", item["citation_state"], item["statement"])
		}
		s.text.WriteString("\n")
	}
	s.text.WriteString(`- [ ] Correct disposition
- [ ] All necessary prerequisites
- [ ] Supported method and limits, with citations
- [ ] Explicit uncertainty
- [ ] No invented outcome
`)
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
	if record.BindingMismatch {
		total.BindingMismatch++
	}
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
