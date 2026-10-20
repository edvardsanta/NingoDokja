//go:build unix

package feeds

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

const (
	maxDocuments     = 1000
	maxDocumentTitle = maxTitleRunes
	maxDocumentRef   = 2000
	maxDocumentKey   = 200
	maxFilenameRunes = 200
	maxReasonRunes   = 300
)

// Reasons an on-demand reading fails. Each error from extract starts with one of them and a
// colon, so a caller can tell a document nobody can read from a plugin that broke.
const (
	ReasonInvalid     = "invalid"     // the request itself is wrong
	ReasonUnsupported = "unsupported" // the file has no extension a plugin could serve
	ReasonUnavailable = "unavailable" // no enabled, compatible plugin serves the format
	ReasonAmbiguous   = "ambiguous"   // several plugins serve it and none is bound
	ReasonTimeout     = "timeout"     // the plugin did not finish in time
	ReasonFailed      = "failed"      // the plugin crashed, printed too much or printed nonsense
	ReasonUnreadable  = "unreadable"  // the plugin ran and says the document has no text it can read
)

// Document is Knowledge's Extracted contract (ADR 0006): what a reading hands to Knowledge.
type Document struct {
	Title     string `json:"title"`
	Text      string `json:"text"`
	SourceRef string `json:"source_ref"`
	Key       string `json:"key"`
}

// extractRequest is what a document.extract.v1 plugin reads on stdin.
type extractRequest struct {
	Operation  string `json:"operation"`
	Filename   string `json:"filename"`
	Format     string `json:"format"`
	ContentB64 string `json:"content_b64"`
}

// extractOutput is what it prints: the documents, or why the file cannot be read.
type extractOutput struct {
	Documents *[]Document `json:"documents"`
	Error     *string     `json:"error"`
}

func reasonError(reason, format string, args ...any) error {
	return fmt.Errorf("%s: %s", reason, fmt.Sprintf(format, args...))
}

// extract reads one document on demand (ADR 0006): the file comes in the request, the one
// plugin that serves its format turns it into documents, and nothing is kept. Nothing here
// stores the result; the orchestrator hands it to Knowledge.
func (s *Service) extract(ctx context.Context, payload map[string]any) (map[string]any, error) {
	filename, format, encoded, err := extractInput(payload)
	if err != nil {
		return nil, err
	}
	e, err := s.extractor(format)
	if err != nil {
		return nil, err
	}

	select {
	case s.extractions <- struct{}{}:
		defer func() { <-s.extractions }()
	case <-ctx.Done():
		return nil, reasonError(ReasonTimeout, "no reading slot was free before the deadline")
	}

	plugin := *e.plugin
	input, err := json.Marshal(extractRequest{
		Operation: CapabilityDocumentExtract, Filename: filename, Format: format, ContentB64: encoded,
	})
	if err != nil {
		return nil, reasonError(ReasonInvalid, "the request cannot be encoded")
	}
	output, runErr := s.runner.Run(ctx, plugin, s.environment(plugin), input)
	var documents []Document
	if runErr == nil {
		documents, err = parseDocuments(output)
	} else {
		err = runFailure(runErr)
	}
	s.recordExtraction(e, err)
	if err != nil {
		s.logger.Printf("extractor %s format=%s failed: %s", plugin.ID, format, cleanText(err.Error(), maxReasonRunes))
		return nil, err
	}
	s.logger.Printf("extractor %s format=%s ok documents=%d", plugin.ID, format, len(documents))
	return map[string]any{
		"documents": documents,
		"count":     len(documents),
		"format":    format,
		"plugin":    plugin.ID,
		"version":   plugin.Version,
	}, nil
}

func extractInput(payload map[string]any) (filename, format, encoded string, err error) {
	raw, _ := payload["filename"].(string)
	filename = cleanText(path.Base(strings.ReplaceAll(raw, "\\", "/")), maxFilenameRunes)
	if filename == "" || filename == "." || filename == "/" {
		return "", "", "", reasonError(ReasonInvalid, "filename is required")
	}
	format = strings.ToLower(path.Ext(filename))
	if !formatPattern.MatchString(format) {
		return "", "", "", reasonError(ReasonUnsupported, "%q has no file extension a plugin can serve", filename)
	}
	encoded, _ = payload["content_b64"].(string)
	if encoded == "" {
		return "", "", "", reasonError(ReasonInvalid, "content_b64 is required")
	}
	if len(encoded) > base64.StdEncoding.EncodedLen(MaxExtractInput) {
		return "", "", "", reasonError(ReasonInvalid, "the file is larger than %d bytes", MaxExtractInput)
	}
	decoded, decodeErr := base64.StdEncoding.DecodeString(encoded)
	if decodeErr != nil {
		return "", "", "", reasonError(ReasonInvalid, "content_b64 is not valid base64")
	}
	if len(decoded) == 0 {
		return "", "", "", reasonError(ReasonInvalid, "the file is empty")
	}
	return filename, format, encoded, nil
}

