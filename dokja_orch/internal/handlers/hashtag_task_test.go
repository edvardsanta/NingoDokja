package handlers

import (
	"context"
	"errors"
	memorydomain "read_books/dokja_domain/dokja_memory"
	"read_books/internal/core"
	"strings"
	"testing"
	"time"
)

const earlierMemeURL = "https://media.example/old.png"

func relevantKnowledge(relevant bool) *fakeHashtagKnowledge {
	return &fakeHashtagKnowledge{answers: map[string]map[string]any{
		"knowledge.search": {"threshold": 0.6, "degraded": false, "hits": []any{map[string]any{
			"title": "#Humor", "text": "words of the earlier meme", "source_ref": earlierMemeURL,
			"score": 0.83, "relevant": relevant,
		}}},
	}}
}

func hashtagTaskWith(knowledge *fakeHashtagKnowledge, memory *recordingMemory, memoryOn bool) *HashtagTask {
	media := &fakeMemeTextReader{result: map[string]any{"text": testMemeText}}
	task := NewHashtagTask(NewMemeHashtagWorkflow(media, knowledge), memory, func() bool { return memoryOn })
	task.budget = 50 * time.Millisecond
	return task
}

func taskEventFor() core.Event {
	return core.Event{EventID: "evt-1", Source: core.SourceOrchestrator, Type: "meme.dispatch.scheduled",
		Channel: core.EventChannel{ID: "discord-channel"}}
}

func stepNames(result core.TaskResult) string {
	names := make([]string, 0, len(result.Steps))
	for _, step := range result.Steps {
		names = append(names, step.Name)
	}
	return strings.Join(names, ",")
}

func TestHashtagTaskLinksReadingRetrievalSelectionAndPrediction(t *testing.T) {
	memory := &recordingMemory{answers: defaultAnswers()}
	attempt := hashtagTaskWith(relevantKnowledge(true), memory, true).Suggest(context.Background(), taskEventFor(), testMemeURL)

	task := attempt.Task
	if attempt.Hashtag != "#Humor" || attempt.QueryText != testMemeText || attempt.Match.Score != 0.83 ||
		attempt.AttemptID == "" || attempt.AttemptID != task.AttemptID {
		t.Fatalf("unexpected attempt: %#v", attempt)
	}
	if task.Status != core.TaskCompleted || stepNames(task) != "read,retrieve,select,predict" {
		t.Fatalf("expected the four faculty steps to complete: %s %s", task.Status, stepNames(task))
	}
	if attempt.Prediction == nil || attempt.Prediction.Method != hashtagPredictionMethod ||
		attempt.Prediction.BaselineP <= 0 || attempt.Prediction.Resolved != 10 {
		t.Fatalf("expected a frozen prediction with its baseline and support: %#v", attempt.Prediction)
	}
	if len(task.Evidence) != 2 || task.Evidence[0].Ref != memorydomain.HashtagExperienceRef(testMemeURL) ||
		task.Evidence[1].Ref != "meme-hashtag:ab79f604fdb461b840259a66c868ae02" || task.Evidence[1].Scope != (core.TaskScope{}) {
		t.Fatalf("evidence must cite the meme and the labelled example without addresses: %#v", task.Evidence)
	}
	if len(task.Proposals) != 1 || task.Proposals[0].ActionID != "append-hashtag" || len(task.Proposals[0].EvidenceRefs) != 2 {
		t.Fatalf("the selection must propose the action with its evidence: %#v", task.Proposals)
	}
	for _, private := range []string{"media.example", testMemeText} {
		for _, evidence := range task.Evidence {
			if strings.Contains(evidence.Ref, private) || strings.Contains(evidence.Reason, private) {
				t.Fatalf("evidence leaked %q: %#v", private, evidence)
			}
		}
		if strings.Contains(task.Summary, private) || strings.Contains(task.Reason, private) {
			t.Fatalf("the task summary leaked %q", private)
		}
	}
}

func TestHashtagTaskAbstainsBelowTheThresholdWithoutPredicting(t *testing.T) {
	memory := &recordingMemory{answers: defaultAnswers()}
	attempt := hashtagTaskWith(relevantKnowledge(false), memory, true).Suggest(context.Background(), taskEventFor(), testMemeURL)

	if attempt.Hashtag != "" || attempt.Task.Reason != core.ReasonAbstained || len(memory.sent()) != 0 {
		t.Fatalf("an abstention appends nothing and predicts nothing: %#v, memory %v", attempt, memory.eventTypes())
	}
}

