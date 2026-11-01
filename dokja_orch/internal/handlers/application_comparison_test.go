package handlers

// Step 08 application comparison: the protocol frozen in dokja_docs/knowledge-applicability.md,
// for the fixed strategy. Every run goes through the real orchestrator path with the frozen corpus
// behind a stand-in Knowledge, so nothing is written anywhere. All of it is recorded, failures and
// raw replies included, so a run cannot quietly disappear from the count.
//
// The structure always runs, with a stand-in provider. A live run needs a Runtime with a measured
// provider, and writes one JSON line per run plus a sheet for the semantic review:
//
//	DOKJA_APPLICATION_COMPARE=/some/dir go test ./internal/handlers -run TestApplicationComparison -v -timeout 60m
//
// Only the development cases run by default. The held-out cases are for one comparison, never for
// tuning a prompt: DOKJA_APPLICATION_SCOPE=all includes them.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	knowledge "read_books/dokja_domain/dokja_knowledge"
	"read_books/internal/clients"
	"read_books/internal/core"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	comparisonFixtures = "../../../dokja_domain/dokja_knowledge/testdata/application"
	applicationRepeats = 2
	comparisonStrategy = "fixed"
	comparisonRunLimit = 90 * time.Second
	// The protocol's number for this strategy: one provider call per run and nothing hidden.
	comparisonFixedCalls = 1
)

type applicationCorpus struct {
	Sources []struct {
		SourceID  string `json:"source_id"`
		SourceRef string `json:"source_ref"`
		Text      string `json:"text"`
	} `json:"sources"`
}

type applicationCase struct {
	ID       string   `json:"id"`
	Sources  []string `json:"sources"`
	Goal     string   `json:"goal"`
	Expected string   `json:"expected"`
}

type applicationCases struct {
	Development []applicationCase `json:"development"`
	HeldOut     []applicationCase `json:"held_out"`
}

// applicationRun is one line of the record. Expected stays out of everything the provider sees.
type applicationRun struct {
	Case        string         `json:"case"`
	Set         string         `json:"set"`
	Strategy    string         `json:"strategy"`
	Repeat      int            `json:"repeat"`
	Status      string         `json:"status"`
	Reason      string         `json:"reason,omitempty"`
	Calls       int            `json:"calls"`
	ElapsedMS   int64          `json:"elapsed_ms"`
	Inference   map[string]any `json:"inference,omitempty"`
	Disposition string         `json:"disposition,omitempty"`
	Summary     string         `json:"summary,omitempty"`
	Proposal    any            `json:"proposal,omitempty"`
	RawReply    string         `json:"raw_reply,omitempty"`
	CallError   string         `json:"call_error,omitempty"`
}

// countingProposer accounts for every provider call, whether or not the workflow keeps its result.
type countingProposer struct {
	inner knowledge.ApplicationProposer
	mu    sync.Mutex
	calls int
	raw   string
	err   string
}

func (c *countingProposer) Propose(ctx context.Context, prompt knowledge.ProposalPrompt) (knowledge.ProposalResponse, error) {
	response, err := c.inner.Propose(ctx, prompt)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	c.raw = response.Body
	if err != nil {
		c.err = err.Error()
	}
	return response, err
}

