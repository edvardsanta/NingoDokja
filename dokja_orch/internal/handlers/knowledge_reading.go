package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	knowledgedomain "read_books/dokja_domain/dokja_knowledge"
	"read_books/internal/core"
	"read_books/internal/logger"
	"strings"
	"time"
)

const (
	readingTaskVersion = "2"
	// readingTaskDeadline covers the reading host's 2-minute reading, its 1-minute
	// classification and the Knowledge calls.
	readingTaskDeadline = 4 * time.Minute
	readingCapability   = "document.extract.v1"
)

// DocumentReader is the reading host (Feeds' on-demand mode, ADR 0006).
type DocumentReader interface {
	Extract(ctx context.Context, filename, contentB64 string) (map[string]any, error)
}

// DocumentClassifier is the reading host's document.classify.v1 (ADR 0008). A reader that
// also classifies is used for both.
type DocumentClassifier interface {
	Classify(ctx context.Context, format string, documents []map[string]any) (map[string]any, error)
}

// ReadingTask serves knowledge.read as a typed task (step 07): Reading turns the file into
// documents, then Knowledge stores them with their provenance. The file and the text never
// enter the workspace, which is bounded; the steps keep them and record a hashed reference.
type ReadingTask struct {
	reader     DocumentReader
	classifier DocumentClassifier
	domain     *knowledgedomain.Domain
	enabled    func() bool
	deadline   time.Duration
}

func NewReadingTask(reader DocumentReader, knowledge knowledgedomain.Service, readingEnabled func() bool) *ReadingTask {
	classifier, _ := reader.(DocumentClassifier)
	return &ReadingTask{reader: reader, classifier: classifier, domain: knowledgedomain.New(knowledge), enabled: readingEnabled, deadline: readingTaskDeadline}
}

// readingRun is what one reading holds outside the workspace.
type readingRun struct {
	content string
	reading knowledgedomain.Reading
	stored  []map[string]any
	detail  string // the host's or Knowledge's message for a failed step
	// classification is what the classify step did; it never fails the reading.
	classification map[string]any
}

func (t *ReadingTask) Read(ctx context.Context, event core.Event) (map[string]any, error) {
	if t == nil || t.reader == nil {
		return nil, fmt.Errorf("knowledge.read needs the reading host, which is not configured")
	}
	if err := knowledgedomain.ValidateRead(event.Payload); err != nil {
		return nil, err
	}
	if t.enabled != nil && !t.enabled() {
		return nil, fmt.Errorf("reading is switched off (feeds)")
	}

	run := &readingRun{content: strings.TrimSpace(fmt.Sprint(event.Payload["content_b64"]))}
	request := make(map[string]any, len(event.Payload))
	for key, value := range event.Payload {
		if key != "content_b64" {
			request[key] = value
		}
	}
	filename := strings.TrimSpace(fmt.Sprint(request["filename"]))
	run.reading = knowledgedomain.Reading{Filename: filename, Payload: request}
	taskEvent := event
	taskEvent.Payload = request

	result, err := core.RunTask(ctx, taskEvent, core.TypedWorkflow{
		Name: "knowledge-reading", Version: readingTaskVersion, Goal: "read a file and keep it in knowledge",
		Deadline: t.deadline,
		Steps: []core.TaskStep{
			{Name: "read", Domain: core.DomainKnowledge, Action: "read-document", Run: t.read(run)},
			{Name: "ingest", Domain: core.DomainKnowledge, Action: knowledgedomain.ActionIngestKnowledge, Run: t.ingest(run)},
			{Name: "classify", Domain: core.DomainKnowledge, Action: "classify-reading", Run: t.classify(run)},
		},
	}, core.PrototypeTaskLimits())
	if err != nil {
		return nil, fmt.Errorf("knowledge.read was not started: %w", err)
	}
	// Only the shape of what happened is logged; the file and its text are the user's.
	logger.Info(fmt.Sprintf("reading task attempt=%s status=%s reason=%s format=%s reader=%s documents=%d stored=%d",
		result.AttemptID, result.Status, result.Reason, run.reading.Format, run.reading.Reader,
		len(run.reading.Documents), len(run.stored)))
	if result.Status != core.TaskCompleted {
		message := fmt.Sprintf("knowledge.read %s: %s", result.Status, result.Reason)
		if run.detail != "" {
			message += ": " + run.detail
		}
		if len(run.stored) > 0 {
			message += fmt.Sprintf(" (%d of %d documents stored)", len(run.stored), len(run.reading.Documents))
		}
		return nil, fmt.Errorf("%s", message)
	}
	return map[string]any{
		"attempt_id":     result.AttemptID,
		"format":         run.reading.Format,
		"reader":         run.reading.Reader,
		"reader_version": run.reading.ReaderVersion,
		"count":          len(run.stored),
		"documents":      run.stored,
		"classification": run.classification,
		"task":           result,
	}, nil
}

