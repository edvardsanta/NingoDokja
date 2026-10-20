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
	readingTaskVersion = "1"
	// readingTaskDeadline covers the reading host's 2-minute limit and the ingest calls.
	readingTaskDeadline = 3 * time.Minute
	readingCapability   = "document.extract.v1"
)

// DocumentReader is the reading host (Feeds' on-demand mode, ADR 0006).
type DocumentReader interface {
	Extract(ctx context.Context, filename, contentB64 string) (map[string]any, error)
}

// ReadingTask serves knowledge.read as a typed task (step 07): Reading turns the file into
// documents, then Knowledge stores them with their provenance. The file and the text never
// enter the workspace, which is bounded; the steps keep them and record a hashed reference.
type ReadingTask struct {
	reader   DocumentReader
	domain   *knowledgedomain.Domain
	enabled  func() bool
	deadline time.Duration
}

func NewReadingTask(reader DocumentReader, knowledge knowledgedomain.Service, readingEnabled func() bool) *ReadingTask {
	return &ReadingTask{reader: reader, domain: knowledgedomain.New(knowledge), enabled: readingEnabled, deadline: readingTaskDeadline}
}

// readingRun is what one reading holds outside the workspace.
type readingRun struct {
	content string
	reading knowledgedomain.Reading
	stored  []map[string]any
	detail  string // the host's or Knowledge's message for a failed step
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