func loadComparison(t *testing.T) (applicationCorpus, applicationCases, map[string]string) {
	t.Helper()
	hashes := map[string]string{}
	read := func(name string, into any) {
		raw, err := os.ReadFile(filepath.Join(comparisonFixtures, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, into); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(raw)
		hashes[name] = hex.EncodeToString(sum[:])
	}
	var corpus applicationCorpus
	var cases applicationCases
	read("corpus_v1.json", &corpus)
	read("cases_v1.json", &cases)
	return corpus, cases, hashes
}

func corpusStore(corpus applicationCorpus) *applicationStore {
	documents := make(map[string]string, len(corpus.Sources))
	for _, source := range corpus.Sources {
		documents[source.SourceID] = source.Text
	}
	return &applicationStore{documents: documents}
}

func runComparisonCase(t *testing.T, proposer knowledge.ApplicationProposer, store *applicationStore, set string, item applicationCase, repeat int) applicationRun {
	t.Helper()
	counted := &countingProposer{inner: proposer}
	selected := make([]any, len(item.Sources))
	for i, id := range item.Sources {
		selected[i] = map[string]any{"source_id": id}
	}
	event := core.Event{
		EventID: fmt.Sprintf("comparison-%s-%d", item.ID, repeat), Source: core.SourceCLI,
		Type:    "knowledge.application.propose.measured",
		Payload: map[string]any{"contract": knowledge.ApplicationContract, "goal": item.Goal, "sources": selected},
	}
	handler := NewKnowledgeDomainHandler(store).WithMeasuredApplicationProposer(counted)
	ctx, cancel := context.WithTimeout(context.Background(), comparisonRunLimit)
	defer cancel()
	started := time.Now()
	run := applicationRun{Case: item.ID, Set: set, Strategy: comparisonStrategy, Repeat: repeat}
	result, err := core.DefaultService(handler).ProcessWithResult(ctx, event)
	run.ElapsedMS = time.Since(started).Milliseconds()
	run.Calls, run.RawReply, run.CallError = counted.calls, counted.raw, counted.err
	if err != nil {
		run.Status, run.Reason = "failed", err.Error()
		return run
	}
	reply, _ := result.Result["knowledge"].(map[string]any)
	run.Status, _ = reply["status"].(string)
	if task, ok := reply["task"].(core.TaskResult); ok {
		run.Reason = task.Reason
	}
	run.Inference, _ = reply["inference"].(map[string]any)
	if proposal, ok := reply["proposal"].(map[string]any); ok {
		run.Disposition, _ = proposal["disposition"].(string)
		run.Summary, _ = proposal["summary"].(string)
		run.Proposal = proposal["proposal"]
		if inference, ok := proposal["inference"].(map[string]any); ok {
			run.Inference = inference
		}
	}
	return run
}

// comparisonIntegrity is what the protocol blocks on and a machine can tell. Whether an answer is
// right is the semantic review's to say: nothing here reads the expected text.
func comparisonIntegrity(run applicationRun) []string {
	problems := []string{}
	if run.Calls > comparisonFixedCalls {
		problems = append(problems, fmt.Sprintf("%d provider calls", run.Calls))
	}
	if run.ElapsedMS > comparisonRunLimit.Milliseconds() {
		problems = append(problems, "over the time ceiling")
	}
	if run.Status != "proposed" {
		problems = append(problems, "no proposal: "+run.Reason)
	}
	return problems
}

func reviewSheet(runs []applicationRun, expected map[string]string) string {
	var sheet strings.Builder
	sheet.WriteString(`# Semantic review

Fill this in without having written the proposals. A run passes only with every box ticked.
`)
	for _, run := range runs {
		fmt.Fprintf(&sheet, "\n## %s, %s, repeat %d\n\n", run.Case, run.Strategy, run.Repeat)
		fmt.Fprintf(&sheet, "Expected: %s\n\nStatus: %s %s\n\nDisposition: %s\n\nSummary: %s\n\n", expected[run.Case], run.Status, run.Reason, run.Disposition, run.Summary)
		sheet.WriteString(`- [ ] Correct disposition
- [ ] All necessary prerequisites
- [ ] Supported method and limits, with citations
- [ ] Explicit uncertainty
- [ ] No invented outcome
`)
	}
	return sheet.String()
}

func applicationSets(cases applicationCases, scope string) (map[string][]applicationCase, []string) {
	if scope == "all" {
		return map[string][]applicationCase{"development": cases.Development, "held_out": cases.HeldOut}, []string{"development", "held_out"}
	}
	return map[string][]applicationCase{"development": cases.Development}, []string{"development"}
}

func runApplicationComparison(t *testing.T, proposer knowledge.ApplicationProposer, corpus applicationCorpus, cases applicationCases, scope string) []applicationRun {
	t.Helper()
	sets, order := applicationSets(cases, scope)
	store := corpusStore(corpus)
	runs := []applicationRun{}
	for _, set := range order {
		for _, item := range sets[set] {
			for repeat := 1; repeat <= applicationRepeats; repeat++ {
				runs = append(runs, runComparisonCase(t, proposer, store, set, item, repeat))
			}
		}
	}
	return runs
}

func TestApplicationComparisonAccountsForEveryRun(t *testing.T) {
	corpus, cases, _ := loadComparison(t)
	abstains := applicationProposerFunc(func(_ context.Context, prompt knowledge.ProposalPrompt) (knowledge.ProposalResponse, error) {
		var input struct {
			TaskID string `json:"task_id"`
		}
		if err := json.Unmarshal([]byte(prompt.Data), &input); err != nil {
			t.Fatal(err)
		}
		for _, item := range append(append([]applicationCase{}, cases.Development...), cases.HeldOut...) {
			if strings.Contains(prompt.Data, item.Expected) {
				t.Fatalf("the provider saw the expected answer of %s", item.ID)
			}
		}
		body, err := json.Marshal(knowledge.ProviderProposal{Contract: knowledge.ProposalContract, TaskID: input.TaskID, Revision: 1, Disposition: "abstain", Summary: "Not enough."})
		if err != nil {
			t.Fatal(err)
		}
		return knowledge.ProposalResponse{Body: string(body), Provider: "fixture", RequestedModel: "fixture-model"}, nil
	})

	runs := runApplicationComparison(t, abstains, corpus, cases, "all")

	if want := (len(cases.Development) + len(cases.HeldOut)) * applicationRepeats; len(runs) != want || want != 16 {
		t.Fatalf("runs=%d want=%d", len(runs), want)
	}
	for _, run := range runs {
		if problems := comparisonIntegrity(run); len(problems) != 0 {
			t.Fatalf("%s: %v", run.Case, problems)
		}
		if run.Disposition != "abstain" || run.RawReply == "" {
			t.Fatalf("%+v", run)
		}
	}
}

func TestApplicationComparisonKeepsAFailedCall(t *testing.T) {
	corpus, cases, _ := loadComparison(t)
	failing := applicationProposerFunc(func(context.Context, knowledge.ProposalPrompt) (knowledge.ProposalResponse, error) {
		return knowledge.ProposalResponse{BudgetStatus: "over_budget"}, fmt.Errorf("measured inference exceeds acceptance budget")
	})
	run := runComparisonCase(t, failing, corpusStore(corpus), "development", cases.Development[0], 1)
	if run.Status != "not_proposed" || run.Calls != 1 || run.CallError == "" || run.Reason != "token_budget_exceeded" {
		t.Fatalf("%+v", run)
	}
	if problems := comparisonIntegrity(run); len(problems) == 0 {
		t.Fatal("a run without a proposal is a recorded failure")
	}
}

func TestApplicationComparisonLeavesHeldOutCasesOutByDefault(t *testing.T) {
	_, cases, _ := loadComparison(t)
	sets, _ := applicationSets(cases, "")
	if _, included := sets["held_out"]; included || len(sets["development"]) != 2 {
		t.Fatalf("sets: %v", sets)
	}
}

func TestApplicationComparisonLive(t *testing.T) {
	directory := os.Getenv("DOKJA_APPLICATION_COMPARE")
	if directory == "" {
		t.Skip("set DOKJA_APPLICATION_COMPARE to a directory to run it against a provider")
	}
	corpus, cases, hashes := loadComparison(t)
	expected := map[string]string{}
	for _, item := range append(append([]applicationCase{}, cases.Development...), cases.HeldOut...) {
		expected[item.ID] = item.Expected
	}
	scope := os.Getenv("DOKJA_APPLICATION_SCOPE")
	proposer := clients.NewMeasuredKnowledgeProposalClient(clients.NewChatProviderClient(""))
	runs := runApplicationComparison(t, proposer, corpus, cases, scope)

	var record strings.Builder
	header, err := json.Marshal(map[string]any{"protocol": "knowledge-applicability.md", "scope": scope, "fixtures": hashes, "strategy": comparisonStrategy})
	if err != nil {
		t.Fatal(err)
	}
	record.Write(append(header, '\n'))
	failed := 0
	for _, run := range runs {
		line, err := json.Marshal(run)
		if err != nil {
			t.Fatal(err)
		}
		record.Write(append(line, '\n'))
		if problems := comparisonIntegrity(run); len(problems) != 0 {
			failed++
			t.Logf("%s repeat %d: %v", run.Case, run.Repeat, problems)
		}
	}
	if err := os.WriteFile(filepath.Join(directory, "runs.jsonl"), []byte(record.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "review.md"), []byte(reviewSheet(runs, expected)), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Logf("%d runs, %d with an integrity problem", len(runs), failed)
}