func TestAMissedPredictionBudgetLeavesTheAttemptUnscored(t *testing.T) {
	memory := &recordingMemory{answers: defaultAnswers(), gate: make(chan struct{})}
	t.Cleanup(func() { close(memory.gate) })

	started := time.Now()
	attempt := hashtagTaskWith(relevantKnowledge(true), memory, true).Suggest(context.Background(), taskEventFor(), testMemeURL)

	predict := attempt.Task.Steps[len(attempt.Task.Steps)-1]
	if attempt.Hashtag != "#Humor" || attempt.Prediction != nil || !predict.Degraded || predict.Reason != "prediction_budget_missed" {
		t.Fatalf("a slow memory must not block the suggestion: %#v / %#v", attempt, predict)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("the prediction held the suggestion for %s", elapsed)
	}
}

func TestAMemoryFailureDegradesTheTaskButKeepsTheHashtag(t *testing.T) {
	memory := &recordingMemory{answers: defaultAnswers(), errors: map[string]error{"memory.recall": errors.New("down")}}
	attempt := hashtagTaskWith(relevantKnowledge(true), memory, true).Suggest(context.Background(), taskEventFor(), testMemeURL)

	predict := attempt.Task.Steps[len(attempt.Task.Steps)-1]
	if attempt.Hashtag != "#Humor" || attempt.Prediction != nil || attempt.Task.Status != core.TaskCompleted ||
		!predict.Degraded || predict.Reason != "prediction_unavailable" {
		t.Fatalf("an optional prediction failure is degraded, not fatal: %#v", attempt.Task)
	}
}

func TestSwitchesDecideWhichStepsRun(t *testing.T) {
	memory := &recordingMemory{answers: defaultAnswers()}
	attempt := hashtagTaskWith(relevantKnowledge(true), memory, false).Suggest(context.Background(), taskEventFor(), testMemeURL)
	if attempt.Hashtag != "#Humor" || stepNames(attempt.Task) != "read,retrieve,select" || len(memory.sent()) != 0 {
		t.Fatalf("with memory off nothing is predicted: %s, memory %v", stepNames(attempt.Task), memory.eventTypes())
	}

	knowledge := relevantKnowledge(true)
	media := &fakeMemeTextReader{result: map[string]any{"text": testMemeText}}
	off := NewHashtagTask(NewMemeHashtagWorkflow(media, knowledge).WithKnowledgeEnabled(func() bool { return false }), memory, nil)
	if attempt := off.Suggest(context.Background(), taskEventFor(), testMemeURL); attempt.Hashtag != "" || len(media.urls) != 0 || len(knowledge.calls) != 0 {
		t.Fatalf("with knowledge off nothing runs: %#v", attempt)
	}
}

func TestUnreadableMediaFailsTheTask(t *testing.T) {
	media := &fakeMemeTextReader{err: errors.New("ocr unavailable")}
	task := NewHashtagTask(NewMemeHashtagWorkflow(media, relevantKnowledge(true)), nil, nil)

	attempt := task.Suggest(context.Background(), taskEventFor(), testMemeURL)
	if attempt.Hashtag != "" || attempt.Task.Status != core.TaskFailed || attempt.Task.Reason != "media_text_unavailable" {
		t.Fatalf("expected a failed task without a hashtag: %#v", attempt.Task)
	}
}

// orderingDeliverer checks, at the moment of delivery, what the memory had already been asked.
type orderingDeliverer struct {
	memory        *recordingMemory
	err           error
	contents      []string
	predictedBy   []bool
	recordedEarly bool
}

func (d *orderingDeliverer) Deliver(_ context.Context, _, content, _ string) error {
	types := strings.Join(d.memory.eventTypes(), ",")
	d.predictedBy = append(d.predictedBy, strings.Contains(types, "memory.recall"))
	d.recordedEarly = d.recordedEarly || strings.Contains(types, "memory.record")
	if d.err != nil {
		return d.err
	}
	d.contents = append(d.contents, content)
	return nil
}

func (d *orderingDeliverer) DeliverAttachment(ctx context.Context, channelID, content string, _ []byte, _, url string) error {
	return d.Deliver(ctx, channelID, content, url)
}

func scheduledHandler(memory *recordingMemory, deliverer *orderingDeliverer, urls ...string) *SystemDomainHandler {
	memes := make([]any, 0, len(urls))
	for _, url := range urls {
		memes = append(memes, map[string]any{"title": "meme", "url": url})
	}
	task := hashtagTaskWith(relevantKnowledge(true), memory, true)
	return NewSystemDomainHandler(nil, nil, &fakeMemeFetcher{result: map[string]any{"memes": memes}}, deliverer, "discord-channel").
		WithHashtagTask(task, synchronous(memory, nil))
}

func scheduledRun(handler *SystemDomainHandler) error {
	_, err := handler.Handle(context.Background(), taskEventFor(), core.WorkflowStep{Domain: core.DomainSystem, Action: "deliver-scheduled-memes"})
	return err
}

func recordsOf(memory *recordingMemory) []map[string]any {
	var records []map[string]any
	for _, call := range memory.sent() {
		if call.eventType == "memory.record" {
			records = append(records, call.payload)
		}
	}
	return records
}