func (t *ReadingTask) read(run *readingRun) core.TaskStepFunc {
	return func(ctx context.Context, in core.StepInput) (core.StepResult, error) {
		answer, err := t.reader.Extract(ctx, run.reading.Filename, run.content)
		if err != nil {
			run.detail = err.Error()
			return core.StepResult{Disposition: core.StepFail, Reason: readingReason(err)}, nil
		}
		documents := readDocuments(answer["documents"])
		if len(documents) == 0 {
			run.detail = "the reading host returned no documents"
			return core.StepResult{Disposition: core.StepFail, Reason: "reading_invalid_result"}, nil
		}
		run.reading.Documents = documents
		run.reading.Format = resultString(answer, "format")
		run.reading.Reader = resultString(answer, "plugin")
		run.reading.ReaderVersion = resultString(answer, "version")
		sum := sha256.Sum256([]byte(run.content))
		return core.StepResult{
			Disposition: core.StepContinue,
			Output: map[string]any{
				"format": run.reading.Format, "reader": run.reading.Reader,
				"reader_version": run.reading.ReaderVersion, "documents": len(documents),
			},
			// The file stays the user's: the evidence names its hash, never its name or text.
			Evidence: []core.Evidence{{
				Ref: "file:sha256:" + hex.EncodeToString(sum[:])[:16], Capability: readingCapability,
				Version: run.reading.Reader + "@" + run.reading.ReaderVersion, ObservedAt: time.Now().UTC(),
				Scope: in.Scope, Reason: "file read by the reading host",
			}},
		}, nil
	}
}

func (t *ReadingTask) ingest(run *readingRun) core.TaskStepFunc {
	return func(ctx context.Context, _ core.StepInput) (core.StepResult, error) {
		stored, err := t.domain.StoreReading(ctx, run.reading)
		run.stored = stored
		if err != nil {
			run.detail = err.Error()
			return core.StepResult{Disposition: core.StepFail, Reason: "knowledge_ingest_failed"}, nil
		}
		changed := 0
		for _, result := range stored {
			if flag, _ := result["changed"].(bool); flag {
				changed++
			}
		}
		return core.StepResult{
			Disposition: core.StepContinue,
			Output:      map[string]any{"stored": len(stored), "changed": changed},
		}, nil
	}
}