// extractor resolves the plugin bound to a format (ADR 0002). Without an explicit binding
// a single enabled provider is chosen; several make the format ambiguous. When none is
// available, plugins this host cannot run because of their version are named, so the owner
// knows why.
func (s *Service) extractor(format string) (*entry, error) {
	found := make([]Found, 0, len(s.entries))
	byID := map[string]*entry{}
	var incompatible []string
	for _, e := range s.entries {
		if e.plugin != nil {
			found = append(found, Found{Dir: e.dir, Plugin: e.plugin})
			byID[e.plugin.ID] = e
		} else if e.incompatible {
			incompatible = append(incompatible, cleanText(e.dir, 64)+" ("+e.invalid+")")
		}
	}
	selection := Select(found, Binding{Capability: CapabilityDocumentExtract, Key: format})
	if selection.Primary == nil {
		reason := selection.Reason
		if len(incompatible) > 0 && strings.HasPrefix(reason, ReasonUnavailable+":") {
			reason += "; not loaded: " + strings.Join(incompatible, "; ")
		}
		return nil, errors.New(cleanText(reason, maxReasonRunes))
	}
	return byID[selection.Primary.ID], nil
}

func runFailure(err error) error {
	message := cleanText(err.Error(), maxReasonRunes)
	if errors.Is(err, ErrTimeout) || errors.Is(err, context.DeadlineExceeded) {
		return reasonError(ReasonTimeout, "%s", message)
	}
	return reasonError(ReasonFailed, "%s", message)
}

// parseDocuments checks what an extractor printed before anyone sees it (ADR 0002): one
// JSON object in the Extracted shape, bounded, with every text valid UTF-8. Documents
// without text are dropped, as Knowledge drops them.
func parseDocuments(data []byte) ([]Document, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var output extractOutput
	if err := decoder.Decode(&output); err != nil {
		return nil, reasonError(ReasonFailed, "output is not a documents object: %s", cleanText(err.Error(), 120))
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, reasonError(ReasonFailed, "output must hold one JSON object")
	}
	switch {
	case output.Error != nil && output.Documents != nil:
		return nil, reasonError(ReasonFailed, "output holds both documents and an error")
	case output.Error != nil:
		message := cleanText(*output.Error, maxReasonRunes)
		if message == "" {
			message = "the document cannot be read"
		}
		return nil, reasonError(ReasonUnreadable, "%s", message)
	case output.Documents == nil:
		return nil, reasonError(ReasonFailed, `output has no "documents" list`)
	case len(*output.Documents) > maxDocuments:
		return nil, reasonError(ReasonFailed, "output has more than %d documents", maxDocuments)
	}
	documents := make([]Document, 0, len(*output.Documents))
	for _, raw := range *output.Documents {
		text := strings.ReplaceAll(strings.ToValidUTF8(raw.Text, ""), "\x00", "")
		if strings.TrimSpace(text) == "" {
			continue
		}
		documents = append(documents, Document{
			Title:     cleanText(raw.Title, maxDocumentTitle),
			Text:      text,
			SourceRef: cleanText(raw.SourceRef, maxDocumentRef),
			Key:       cleanText(raw.Key, maxDocumentKey),
		})
	}
	if len(documents) == 0 {
		return nil, reasonError(ReasonUnreadable, "the document has no text")
	}
	return documents, nil
}

// recordExtraction keeps the last invocation result of an extractor for feeds.status.
func (s *Service) recordExtraction(e *entry, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e.ran = true
	e.lastRun = s.now()
	if err != nil {
		e.lastErr = cleanText(err.Error(), 200)
		return
	}
	e.lastOK, e.lastErr = e.lastRun, ""
}
