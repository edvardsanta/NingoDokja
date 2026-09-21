package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"
)

// maxKnowledgeFileBytes keeps a file well under the service's own limit (2M characters).
const maxKnowledgeFileBytes = 4 << 20

var knowledgeTextExtensions = map[string]bool{".txt": true, ".md": true, ".markdown": true}

// KnowledgeDocument is one document ready to be sent to the knowledge base.
type KnowledgeDocument struct {
	SourceID  string
	Title     string
	Body      string
	Kind      string
	SourceRef string
	Tags      []string
}

func (d KnowledgeDocument) Payload() map[string]any {
	payload := map[string]any{"title": d.Title, "body": d.Body, "kind": d.Kind}
	if d.SourceID != "" {
		payload["source_id"] = d.SourceID
	}
	if d.SourceRef != "" {
		payload["source_ref"] = d.SourceRef
	}
	if len(d.Tags) > 0 {
		payload["tags"] = d.Tags
	}
	return payload
}

// ReadKnowledgeFile loads a text or markdown file. Its source id comes from the file
// name, so adding the same file again updates the document instead of duplicating it.
func ReadKnowledgeFile(path, title, kind string, tags []string) (KnowledgeDocument, error) {
	extension := strings.ToLower(filepath.Ext(path))
	if !knowledgeTextExtensions[extension] {
		return KnowledgeDocument{}, fmt.Errorf("%s: only .txt and .md files are supported for now", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		return KnowledgeDocument{}, fmt.Errorf("read %s: %w", path, err)
	}
	if info.Size() > maxKnowledgeFileBytes {
		return KnowledgeDocument{}, fmt.Errorf("%s: %d bytes is over the %d byte limit; split it first", path, info.Size(), maxKnowledgeFileBytes)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return KnowledgeDocument{}, fmt.Errorf("read %s: %w", path, err)
	}
	body, err := decodeKnowledgeText(path, raw)
	if err != nil {
		return KnowledgeDocument{}, err
	}

	base := filepath.Base(path)
	if strings.TrimSpace(title) == "" {
		title = firstHeading(body)
	}
	if title == "" {
		title = strings.TrimSuffix(base, filepath.Ext(base))
	}
	return KnowledgeDocument{
		SourceID: "file:" + sanitizeSourceID(base),
		Title:    strings.TrimSpace(title),
		Body:     body,
		Kind:     kind,
		Tags:     tags,
	}, nil
}

// ReadKnowledgeStdin reads a note piped in. It needs a title, since there is no file name.
func ReadKnowledgeStdin(reader io.Reader, title, kind string, tags []string) (KnowledgeDocument, error) {
	if strings.TrimSpace(title) == "" {
		return KnowledgeDocument{}, fmt.Errorf("--title is required when reading from standard input")
	}
	raw, err := io.ReadAll(io.LimitReader(reader, maxKnowledgeFileBytes+1))
	if err != nil {
		return KnowledgeDocument{}, fmt.Errorf("read standard input: %w", err)
	}
	if len(raw) > maxKnowledgeFileBytes {
		return KnowledgeDocument{}, fmt.Errorf("standard input is over the %d byte limit", maxKnowledgeFileBytes)
	}
	body, err := decodeKnowledgeText("standard input", raw)
	if err != nil {
		return KnowledgeDocument{}, err
	}
	return KnowledgeDocument{Title: strings.TrimSpace(title), Body: body, Kind: kind, Tags: tags}, nil
}

func decodeKnowledgeText(name string, raw []byte) (string, error) {
	if strings.ContainsRune(string(raw), 0) || !utf8.Valid(raw) {
		return "", fmt.Errorf("%s is not UTF-8 text", name)
	}
	body := strings.TrimPrefix(string(raw), "\uFEFF")
	if strings.TrimSpace(body) == "" {
		return "", fmt.Errorf("%s is empty", name)
	}
	return body, nil
}

func firstHeading(body string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
		if line != "" && !strings.HasPrefix(line, "#") {
			return ""
		}
	}
	return ""
}

// sanitizeSourceID keeps to the characters the service accepts in a source id.
func sanitizeSourceID(name string) string {
	var out strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', strings.ContainsRune("._-", r):
			out.WriteRune(r)
		default:
			out.WriteRune('-')
		}
	}
	return out.String()
}

func buildKnowledgeSearchPayload(query string, k int) (map[string]any, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("a question is required")
	}
	if k < 1 || k > 20 {
		return nil, fmt.Errorf("-k must be between 1 and 20, got %d", k)
	}
	return map[string]any{"query": query, "k": k}, nil
}

