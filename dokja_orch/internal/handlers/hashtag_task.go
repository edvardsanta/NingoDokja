package handlers

import (
	"context"
	"fmt"
	knowledgedomain "read_books/dokja_domain/dokja_knowledge"
	memorydomain "read_books/dokja_domain/dokja_memory"
	"read_books/internal/core"
	"read_books/internal/logger"
	"strings"
	"time"
)

const (
	hashtagTaskVersion = "1"
	// hashtagTaskDeadline covers today's worst case: the OCR call (a 5 s send and a 5 s
	// receive timeout), the 5 s Knowledge search and the prediction budget.
	hashtagTaskDeadline = 20 * time.Second
	// hashtagPredictionBudget is how long the shadow prediction may hold a delivery. On a miss
	// the delivery goes ahead and the attempt is recorded unscored (ADR 0004).
	hashtagPredictionBudget = time.Second
	hashtagPredictionMethod = "memory.predict"
)

// HashtagPrediction is the shadow snapshot frozen before the action. It is a copy: nothing
// that happens later changes it, and it never decides what is delivered.
type HashtagPrediction = memorydomain.HashtagPrediction

// HashtagAttempt is one suggestion attempt. Hashtag is empty when the task abstained or
// could not finish; Prediction is nil when the attempt is unscored.
type HashtagAttempt struct {
	AttemptID  string
	Hashtag    string
	QueryText  string
	Match      HashtagMatch
	Prediction *HashtagPrediction
	Task       core.TaskResult
}

// HashtagSuggestionTask is what the delivery handlers need from the task.
type HashtagSuggestionTask interface {
	Suggest(ctx context.Context, event core.Event, url string) HashtagAttempt
}

// HashtagTask runs the hashtag suggestion as a typed task (step 06): Reading (the meme's
// text), Knowledge (the closest labelled example), Attention (whether it is relevant) and
// Prediction (a shadow estimate, optional). Delivery stays with the caller.
type HashtagTask struct {
	hashtags      *MemeHashtagWorkflow
	memory        *memorydomain.Domain
	memoryEnabled func() bool
	budget        time.Duration
	deadline      time.Duration
}

func NewHashtagTask(hashtags *MemeHashtagWorkflow, memory memorydomain.Service, memoryEnabled func() bool) *HashtagTask {
	task := &HashtagTask{hashtags: hashtags, memoryEnabled: memoryEnabled, budget: hashtagPredictionBudget, deadline: hashtagTaskDeadline}
	if memory != nil {
		task.memory = memorydomain.New(memory)
	}
	return task
}

func (t *HashtagTask) predicting() bool {
	return t.memory != nil && (t.memoryEnabled == nil || t.memoryEnabled())
}

func (t *HashtagTask) Suggest(ctx context.Context, event core.Event, url string) HashtagAttempt {
	url = strings.TrimSpace(url)
	if t == nil || url == "" || !t.hashtags.knowledgeEnabled() {
		return HashtagAttempt{}
	}
	steps := []core.TaskStep{
		{Name: "read", Domain: core.DomainMeme, Action: "read-meme-text", Run: t.read(url)},
		{Name: "retrieve", Domain: core.DomainKnowledge, Action: "search-knowledge", Run: t.retrieve},
		{Name: "select", Domain: core.DomainMeme, Action: "select-hashtag", Run: selectHashtag},
	}
	if t.predicting() {
		steps = append(steps, core.TaskStep{
			Name: "predict", Domain: core.DomainMemory, Action: "predict-experience", Optional: true, Run: t.predict,
		})
	}
	result, err := core.RunTask(ctx, event, core.TypedWorkflow{
		Name: "hashtag-suggestion", Version: hashtagTaskVersion, Goal: "suggest a learned hashtag for a meme",
		Deadline: t.deadline, Steps: steps,
	}, core.PrototypeTaskLimits())
	if err != nil {
		logger.Info(fmt.Sprintf("hashtag task was not started: %v", err))
		return HashtagAttempt{}
	}
	attempt := HashtagAttempt{AttemptID: result.AttemptID, Task: result}
	selected := result.Outputs["select"]
	if hashtag, _ := selected["hashtag"].(string); hashtag != "" {
		retrieved := result.Outputs["retrieve"]
		score, _ := retrieved["score"].(float64)
		attempt.Hashtag = hashtag
		attempt.QueryText = resultString(retrieved, "query_text")
		attempt.Match = HashtagMatch{Score: score, Text: resultString(retrieved, "matched_text")}
		attempt.Prediction = snapshotFrom(result.Outputs["predict"])
	}
	// Only the shape of what happened is logged; the address and the text are the user's.
	logger.Info(fmt.Sprintf("hashtag task attempt=%s status=%s reason=%s suggested=%t scored=%t",
		result.AttemptID, result.Status, result.Reason, attempt.Hashtag != "", attempt.Prediction != nil))
	return attempt
}

