package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	knowledge "read_books/dokja_domain/dokja_knowledge"
	"read_books/internal/core"
	"read_books/internal/handlers"
	"strings"
	"testing"
)

const fixtureDir = "../../../dokja_domain/dokja_knowledge/testdata/application"

type inferenceFunc func(context.Context, knowledge.ProposalPrompt) (knowledge.ProposalResponse, error)

func (f inferenceFunc) Propose(ctx context.Context, prompt knowledge.ProposalPrompt) (knowledge.ProposalResponse, error) {
	return f(ctx, prompt)
}

func (f inferenceFunc) Review(ctx context.Context, prompt knowledge.ProposalPrompt) (knowledge.ProposalResponse, error) {
	return f(ctx, prompt)
}

func measuredReply(t *testing.T, value any) knowledge.ProposalResponse {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	input, output := int64(100), int64(10)
	return knowledge.ProposalResponse{Body: string(raw), Provider: "fixture", RequestedModel: "fixture-model", InputTokens: &input, OutputTokens: &output, BudgetMode: "post_generation", BudgetStatus: "ok"}
}

// fixtureInference answers through real handlers: propose for one case, clarify for the
// other, and an accepted review, so the whole chain runs without a model.
func fixtureInference(t *testing.T, calls *[]string) inferenceFunc {
	return func(_ context.Context, prompt knowledge.ProposalPrompt) (knowledge.ProposalResponse, error) {
		var data struct {
			Contract    string `json:"contract"`
			TaskID      string `json:"task_id"`
			Application struct {
				Goal    string           `json:"goal"`
				Sources []map[string]any `json:"sources"`
			} `json:"application"`
		}
		if err := json.Unmarshal([]byte(prompt.Data), &data); err != nil {
			t.Fatal(err)
		}
		*calls = append(*calls, data.Contract)
		if data.Contract == knowledge.ReviewContract {
			return measuredReply(t, knowledge.ProviderReview{
				Contract: knowledge.ReviewContract, TaskID: data.TaskID, Revision: 1,
				RequiredConditions: []knowledge.ReviewCondition{{ID: "key", Statement: "The key is valid", CoveredBy: []string{"claim"}}},
				Claims:             []knowledge.ReviewFinding{{ID: "claim", Verdict: "supported", Reason: "stated"}},
				Method:             knowledge.ReviewFinding{ID: "method", Verdict: "supported", Reason: "stated"},
				Conflicts:          []knowledge.ReviewConflict{}, Verdict: "accepted", Reason: "fixture",
			}), nil
		}
		reply := knowledge.ProviderProposal{Contract: knowledge.ProposalContract, TaskID: data.TaskID, Revision: 1, Disposition: "clarify", Summary: "Need the key age"}
		if !strings.Contains(data.Application.Goal, "were not supplied") {
			version := data.Application.Sources[0]["text_version"].(string)
			id := data.Application.Sources[0]["source_id"].(string)
			quote := knowledge.QuotedCitation{SourceID: id, TextVersion: version, Quote: "Use bounded exponential backoff with jitter.", Relation: "supports"}
			reply.Disposition, reply.Summary = "propose", "Retry"
			reply.Proposal = &knowledge.QuotedProposal{
				Claims: []knowledge.QuotedClaim{{ID: "claim", Statement: "Backoff applies", Citations: []knowledge.QuotedCitation{quote}}},
				Method: knowledge.QuotedMethod{ID: "method", Description: "Back off", Prerequisites: []string{"claim"}, Citations: []knowledge.QuotedCitation{quote}},
			}
		}
		return measuredReply(t, reply), nil
	}
}

func handlerProcessor(inference inferenceFunc) processor {
	return func(ctx context.Context, store *fixtureStore, event core.Event) (map[string]any, error) {
		result, err := core.DefaultService(handlers.NewKnowledgeDomainHandler(store).WithMeasuredApplicationProposer(inference).WithApplicationReviewer(inference)).ProcessWithResult(ctx, event)
		if err != nil {
			return nil, err
		}
		return result.Result["knowledge"].(map[string]any), nil
	}
}

func testOptions(t *testing.T) options {
	return options{fixtures: fixtureDir, split: "development", strategy: "fixed", out: filepath.Join(t.TempDir(), "report.jsonl"), runs: 2, review: true}
}