// knowledgeResult digs the knowledge domain's answer out of an orchestrator response.
// The envelope depends on the orchestrator's VA_RESPONSE_MODE: compact (the default)
// puts the domain's answer straight in "result", debug/verbose nests it under the domain
// name. A switched-off service answers with a skip reason instead, returned as skipped.
func knowledgeResult(result any) (answer map[string]any, skipped string, err error) {
	envelope, ok := result.(map[string]any)
	if !ok {
		return nil, "", fmt.Errorf("unexpected orchestrator response shape")
	}
	body, _ := envelope["result"].(map[string]any)
	if body == nil {
		return nil, "", fmt.Errorf("orchestrator response is missing result")
	}
	if isSkipped, _ := body["skipped"].(bool); isSkipped {
		reason, _ := body["reason"].(string)
		return nil, reason, nil
	}
	if nested, ok := body["knowledge"].(map[string]any); ok {
		return nested, "", nil
	}
	return body, "", nil
}

func (a *App) knowledgeCall(ctx context.Context, eventType string, payload map[string]any) (map[string]any, error) {
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
	answer, skipped, err := knowledgeResult(response.Result)
	if err != nil {
		return nil, err
	}
	if skipped != "" {
		return nil, fmt.Errorf("knowledge base is off: %s", skipped)
	}
	return answer, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (a *App) newKnowledgeCommand(ctx context.Context) *cobra.Command {
	command := &cobra.Command{
		Use:   "knowledge",
		Short: "Feed and query the research knowledge base",
	}

	var addTitle, addKind, addRef string
	var addTags []string
	add := &cobra.Command{
		Use:   "add <file.md|file.txt|->...",
		Short: "Add documents (use - to read one note from standard input)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 1 && addTitle != "" {
				return fmt.Errorf("--title only makes sense with a single file")
			}
			failed := 0
			for _, arg := range args {
				var document KnowledgeDocument
				var err error
				if arg == "-" {
					document, err = ReadKnowledgeStdin(os.Stdin, addTitle, addKind, addTags)
				} else {
					document, err = ReadKnowledgeFile(arg, addTitle, addKind, addTags)
				}
				if err == nil {
					document.SourceRef = addRef
					var answer map[string]any
					if answer, err = a.knowledgeCall(ctx, "knowledge.ingest", document.Payload()); err == nil {
						fmt.Println(FormatIngest(arg, answer))
						continue
					}
				}
				failed++
				fmt.Fprintf(os.Stderr, "error  %s: %v\n", arg, err)
			}
			if failed > 0 {
				return fmt.Errorf("%d of %d documents failed", failed, len(args))
			}
			return nil
		},
	}
	add.Flags().StringVar(&addTitle, "title", "", "Title (default: the first # heading, then the file name)")
	add.Flags().StringVar(&addKind, "kind", "note", "Kind of document: note, article, book, paper...")
	add.Flags().StringVar(&addRef, "ref", "", "Where it came from, such as a URL")
	add.Flags().StringSliceVar(&addTags, "tag", nil, "Tag (repeatable)")

	var searchK int
	search := &cobra.Command{
		Use:   "search <question>",
		Short: "Find passages that answer a question",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload, err := buildKnowledgeSearchPayload(strings.Join(args, " "), searchK)
			if err != nil {
				return err
			}
			answer, err := a.knowledgeCall(ctx, "knowledge.search", payload)
			if err != nil {
				return err
			}
			fmt.Print(FormatSearch(answer))
			return nil
		},
	}
	search.Flags().IntVarP(&searchK, "k", "k", 5, "How many passages to show (1-20)")

	var listLimit, listOffset int
	list := &cobra.Command{
		Use:   "list",
		Short: "List stored documents",
		RunE: func(cmd *cobra.Command, args []string) error {
			answer, err := a.knowledgeCall(ctx, "knowledge.list", map[string]any{"limit": listLimit, "offset": listOffset})
			if err != nil {
				return err
			}
			fmt.Print(FormatList(answer))
			return nil
		},
	}
	list.Flags().IntVar(&listLimit, "limit", 20, "Page size (1-100)")
	list.Flags().IntVar(&listOffset, "offset", 0, "Documents to skip")

	remove := &cobra.Command{
		Use:   "rm <source-id>",
		Short: "Delete a document",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			answer, err := a.knowledgeCall(ctx, "knowledge.delete", map[string]any{"source_id": strings.TrimSpace(args[0])})
			if err != nil {
				return err
			}
			if deleted, _ := answer["deleted"].(bool); !deleted {
				return fmt.Errorf("no document with source id %q", args[0])
			}
			fmt.Printf("deleted %s\n", args[0])
			return nil
		},
	}

	status := &cobra.Command{
		Use:   "status",
		Short: "Show what is stored and whether embeddings are working",
		RunE: func(cmd *cobra.Command, args []string) error {
			answer, err := a.knowledgeCall(ctx, "knowledge.status", map[string]any{})
			if err != nil {
				return err
			}
			return printJSON(answer)
		},
	}

	reindex := &cobra.Command{
		Use:   "reindex",
		Short: "Embed chunks that are still missing a vector (after the embedder was down or the model changed)",
		RunE: func(cmd *cobra.Command, args []string) error {
			answer, err := a.knowledgeCall(ctx, "knowledge.reindex", map[string]any{})
			if err != nil {
				return err
			}
			return printJSON(answer)
		},
	}

	command.AddCommand(add, search, list, remove, status, reindex)
	return command
}
