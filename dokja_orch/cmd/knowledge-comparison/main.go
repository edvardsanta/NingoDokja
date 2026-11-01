// Command knowledge-comparison runs the frozen Knowledge application cases through the
// real orchestrator path and records every run for later rubric scoring. It reads only
// the frozen fixtures, writes only its own report and takes no external action.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"read_books/internal/clients"
	"read_books/internal/core"
	"read_books/internal/handlers"
)

func main() {
	options := options{}
	flag.StringVar(&options.fixtures, "fixtures", "../dokja_domain/dokja_knowledge/testdata/application", "frozen fixture directory")
	flag.StringVar(&options.split, "split", "development", "development or held_out")
	flag.StringVar(&options.strategy, "strategy", "fixed", "comparison strategy: fixed or adaptive")
	flag.StringVar(&options.out, "out", "", "new JSONL report file")
	flag.StringVar(&options.caseID, "case", "", "run only this case")
	flag.StringVar(&options.provider, "provider", "", "provider expected from Runtime, recorded before the first call")
	flag.StringVar(&options.model, "model", "", "model expected from Runtime, recorded before the first call")
	flag.IntVar(&options.runs, "runs", frozenRuns, "independent runs per case")
	flag.BoolVar(&options.review, "review", true, "also ask for a semantic review, reported apart from strategy calls")
	flag.BoolVar(&options.allowHeldOut, "allow-held-out", false, "required for the held_out split, which is not for tuning")
	flag.Parse()

	recorder := &replyRecorder{inner: clients.NewMeasuredKnowledgeProposalClient(clients.NewChatProviderClient(""))}
	process := func(ctx context.Context, store *fixtureStore, event core.Event) (map[string]any, error) {
		service := core.DefaultService(handlers.NewKnowledgeDomainHandler(store).WithMeasuredApplicationProposer(recorder).WithApplicationReviewer(recorder))
		result, err := service.ProcessWithResult(ctx, event)
		if err != nil {
			return nil, err
		}
		output, ok := result.Result["knowledge"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("missing Knowledge result")
		}
		return output, nil
	}
	summary, err := run(context.Background(), options, process, recorder)
	if err != nil {
		fmt.Fprintln(os.Stderr, "knowledge-comparison:", err)
		os.Exit(1)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(summary); err != nil {
		os.Exit(1)
	}
}