// classify asks the reading host's classifier about the documents stored just now (ADR
// 0008). Unchanged rereads are never sent again, so a classification runs once per content.
// Whatever goes wrong here, the documents are already stored: the step records why and the
// undecided axes stay questions for the user.
func (t *ReadingTask) classify(run *readingRun) core.TaskStepFunc {
	return func(ctx context.Context, _ core.StepInput) (core.StepResult, error) {
		candidates := knowledgedomain.ClassificationCandidates(run.stored)
		run.classification = map[string]any{"candidates": len(candidates), "classified": 0}
		done := func(reason string) (core.StepResult, error) {
			if reason != "" {
				run.classification["reason"] = reason
			}
			return core.StepResult{Disposition: core.StepContinue, Output: run.classification}, nil
		}
		if len(candidates) == 0 {
			return done("")
		}
		if t.classifier == nil {
			return done("classifier_not_configured")
		}
		documents := make([]map[string]any, len(candidates))
		for n, i := range candidates {
			document := run.reading.Documents[i]
			documents[n] = map[string]any{"title": document.Title, "text": document.Text, "key": document.Key}
		}
		answer, err := t.classifier.Classify(ctx, run.reading.Format, documents)
		if err != nil {
			logger.Info(fmt.Sprintf("reading classification skipped: %s", err))
			return done(strings.Replace(readingReason(err), "reading_", "classifier_", 1))
		}
		classifierID, version := resultString(answer, "plugin"), resultString(answer, "version")
		run.classification["classifier"] = classifierID + "@" + version
		proposals, ok := readProposals(answer["classifications"])
		if !ok || len(proposals) != len(candidates) {
			return done("classifier_invalid_result")
		}
		err = t.domain.ApplyClassification(ctx, run.stored, candidates, proposals, classifierID, version)
		classified := 0
		for _, i := range candidates {
			if flag, _ := run.stored[i]["classified"].(bool); flag {
				classified++
			}
		}
		run.classification["classified"] = classified
		if err != nil {
			logger.Info(fmt.Sprintf("reading classification not kept: %s", err))
			return done("knowledge_classify_failed")
		}
		return done("")
	}
}

// readProposals decodes the reading host's checked proposals, one per document sent.
func readProposals(raw any) ([]knowledgedomain.ReadingProposal, bool) {
	list, ok := raw.([]any)
	if !ok {
		return nil, false
	}
	proposals := make([]knowledgedomain.ReadingProposal, 0, len(list))
	for _, item := range list {
		fields, ok := item.(map[string]any)
		if !ok {
			return nil, false
		}
		proposal := knowledgedomain.ReadingProposal{
			Form:        knowledgedomain.Form(resultString(fields, "form")),
			ContentKind: knowledgedomain.ContentKind(resultString(fields, "content_kind")),
			DateAnchor:  resultString(fields, "date_anchor"),
		}
		proposal.UncertainForm, _ = fields["uncertain_form"].(bool)
		proposal.UncertainContentKind, _ = fields["uncertain_content_kind"].(bool)
		topics, _ := fields["topics"].([]any)
		for _, topic := range topics {
			if text, ok := topic.(string); ok {
				proposal.Topics = append(proposal.Topics, text)
			}
		}
		entities, _ := fields["entities"].([]any)
		for _, raw := range entities {
			if entity, ok := raw.(map[string]any); ok {
				proposal.Entities = append(proposal.Entities, knowledgedomain.Entity{
					Name: resultString(entity, "name"), Type: resultString(entity, "type"),
				})
			}
		}
		proposals = append(proposals, proposal)
	}
	return proposals, true
}

// readingReason turns the host's reason prefix (feeds.extract) into a step reason.
func readingReason(err error) string {
	prefix, _, found := strings.Cut(err.Error(), ":")
	switch prefix {
	case "invalid", "unsupported", "unavailable", "ambiguous", "timeout", "failed", "unreadable":
		if found {
			return "reading_" + prefix
		}
	}
	return "reading_host_unavailable"
}

func readDocuments(raw any) []knowledgedomain.ReadDocument {
	list, _ := raw.([]any)
	documents := make([]knowledgedomain.ReadDocument, 0, len(list))
	for _, item := range list {
		fields, ok := item.(map[string]any)
		if !ok {
			continue
		}
		text := resultString(fields, "text")
		if strings.TrimSpace(text) == "" {
			continue
		}
		documents = append(documents, knowledgedomain.ReadDocument{
			Title: resultString(fields, "title"), Text: text,
			SourceRef: resultString(fields, "source_ref"), Key: resultString(fields, "key"),
		})
	}
	return documents
}
