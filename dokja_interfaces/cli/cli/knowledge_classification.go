package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
)

type knowledgeCallFunc func(context.Context, string, map[string]any) (map[string]any, error)

// newKnowledgeClassifyCommand resumes a stored classification from explicit user
// input. It sends no document content and never supplies a default answer.
func newKnowledgeClassifyCommand(ctx context.Context, call knowledgeCallFunc) *cobra.Command {
	var form, kind string
	command := &cobra.Command{
		Use: "classify <source-id>", Short: "Answer classification questions for a stored document",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sourceID := strings.TrimSpace(args[0])
			if sourceID == "" {
				return fmt.Errorf("source-id is required")
			}
			answers := map[string]any{}
			for _, flag := range []struct{ name, value, field string }{{"form", form, "form"}, {"content-kind", kind, "content_kind"}} {
				if cmd.Flags().Changed(flag.name) {
					if strings.TrimSpace(flag.value) == "" {
						return fmt.Errorf("--%s must be nonempty", flag.name)
					}
					answers[flag.field] = strings.TrimSpace(flag.value)
				}
			}
			snapshot, err := call(ctx, "knowledge.classify", map[string]any{"source_id": sourceID})
			if err != nil {
				return err
			}
			classification, ok := snapshot["classification"].(map[string]any)
			if !ok {
				return fmt.Errorf("classification response is missing its questions")
			}
			if len(answers) == 0 {
				if classification["needs_user_input"] == false {
					fmt.Fprintln(cmd.OutOrStdout(), "Classification is already complete.")
					return nil
				}
				questions, ok := classification["questions"].([]any)
				if !ok || len(questions) == 0 {
					return fmt.Errorf("classification response has no questions")
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Classification for %s\n", sourceID)
				reader := bufio.NewReader(cmd.InOrStdin())
				for _, raw := range questions {
					question, ok := raw.(map[string]any)
					if !ok {
						return fmt.Errorf("invalid classification question")
					}
					field, _ := question["field"].(string)
					if field != "form" && field != "content_kind" {
						return fmt.Errorf("unsupported classification question")
					}
					text, _ := question["question"].(string)
					values, _ := question["allowed_values"].([]any)
					choices := []string{}
					for _, raw := range values {
						if choice, ok := raw.(string); ok && choice != "" {
							choices = append(choices, choice)
						}
					}
					if text == "" || len(choices) == 0 {
						return fmt.Errorf("classification question has no choices")
					}
					for {
						fmt.Fprintf(cmd.OutOrStdout(), "%s [%s] (Enter to leave pending): ", text, strings.Join(choices, ", "))
						line, readErr := reader.ReadString('\n')
						if readErr != nil && (readErr != io.EOF || len(line) == 0) {
							return fmt.Errorf("classification remains pending: no answer submitted")
						}
						answer := strings.ToLower(strings.TrimSpace(line))
						if answer == "" {
							break
						}
						valid := false
						for _, choice := range choices {
							if answer == choice {
								valid = true
								break
							}
						}
						if !valid {
							if readErr == io.EOF {
								return fmt.Errorf("classification remains pending: invalid answer")
							}
							fmt.Fprintln(cmd.OutOrStdout(), "Choose one of the listed values; no default will be selected.")
							continue
						}
						answers[field] = answer
						break
					}
				}
			}
			if len(answers) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "Classification remains pending; no answers were submitted.")
				return nil
			}
			revision, _ := snapshot["revision"].(string)
			if revision == "" {
				return fmt.Errorf("classification response is missing its revision")
			}
			result, err := call(ctx, "knowledge.classify", map[string]any{"source_id": sourceID, "answers": answers, "revision": revision})
			if err != nil {
				return err
			}
			state, ok := result["classification"].(map[string]any)
			if !ok {
				return fmt.Errorf("classification update is missing its state")
			}
			if state["needs_user_input"] == false {
				fmt.Fprintln(cmd.OutOrStdout(), "Classification saved.")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "Answers saved; classification still needs your input. Run this command again to continue.")
			}
			return nil
		},
	}
	command.Flags().StringVar(&form, "form", "", "Explicit document form")
	command.Flags().StringVar(&kind, "content-kind", "", "Explicit content kind")
	return command
}

func classificationHint(answer map[string]any) string {
	var out strings.Builder
	if classification, ok := answer["classification"].(map[string]any); ok && classification["needs_user_input"] == true {
		fmt.Fprintf(&out, "\n  Classification needs your answer: knowledge classify %v", answer["source_id"])
	}
	if documents, ok := answer["documents"].([]any); ok {
		for _, raw := range documents {
			if document, ok := raw.(map[string]any); ok {
				out.WriteString(classificationHint(document))
			}
		}
	}
	return out.String()
}

// Explicit inference is separate from answering the domain's questions.
func newKnowledgeReclassifyCommand(ctx context.Context, call knowledgeCallFunc) *cobra.Command {
	return &cobra.Command{
		Use:   "reclassify <source-id>",
		Short: "Ask the configured classifier to revise automatic document labels",
		Long:  "Reclassify stored text without re-uploading or embedding it. User decisions remain unchanged. Use --timeout 7m for slow providers.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := strings.TrimSpace(args[0])
			if id == "" {
				return fmt.Errorf("source-id is required")
			}
			result, err := call(ctx, "knowledge.reclassify", map[string]any{"source_id": id})
			if err != nil {
				return err
			}
			if result["reclassified"] == false {
				fmt.Fprintln(cmd.OutOrStdout(), "User decisions preserved; no automatic labels to revise.")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "Automatic classification updated.")
				fmt.Fprintln(cmd.OutOrStdout(), classificationHint(result))
			}
			return nil
		},
	}
}
