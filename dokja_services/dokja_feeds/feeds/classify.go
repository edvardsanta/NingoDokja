//go:build unix

package feeds

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// maxClassifySample is how much of each document's text a classifier sees. Form and
	// content kind show in the opening; the whole book never needs to leave the host.
	maxClassifySample = 16_000
	maxTopics         = 20
	maxTopicRunes     = 80
	maxEntities       = 50
	maxEntityName     = 120
	maxEntityType     = 40
	// classifyKey names the single binding of document.classify.v1, which has no format.
	classifyKey = "documents"
)

// labelPattern is the shape of a form or content kind. Which labels exist is Knowledge's
// rule (ADR 0008); the host only refuses what cannot be a label.
var labelPattern = regexp.MustCompile(`^[a-z][a-z_]{0,31}$`)

// ClassifyDocument is what a classifier is shown of one document read.
type ClassifyDocument struct {
	Title string `json:"title"`
	Text  string `json:"text"`
	Key   string `json:"key"`
}

// Entity is a named thing a classifier found in a document.
type Entity struct {
	Name string `json:"name"`
	Type string `json:"type,omitempty"`
}

// Proposal is one document's untrusted classification (ADR 0008). An empty label makes no
// claim; an uncertain axis is left for the user.
type Proposal struct {
	Form                 string   `json:"form,omitempty"`
	ContentKind          string   `json:"content_kind,omitempty"`
	UncertainForm        bool     `json:"uncertain_form,omitempty"`
	UncertainContentKind bool     `json:"uncertain_content_kind,omitempty"`
	Topics               []string `json:"topics,omitempty"`
	Entities             []Entity `json:"entities,omitempty"`
	DateAnchor           string   `json:"date_anchor,omitempty"`
}

// classifyRequest is what a document.classify.v1 plugin reads on stdin.
type classifyRequest struct {
	Operation string             `json:"operation"`
	Format    string             `json:"format"`
	Documents []ClassifyDocument `json:"documents"`
}

// classifyOutput is what it prints: one proposal per document, in order, or why it cannot.
type classifyOutput struct {
	Classifications *[]Proposal `json:"classifications"`
	Error           *string     `json:"error"`
}

// classify proposes form, content kind and defining metadata for documents already read
// (ADR 0008). It keeps nothing and decides nothing: Knowledge resolves the proposals
// against the user's answers, and an uncertain axis stays the user's to answer.
func (s *Service) classify(ctx context.Context, payload map[string]any) (map[string]any, error) {
	format, documents, err := classifyInput(payload)
	if err != nil {
		return nil, err
	}
	e, err := s.provider(CapabilityDocumentClassify, classifyKey)
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
	input, err := json.Marshal(classifyRequest{Operation: CapabilityDocumentClassify, Format: format, Documents: documents})
	if err != nil {
		return nil, reasonError(ReasonInvalid, "the request cannot be encoded")
	}
	output, runErr := s.runner.Run(ctx, plugin, s.environment(plugin), input)
	var proposals []Proposal
	if runErr == nil {
		proposals, err = parseProposals(output, len(documents))
	} else {
		err = runFailure(runErr)
	}
	s.recordExtraction(e, err)
	if err != nil {
		s.logger.Printf("classifier %s documents=%d failed: %s", plugin.ID, len(documents), cleanText(err.Error(), maxReasonRunes))
		return nil, err
	}
	s.logger.Printf("classifier %s documents=%d ok", plugin.ID, len(documents))
	return map[string]any{
		"classifications": proposals,
		"count":           len(proposals),
		"plugin":          plugin.ID,
		"version":         plugin.Version,
	}, nil
}