func (t *HashtagTask) read(url string) core.TaskStepFunc {
	return func(ctx context.Context, in core.StepInput) (core.StepResult, error) {
		text, err := t.hashtags.text(ctx, url, "")
		if err != nil {
			return core.StepResult{Disposition: core.StepFail, Reason: "media_text_unavailable"}, nil
		}
		return core.StepResult{
			Disposition: core.StepContinue,
			Output:      map[string]any{"text": text},
			Evidence: []core.Evidence{{
				Ref: memorydomain.HashtagExperienceRef(url), Capability: "meme.text.read", ObservedAt: time.Now().UTC(),
				Scope: in.Scope, Reason: "text read from the meme",
			}},
		}, nil
	}
}

func (t *HashtagTask) retrieve(ctx context.Context, in core.StepInput) (core.StepResult, error) {
	text, _ := in.Outputs["read"]["text"].(string)
	answer, err := t.hashtags.search(ctx, text, nil)
	if err != nil {
		return core.StepResult{Disposition: core.StepFail, Reason: "knowledge_unavailable"}, nil
	}
	result := core.StepResult{Disposition: core.StepContinue, Output: answer}
	if source := resultString(answer, "matched_source_url"); source != "" {
		// Knowledge has no scope model: the evidence stays unscoped.
		result.Evidence = []core.Evidence{{
			Ref: knowledgedomain.HashtagKnowledgeID(source), Capability: "knowledge.search", ObservedAt: time.Now().UTC(),
			Reason: "closest labelled example",
		}}
	}
	return result, nil
}

// selectHashtag is the attention decision: the closest example's hashtag is used only when
// Knowledge judged it relevant under the configured threshold.
func selectHashtag(_ context.Context, in core.StepInput) (core.StepResult, error) {
	retrieved := in.Outputs["retrieve"]
	relevant, _ := retrieved["relevant"].(bool)
	hashtag := strings.TrimSpace(resultString(retrieved, "hashtag"))
	if !relevant || hashtag == "" {
		reason := "below_threshold"
		if hashtag == "" {
			reason = "no_example"
		}
		return core.StepResult{Disposition: core.StepAbstain, Reason: reason}, nil
	}
	var refs []string
	for _, evidence := range in.Evidence {
		refs = append(refs, evidence.Ref)
	}
	return core.StepResult{
		Disposition: core.StepContinue,
		Output:      map[string]any{"hashtag": hashtag},
		Proposals: []core.Proposal{{
			ActionID: "append-hashtag", Domain: core.DomainMeme, Decision: "append", EvidenceRefs: refs,
			Reason: "relevant labelled example",
		}},
	}, nil
}

func (t *HashtagTask) predict(ctx context.Context, in core.StepInput) (core.StepResult, error) {
	ctx, cancel := context.WithTimeout(ctx, t.budget)
	defer cancel()
	text, _ := in.Outputs["read"]["text"].(string)
	type answer struct {
		values map[string]any
		err    error
	}
	done := make(chan answer, 1)
	go func() {
		values, err := t.memory.Handle(ctx, memorydomain.HashtagPredictionRequest(text))
		done <- answer{values, err}
	}()
	select {
	case <-ctx.Done():
		return core.StepResult{Disposition: core.StepFail, Reason: "prediction_budget_missed"}, nil
	case got := <-done:
		if got.err != nil {
			return core.StepResult{Disposition: core.StepFail, Reason: "prediction_unavailable"}, nil
		}
		predicted, okP := numberValue(got.values["predicted_p"])
		baseline, okB := numberValue(got.values["baseline_p"])
		if !okP || !okB {
			return core.StepResult{Disposition: core.StepFail, Reason: "prediction_unusable"}, nil
		}
		support, _ := numberValue(got.values["support"])
		resolved, _ := numberValue(got.values["resolved"])
		insufficient, _ := got.values["insufficient"].(bool)
		return core.StepResult{Disposition: core.StepContinue, Output: map[string]any{
			"method": hashtagPredictionMethod, "predicted_p": predicted, "baseline_p": baseline,
			"support": support, "resolved": resolved, "insufficient": insufficient,
			"reason": resultString(got.values, "reason"),
		}}, nil
	}
}

func snapshotFrom(output map[string]any) *HashtagPrediction {
	predicted, okP := output["predicted_p"].(float64)
	baseline, okB := output["baseline_p"].(float64)
	if !okP || !okB {
		return nil
	}
	support, _ := output["support"].(float64)
	resolved, _ := output["resolved"].(float64)
	insufficient, _ := output["insufficient"].(bool)
	return &HashtagPrediction{
		Method: resultString(output, "method"), PredictedP: predicted, BaselineP: baseline,
		Support: support, Resolved: resolved, Insufficient: insufficient, Reason: resultString(output, "reason"),
	}
}
