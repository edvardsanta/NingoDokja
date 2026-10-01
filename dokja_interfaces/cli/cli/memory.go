package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func (a *App) memoryCall(ctx context.Context, eventType string, payload map[string]any) (map[string]any, error) {
	response, err := a.call(ctx, EmitOptions{
		Type:      eventType,
		UserID:    a.emitOpts.userID,
		UserName:  a.emitOpts.userName,
		ChannelID: a.emitOpts.channelID,
		Payload:   payload,
		Context:   map[string]any{"interface": "cli"},
		Source:    "cli",
	})
	if err != nil {
		return nil, err
	}
	if response.Status != "ok" {
		return nil, fmt.Errorf("%s", firstNonEmpty(response.Error, "orchestrator returned "+response.Status))
	}
	answer, skipped, err := domainResult(response.Result, "memory")
	if err != nil {
		return nil, err
	}
	if skipped != "" {
		return nil, fmt.Errorf("experience memory is off: %s", skipped)
	}
	return answer, nil
}

func joinWords(words []string) string {
	return strings.TrimSpace(strings.Join(words, " "))
}

func buildMemoryRecordPayload(ref, action, detail string, contextWords []string) (map[string]any, error) {
	payload := map[string]any{
		"ref": strings.TrimSpace(ref), "action": strings.TrimSpace(action), "context": joinWords(contextWords),
	}
	for _, field := range []string{"ref", "action", "context"} {
		if payload[field] == "" {
			return nil, fmt.Errorf("%s is required", field)
		}
	}
	if detail = strings.TrimSpace(detail); detail != "" {
		payload["detail"] = detail
	}
	return payload, nil
}

func buildMemoryRecallPayload(contextWords []string, action string, k int) (map[string]any, error) {
	if joinWords(contextWords) == "" {
		return nil, fmt.Errorf("context is required")
	}
	if k < 1 || k > 50 {
		return nil, fmt.Errorf("k must be between 1 and 50")
	}
	payload := map[string]any{"context": joinWords(contextWords), "k": k}
	if action = strings.TrimSpace(action); action != "" {
		payload["action"] = action
	}
	return payload, nil
}

func buildMemoryPredictPayload(contextWords []string, action string) (map[string]any, error) {
	if strings.TrimSpace(action) == "" {
		return nil, fmt.Errorf("action is required: a prediction is about one action")
	}
	if joinWords(contextWords) == "" {
		return nil, fmt.Errorf("context is required")
	}
	return map[string]any{"context": joinWords(contextWords), "action": strings.TrimSpace(action)}, nil
}

func buildMemoryResolvePayload(ref, outcome, observed string) (map[string]any, error) {
	ref, outcome, observed = strings.TrimSpace(ref), strings.TrimSpace(outcome), strings.TrimSpace(observed)
	switch {
	case ref == "":
		return nil, fmt.Errorf("ref is required")
	case outcome != "" && observed != "":
		return nil, fmt.Errorf("send either --outcome or --observed, not both")
	case outcome == "" && observed == "":
		return nil, fmt.Errorf("send --outcome (accepted, replaced, ignored or expired) or --observed (what actually happened)")
	case outcome != "":
		return map[string]any{"ref": ref, "outcome": outcome}, nil
	}
	return map[string]any{"ref": ref, "observed": observed}, nil
}