// classifyInput takes the documents a reading produced. Each text is cut to a sample.
func classifyInput(payload map[string]any) (string, []ClassifyDocument, error) {
	format, _ := payload["format"].(string)
	format = strings.ToLower(strings.TrimSpace(format))
	if format != "" && !formatPattern.MatchString(format) {
		return "", nil, reasonError(ReasonInvalid, "format must be a file extension such as .pdf")
	}
	raw, _ := payload["documents"].([]any)
	if len(raw) == 0 {
		return "", nil, reasonError(ReasonInvalid, "documents are required")
	}
	if len(raw) > maxDocuments {
		return "", nil, reasonError(ReasonInvalid, "at most %d documents", maxDocuments)
	}
	documents := make([]ClassifyDocument, 0, len(raw))
	for _, item := range raw {
		fields, _ := item.(map[string]any)
		text, _ := fields["text"].(string)
		text = strings.ReplaceAll(strings.ToValidUTF8(text, ""), "\x00", "")
		if strings.TrimSpace(text) == "" {
			return "", nil, reasonError(ReasonInvalid, "every document needs text")
		}
		title, _ := fields["title"].(string)
		key, _ := fields["key"].(string)
		documents = append(documents, ClassifyDocument{
			Title: cleanText(title, maxDocumentTitle), Text: sample(text, maxClassifySample),
			Key: cleanText(key, maxDocumentKey),
		})
	}
	return format, documents, nil
}

func sample(text string, limit int) string {
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	return string([]rune(text)[:limit])
}

// parseProposals checks what a classifier printed (ADR 0002): one JSON object with exactly
// one bounded proposal per document. A proposal with a malformed field fails the whole run;
// the user is asked instead of half-trusting a plugin that broke its contract.
func parseProposals(data []byte, documents int) ([]Proposal, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var output classifyOutput
	if err := decoder.Decode(&output); err != nil {
		return nil, reasonError(ReasonFailed, "output is not a classifications object: %s", cleanText(err.Error(), 120))
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, reasonError(ReasonFailed, "output must hold one JSON object")
	}
	switch {
	case output.Error != nil && output.Classifications != nil:
		return nil, reasonError(ReasonFailed, "output holds both classifications and an error")
	case output.Error != nil:
		message := cleanText(*output.Error, maxReasonRunes)
		if message == "" {
			message = "the documents cannot be classified"
		}
		return nil, reasonError(ReasonUnreadable, "%s", message)
	case output.Classifications == nil:
		return nil, reasonError(ReasonFailed, `output has no "classifications" list`)
	case len(*output.Classifications) != documents:
		return nil, reasonError(ReasonFailed, "output has %d classifications for %d documents", len(*output.Classifications), documents)
	}
	proposals := make([]Proposal, 0, documents)
	for i, proposal := range *output.Classifications {
		checked, err := checkProposal(proposal)
		if err != nil {
			return nil, reasonError(ReasonFailed, "classification %d: %s", i+1, err)
		}
		proposals = append(proposals, checked)
	}
	return proposals, nil
}

func checkProposal(p Proposal) (Proposal, error) {
	for _, label := range []string{p.Form, p.ContentKind} {
		if label != "" && !labelPattern.MatchString(label) {
			return p, errors.New("form and content_kind must be lowercase labels")
		}
	}
	if len(p.Topics) > maxTopics {
		return p, errors.New("too many topics")
	}
	topics := make([]string, 0, len(p.Topics))
	for _, topic := range p.Topics {
		topic = cleanText(topic, maxTopicRunes)
		if topic == "" {
			return p, errors.New("topics must be nonempty")
		}
		topics = append(topics, topic)
	}
	if len(p.Entities) > maxEntities {
		return p, errors.New("too many entities")
	}
	entities := make([]Entity, 0, len(p.Entities))
	for _, entity := range p.Entities {
		name := cleanText(entity.Name, maxEntityName)
		if name == "" {
			return p, errors.New("entities require a name")
		}
		entities = append(entities, Entity{Name: name, Type: cleanText(entity.Type, maxEntityType)})
	}
	if p.DateAnchor != "" {
		if _, err := time.Parse("2006-01-02", p.DateAnchor); err != nil {
			return p, errors.New("date_anchor must be YYYY-MM-DD")
		}
	}
	p.Topics, p.Entities = nil, nil
	if len(topics) > 0 {
		p.Topics = topics
	}
	if len(entities) > 0 {
		p.Entities = entities
	}
	return p, nil
}