func TestThePredictionIsFrozenBeforeDeliveryAndRecordedAfterIt(t *testing.T) {
	memory := &recordingMemory{answers: defaultAnswers()}
	deliverer := &orderingDeliverer{memory: memory}

	if err := scheduledRun(scheduledHandler(memory, deliverer, testMemeURL)); err != nil {
		t.Fatal(err)
	}
	if len(deliverer.contents) != 1 || deliverer.contents[0] != "meme\n#Humor" {
		t.Fatalf("the delivered text must not change: %q", deliverer.contents)
	}
	if !deliverer.predictedBy[0] || deliverer.recordedEarly {
		t.Fatalf("predict before the delivery, record after it: predicted=%v recordedEarly=%v", deliverer.predictedBy, deliverer.recordedEarly)
	}
	records := recordsOf(memory)
	if len(records) != 1 || records[0]["ref"] != memorydomain.HashtagExperienceRef(testMemeURL) || records[0]["detail"] != "#Humor" ||
		records[0]["predicted_p"] == nil || records[0]["baseline_p"] == nil {
		t.Fatalf("the attempt must be recorded with its frozen prediction: %#v", records)
	}
	if strings.Count(strings.Join(memory.eventTypes(), ","), "memory.recall") != 1 {
		t.Fatalf("recording must not predict again: %v", memory.eventTypes())
	}
}

func TestAFailedDeliveryRecordsNothing(t *testing.T) {
	memory := &recordingMemory{answers: defaultAnswers()}
	deliverer := &orderingDeliverer{memory: memory, err: errors.New("discord down")}

	if err := scheduledRun(scheduledHandler(memory, deliverer, testMemeURL)); err == nil {
		t.Fatal("expected the delivery failure to be reported")
	}
	if records := recordsOf(memory); len(records) != 0 {
		t.Fatalf("a meme that reached no channel is not an example: %#v", records)
	}
}

func TestRepeatedMediaKeepsTheSameRef(t *testing.T) {
	memory := &recordingMemory{answers: defaultAnswers()}
	deliverer := &orderingDeliverer{memory: memory}

	if err := scheduledRun(scheduledHandler(memory, deliverer, testMemeURL, testMemeURL)); err != nil {
		t.Fatal(err)
	}
	records := recordsOf(memory)
	if len(records) != 2 || records[0]["ref"] != records[1]["ref"] {
		t.Fatalf("repeated media keeps today's media-derived ref (ADR 0004): %#v", records)
	}
}

func TestManualSendThroughTheTaskDeliversTheSameText(t *testing.T) {
	memory := &recordingMemory{answers: defaultAnswers()}
	deliverer := &orderingDeliverer{memory: memory}
	handler := NewSystemDomainHandler(nil, nil, nil, deliverer, "channel").
		WithHashtagTask(hashtagTaskWith(relevantKnowledge(true), memory, true), synchronous(memory, nil))

	event, step := discordSendEvent(map[string]any{"channel_ids": []any{"channel"}, "content": "meme", "attachment_url": testMemeURL})
	if _, err := handler.Handle(context.Background(), event, step); err != nil {
		t.Fatal(err)
	}
	if len(deliverer.contents) != 1 || deliverer.contents[0] != "meme\n#Humor" || len(recordsOf(memory)) != 1 {
		t.Fatalf("manual send must keep its text and record once: %q, %#v", deliverer.contents, recordsOf(memory))
	}
}

func TestTheRecordedSnapshotCannotBeChangedAfterTheFact(t *testing.T) {
	memory := &recordingMemory{answers: defaultAnswers()}
	experience := NewHashtagExperience(memory, nil)
	var pending []func()
	experience.spawn = func(work func()) { pending = append(pending, work) }
	attempt := HashtagAttempt{Hashtag: "#Humor", QueryText: testMemeText,
		Prediction: &HashtagPrediction{PredictedP: 0.7, BaselineP: 0.6}}

	experience.Executed(testMemeURL, attempt)
	attempt.Prediction.PredictedP = 0.1
	for _, work := range pending {
		work()
	}
	records := recordsOf(memory)
	if len(records) != 1 || records[0]["predicted_p"] != 0.7 || records[0]["baseline_p"] != 0.6 {
		t.Fatalf("the record must carry the snapshot as frozen: %#v", records)
	}
}

func TestAnUnscoredAttemptIsRecordedWithoutAPrediction(t *testing.T) {
	memory := &recordingMemory{answers: defaultAnswers()}
	synchronous(memory, nil).Executed(testMemeURL, HashtagAttempt{Hashtag: "#Humor", QueryText: testMemeText})

	records := recordsOf(memory)
	if len(records) != 1 || records[0]["predicted_p"] != nil || strings.Contains(strings.Join(memory.eventTypes(), ","), "memory.recall") {
		t.Fatalf("no snapshot means unscored, never predicted afterwards: %#v %v", records, memory.eventTypes())
	}
}