func readRecords(t *testing.T, path string) []runRecord {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	records := []runRecord{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		var record runRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	return records
}

func TestComparisonRunsThroughTheRealApplicationPath(t *testing.T) {
	calls := []string{}
	o := testOptions(t)
	result, err := run(t.Context(), o, handlerProcessor(fixtureInference(t, &calls)))
	if err != nil {
		t.Fatal(err)
	}
	if result.Runs != 4 || result.StrategyCalls != 4 || result.ReviewCalls != 2 || result.Rubric != "pending" || result.UnknownUsage != 0 || result.InputTokens != 600 {
		t.Fatalf("%+v", result)
	}
	supported, missing := result.Cases["dev-supported"], result.Cases["dev-missing"]
	if supported.Dispositions["propose"] != 2 || supported.Reviews["accepted"] != 2 || missing.Dispositions["clarify"] != 2 || len(missing.Reviews) != 0 {
		t.Fatalf("%+v %+v", supported, missing)
	}
	records := readRecords(t, o.out)
	if len(records) != 4 || records[0].CorpusDigest != frozenCorpusDigest || records[0].ProtocolRevision != 2 || records[0].Rubric != "pending" {
		t.Fatalf("%+v", records)
	}
	if records[0].Review == nil || records[0].Review.Review["independence"] != "same_model" {
		t.Fatalf("%+v", records[0].Review)
	}
}

func TestComparisonReportsFailuresAndUnknownUsage(t *testing.T) {
	o := testOptions(t)
	o.runs = 1
	failing := inferenceFunc(func(context.Context, knowledge.ProposalPrompt) (knowledge.ProposalResponse, error) {
		return knowledge.ProposalResponse{Provider: "fixture", RequestedModel: "fixture-model", BudgetStatus: "provider_error"}, fmt.Errorf("private diagnostic")
	})
	result, err := run(t.Context(), o, handlerProcessor(failing))
	if err != nil {
		t.Fatal(err)
	}
	if result.Runs != 2 || result.StrategyCalls != 2 || result.UnknownUsage != 2 || result.InputTokens != 0 || result.Cases["dev-missing"].Statuses["not_proposed"] != 1 {
		t.Fatalf("%+v", result)
	}
	for _, record := range readRecords(t, o.out) {
		if record.Status != "not_proposed" || record.Reason != "provider_unavailable" || record.Proposal != nil || strings.Contains(fmt.Sprint(record), "private diagnostic") {
			t.Fatalf("%+v", record)
		}
	}
}

func TestComparisonRefusesUnfrozenOrUnsafeRuns(t *testing.T) {
	never := processor(func(context.Context, *fixtureStore, core.Event) (map[string]any, error) {
		t.Error("run started")
		return nil, nil
	})
	changed := t.TempDir()
	for _, name := range []string{"cases_v1.json", "corpus_v1.json"} {
		raw, err := os.ReadFile(filepath.Join(fixtureDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if name == "cases_v1.json" {
			raw = append(raw, '\n')
		}
		if err := os.WriteFile(filepath.Join(changed, name), raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	existing := filepath.Join(t.TempDir(), "report.jsonl")
	if err := os.WriteFile(existing, []byte("earlier result"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*options){
		"held-out without consent": func(o *options) { o.split = "held_out" },
		"partial held-out":         func(o *options) { o.split, o.allowHeldOut, o.caseID = "held_out", true, "held-1" },
		"missing strategy":         func(o *options) { o.strategy = "adaptive" },
		"changed fixture":          func(o *options) { o.fixtures = changed },
		"overwritten report":       func(o *options) { o.out = existing },
		"unknown case":             func(o *options) { o.caseID = "foreign" },
		"call ceiling":             func(o *options) { o.runs = 33 },
		"no report":                func(o *options) { o.out = "" },
	} {
		t.Run(name, func(t *testing.T) {
			o := testOptions(t)
			mutate(&o)
			if _, err := run(t.Context(), o, never); err == nil {
				t.Fatal("run accepted")
			}
		})
	}
	if raw, _ := os.ReadFile(existing); string(raw) != "earlier result" {
		t.Fatal("earlier report changed")
	}
}

func TestFixtureStoreRefusesAnyOtherKnowledgeOperation(t *testing.T) {
	store := &fixtureStore{documents: map[string]fixtureDocument{}}
	if _, err := store.Dispatch(t.Context(), "knowledge.ingest", map[string]any{}); err == nil {
		t.Fatal("corpus mutation allowed")
	}
}