func (a *App) newMemoryCommand(ctx context.Context) *cobra.Command {
	command := &cobra.Command{
		Use:   "memory",
		Short: "Inspect the experience memory: what the bot did, how it turned out and what it predicts",
		Long: "An experience is \"in this context the bot took this action, and this was the outcome\".\n" +
			"The memory finds the closest earlier ones and estimates how likely an action is to be\n" +
			"accepted, then scores those estimates against a baseline once the outcomes are known.",
	}

	var recordAction, recordDetail string
	record := &cobra.Command{
		Use:   "record <ref> <context...>",
		Short: "Record an experience by hand: in this context the bot did this (memory.record)",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload, err := buildMemoryRecordPayload(args[0], recordAction, recordDetail, args[1:])
			if err != nil {
				return err
			}
			answer, err := a.memoryCall(ctx, "memory.record", payload)
			if err != nil {
				return err
			}
			return printJSON(answer)
		},
	}
	record.Flags().StringVar(&recordAction, "action", "", "What the bot did, as a short lowercase name such as hashtag.suggest")
	record.Flags().StringVar(&recordDetail, "detail", "", "What exactly it did, such as the hashtag it suggested")
	_ = record.MarkFlagRequired("action")

	var resolveOutcome, resolveObserved string
	resolve := &cobra.Command{
		Use:   "resolve <ref>",
		Short: "Say how an experience turned out (memory.resolve)",
		Long: "Give --outcome (accepted, replaced, ignored or expired) or --observed, what actually happened:\n" +
			"the experience is accepted when it is exactly what the bot did, and replaced otherwise.\n" +
			"The first verdict wins; resolving again changes nothing.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload, err := buildMemoryResolvePayload(args[0], resolveOutcome, resolveObserved)
			if err != nil {
				return err
			}
			answer, err := a.memoryCall(ctx, "memory.resolve", payload)
			if err != nil {
				return err
			}
			return printJSON(answer)
		},
	}
	resolve.Flags().StringVar(&resolveOutcome, "outcome", "", "accepted, replaced, ignored or expired")
	resolve.Flags().StringVar(&resolveObserved, "observed", "", "What actually happened, to compare with what the bot did")

	var recallAction string
	var recallK int
	recall := &cobra.Command{
		Use:   "recall <context...>",
		Short: "Find the closest earlier experiences, with how each turned out (memory.recall)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload, err := buildMemoryRecallPayload(args, recallAction, recallK)
			if err != nil {
				return err
			}
			answer, err := a.memoryCall(ctx, "memory.recall", payload)
			if err != nil {
				return err
			}
			fmt.Print(FormatRecall(answer))
			return nil
		},
	}
	recall.Flags().StringVar(&recallAction, "action", "", "Only experiences of this action")
	recall.Flags().IntVarP(&recallK, "k", "k", 10, "How many to show (1-50)")

	var predictAction string
	predict := &cobra.Command{
		Use:   "predict <context...>",
		Short: "Estimate how likely an action is to be accepted in a context (memory.predict)",
		Long: "The estimate comes from the similar earlier experiences of that action and is pulled toward\n" +
			"the action's overall acceptance rate (the baseline). With too little evidence it is the baseline,\n" +
			"and says so.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload, err := buildMemoryPredictPayload(args, predictAction)
			if err != nil {
				return err
			}
			answer, err := a.memoryCall(ctx, "memory.predict", payload)
			if err != nil {
				return err
			}
			fmt.Print(FormatPrediction(answer))
			return nil
		},
	}
	predict.Flags().StringVar(&predictAction, "action", "", "The action to predict, such as hashtag.suggest (required)")
	_ = predict.MarkFlagRequired("action")

	var scoreAction string
	score := &cobra.Command{
		Use:   "score",
		Short: "Score the stored predictions against what happened (memory.stats)",
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := map[string]any{}
			if action := strings.TrimSpace(scoreAction); action != "" {
				payload["action"] = action
			}
			answer, err := a.memoryCall(ctx, "memory.stats", payload)
			if err != nil {
				return err
			}
			fmt.Print(FormatScore(answer))
			return nil
		},
	}
	score.Flags().StringVar(&scoreAction, "action", "", "Only experiences of this action")

	show := &cobra.Command{
		Use:   "show <ref>",
		Short: "Show one experience, without its context (memory.get)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			answer, err := a.memoryCall(ctx, "memory.get", map[string]any{"ref": strings.TrimSpace(args[0])})
			if err != nil {
				return err
			}
			if found, _ := answer["found"].(bool); !found {
				return fmt.Errorf("no experience with ref %q", args[0])
			}
			return printJSON(answer)
		},
	}

	forget := &cobra.Command{
		Use:   "forget <ref>",
		Short: "Delete an experience (memory.forget)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			answer, err := a.memoryCall(ctx, "memory.forget", map[string]any{"ref": strings.TrimSpace(args[0])})
			if err != nil {
				return err
			}
			if deleted, _ := answer["deleted"].(bool); !deleted {
				return fmt.Errorf("no experience with ref %q", args[0])
			}
			fmt.Printf("forgot %s\n", args[0])
			return nil
		},
	}

	status := &cobra.Command{
		Use:   "status",
		Short: "Show what is stored and whether embeddings are working (memory.status)",
		RunE: func(cmd *cobra.Command, args []string) error {
			answer, err := a.memoryCall(ctx, "memory.status", map[string]any{})
			if err != nil {
				return err
			}
			return printJSON(answer)
		},
	}

	var reindexLimit int
	reindex := &cobra.Command{
		Use:   "reindex",
		Short: "Embed experiences that are missing a vector, a batch at a time (memory.reindex)",
		RunE: func(cmd *cobra.Command, args []string) error {
			payload := map[string]any{}
			if cmd.Flags().Changed("limit") {
				payload["limit"] = reindexLimit
			}
			answer, err := a.memoryCall(ctx, "memory.reindex", payload)
			if err != nil {
				return err
			}
			return printJSON(answer)
		},
	}
	reindex.Flags().IntVar(&reindexLimit, "limit", 32, "How many to embed in this call (1-500); repeat until none remain")

	command.AddCommand(record, resolve, recall, predict, score, show, forget, status, reindex)
	return command
}
