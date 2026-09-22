package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// newMemeHashtagCommand is "meme hashtag": train and query the hashtag classifier that
// suggests a meme's hashtag from the text printed on the image (its OCR text, not the
// scraper's title or tags, which are often meaningless). Training is per this project's
// own scheme: the operator tags examples, and the orchestrator's meme service finds the
// closest one for a new meme; nothing here decides that on its own.
func (a *App) newMemeHashtagCommand(ctx context.Context) *cobra.Command {
	hashtagCommand := &cobra.Command{
		Use:   "hashtag",
		Short: "Tag memes with a hashtag by example, and suggest one for a new meme",
	}

	var tagText string
	tag := &cobra.Command{
		Use:   "tag <image-url> <hashtag>",
		Short: "Train the classifier: this meme's text means this hashtag (meme.hashtag.tag)",
		Long: "Reads the hashtag from the image's own text (via the same OCR the NSFW filter uses)\n" +
			"unless --text is given. A meme with no legible text cannot be tagged.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.request(ctx, EmitOptions{
				Type:      "meme.hashtag.tag",
				UserID:    a.emitOpts.userID,
				UserName:  a.emitOpts.userName,
				ChannelID: a.emitOpts.channelID,
				Payload: map[string]any{
					"url":     strings.TrimSpace(args[0]),
					"hashtag": strings.TrimSpace(args[1]),
					"text":    strings.TrimSpace(tagText),
				},
				Context: map[string]any{"interface": "cli"},
				Source:  "cli",
			})
		},
	}
	tag.Flags().StringVar(&tagText, "text", "", "Use this text instead of reading it from the image")

	var suggestText string
	var suggestMinScore float64
	var suggestMinScoreSet bool
	suggest := &cobra.Command{
		Use:   "suggest [image-url]",
		Short: "Suggest a hashtag for a meme by its closest tagged example (meme.hashtag.suggest)",
		Long: "Check the result's \"relevant\" field: a hit below the threshold is still\n" +
			"reported for visibility, but must not be treated as a match.",
		Args: cobra.MaximumNArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			suggestMinScoreSet = cmd.Flags().Changed("min-score")
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			payload, err := buildHashtagSuggestPayload(args, suggestText, suggestMinScore, suggestMinScoreSet)
			if err != nil {
				return err
			}
			return a.request(ctx, EmitOptions{
				Type:      "meme.hashtag.suggest",
				UserID:    a.emitOpts.userID,
				UserName:  a.emitOpts.userName,
				ChannelID: a.emitOpts.channelID,
				Payload:   payload,
				Context:   map[string]any{"interface": "cli"},
				Source:    "cli",
			})
		},
	}
	suggest.Flags().StringVar(&suggestText, "text", "", "Suggest for this text instead of an image's url")
	suggest.Flags().Float64Var(&suggestMinScore, "min-score", 0, "Override the service's relevance threshold for this call")

	var listLimit, listOffset int
	list := &cobra.Command{
		Use:   "list",
		Short: "List tagged examples, newest first (meme.hashtag.list)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.request(ctx, EmitOptions{
				Type:      "meme.hashtag.list",
				UserID:    a.emitOpts.userID,
				UserName:  a.emitOpts.userName,
				ChannelID: a.emitOpts.channelID,
				Payload:   map[string]any{"limit": listLimit, "offset": listOffset},
				Context:   map[string]any{"interface": "cli"},
				Source:    "cli",
			})
		},
	}
	list.Flags().IntVar(&listLimit, "limit", 50, "Page size (1-200)")
	list.Flags().IntVar(&listOffset, "offset", 0, "Rows to skip")

	untag := &cobra.Command{
		Use:   "untag <image-url>",
		Short: "Remove a tagged example (meme.hashtag.untag)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.request(ctx, EmitOptions{
				Type:      "meme.hashtag.untag",
				UserID:    a.emitOpts.userID,
				UserName:  a.emitOpts.userName,
				ChannelID: a.emitOpts.channelID,
				Payload:   map[string]any{"url": strings.TrimSpace(args[0])},
				Context:   map[string]any{"interface": "cli"},
				Source:    "cli",
			})
		},
	}

	hashtagCommand.AddCommand(tag, suggest, list, untag)
	return hashtagCommand
}

func buildHashtagSuggestPayload(args []string, text string, minScore float64, minScoreSet bool) (map[string]any, error) {
	url := ""
	if len(args) == 1 {
		url = strings.TrimSpace(args[0])
	}
	text = strings.TrimSpace(text)
	if url == "" && text == "" {
		return nil, fmt.Errorf("meme hashtag suggest needs an image url or --text")
	}
	payload := map[string]any{}
	if url != "" {
		payload["url"] = url
	}
	if text != "" {
		payload["text"] = text
	}
	if minScoreSet {
		payload["min_score"] = minScore
	}
	return payload, nil
}
