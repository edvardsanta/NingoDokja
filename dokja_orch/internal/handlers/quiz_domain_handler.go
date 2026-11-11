package handlers

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	quiz "read_books/dokja_domain/dokja_quiz"
	"read_books/internal/core"
	"read_books/internal/logger"
)

// quizRunBudget bounds a whole run: two runner calls of up to five minutes each would not fit, but
// a run that reads the questions, answers them in a few model calls and clicks through once does.
// It stays under Discord's fifteen-minute interaction lifetime.
const quizRunBudget = 13 * time.Minute

// maxQuizRunners bounds how many enabled runners are asked for one link.
const maxQuizRunners = 8

// QuizMemory keeps the questionnaires Ningo took, so later conversations remember them.
type QuizMemory interface {
	Save(ctx context.Context, run quiz.Run) error
	Recent(ctx context.Context, limit int) ([]quiz.Run, error)
}

// QuizFeeds is the part of the Feeds client the quiz workflow uses.
type QuizFeeds interface {
	Dispatch(ctx context.Context, eventType string, payload map[string]any) (map[string]any, error)
}

// QuizDomainHandler takes an online questionnaire as Ningo: a Feeds quiz runner reads the questions
// and clicks the answers, the model answers from Ningo's worldview, and the Quiz domain decides what
// counts. Only one run happens at a time, because a run holds a browser and a Feeds plugin slot.
type QuizDomainHandler struct {
	feeds     QuizFeeds
	generator ChatConversationGenerator
	enabled   func() bool
	visions   string
	memory    QuizMemory
	now       func() time.Time
	running   atomic.Bool
}

func NewQuizDomainHandler(feeds QuizFeeds, generator ChatConversationGenerator, enabled func() bool) *QuizDomainHandler {
	return &QuizDomainHandler{feeds: feeds, generator: generator, enabled: enabled, now: time.Now}
}

// WithMemory keeps every finished run, so Ningo remembers it in later conversations.
func (h *QuizDomainHandler) WithMemory(memory QuizMemory) *QuizDomainHandler {
	h.memory = memory
	return h
}

// WithVisions gives the quiz Ningo's convictions, the same ones every chat turn carries.
func (h *QuizDomainHandler) WithVisions(visions string) *QuizDomainHandler {
	h.visions = visions
	return h
}

func (h *QuizDomainHandler) Domain() core.Domain {
	return core.DomainQuiz
}

func (h *QuizDomainHandler) Handle(ctx context.Context, event core.Event, workflow core.WorkflowStep) (map[string]any, error) {
	if workflow.Action != quiz.ActionTake {
		return nil, fmt.Errorf("unsupported quiz action %q", workflow.Action)
	}
	if h == nil || h.feeds == nil || h.generator == nil {
		return nil, fmt.Errorf("quiz workflow is not configured")
	}
	raw, _ := event.Payload["url"].(string)
	link, err := quiz.ParseLink(raw)
	if err != nil {
		return quizRefused(quiz.RefusedInvalidLink), nil
	}
	if h.enabled != nil && !h.enabled() {
		return quizRefused(quiz.RefusedUnavailable), nil
	}
	if !h.running.CompareAndSwap(false, true) {
		return quizRefused(quiz.RefusedBusy), nil
	}
	defer h.running.Store(false)
	ctx, cancel := context.WithTimeout(ctx, quizRunBudget)
	defer cancel()
	result, refusal := h.take(ctx, link, quizLanguage(event))
	if refusal != "" {
		logger.Info(fmt.Sprintf("quiz run refused reason=%s", refusal))
		return quizRefused(refusal), nil
	}
	return result, nil
}

func (h *QuizDomainHandler) take(ctx context.Context, link, language string) (map[string]any, string) {
	runner, questionnaire, refusal := h.questions(ctx, link)
	if refusal != "" {
		return nil, refusal
	}
	answers, refusal := h.answer(ctx, questionnaire.Questions)
	if refusal != "" {
		return nil, refusal
	}
	reply, err := h.invoke(ctx, runner, quiz.RunnerSubmit, map[string]any{"url": link, "answers": answers})
	if err != nil {
		return nil, quiz.RefusedFailed
	}
	if reason, _ := reply["refused"].(string); reason != "" {
		return nil, quiz.Refusal(reason)
	}
	result, err := quiz.ParseResult(link, reply["result"])
	if err != nil {
		return nil, quiz.RefusedFailed
	}
	logger.Info(fmt.Sprintf("quiz run finished questions=%d answered=%d", len(questionnaire.Questions), len(answers)))
	comment := h.comment(ctx, result, language)
	h.remember(ctx, link, questionnaire, result, answers, reply["result"], comment)
	return map[string]any{
		"status": "ok", "url": result.URL, "label": result.Label, "title": questionnaire.Title,
		"answered": len(answers), "comment": comment,
	}, ""
}

// remember keeps the finished run with the questions the site actually asked, when the runner
// says which. A storage failure never costs the person their result.
func (h *QuizDomainHandler) remember(ctx context.Context, link string, questionnaire quiz.Quiz, result quiz.Result, answers map[string]string, raw any, comment string) {
	if h.memory == nil {
		return
	}
	reported, _ := raw.(map[string]any)
	asked, err := quiz.ParseAsked(reported["asked"], questionnaire)
	if err != nil {
		logger.Error("quiz runner reported invalid asked questions", err)
		asked = nil
	}
	run := quiz.NewRun(link, questionnaire, result, answers, asked, comment, h.now())
	if err := h.memory.Save(ctx, run); err != nil {
		logger.Error("quiz run was not remembered", err)
		return
	}
	logger.Info(fmt.Sprintf("quiz run remembered items=%d", len(run.Items)))
}

// questions asks the enabled runners, in id order, and keeps the first that handles the link. A
// runner that refuses ends the search: it said the link is its own.
func (h *QuizDomainHandler) questions(ctx context.Context, link string) (string, quiz.Quiz, string) {
	runners, err := h.runners(ctx)
	if err != nil {
		return "", quiz.Quiz{}, quiz.RefusedUnavailable
	}
	if len(runners) == 0 {
		return "", quiz.Quiz{}, quiz.RefusedNoRunner
	}
	failures := 0
	for _, runner := range runners {
		reply, err := h.invoke(ctx, runner, quiz.RunnerQuestions, map[string]any{"url": link})
		if err != nil {
			if ctx.Err() != nil {
				return "", quiz.Quiz{}, quiz.RefusedFailed
			}
			failures++
			continue
		}
		if handled, _ := reply["handled"].(bool); !handled {
			continue
		}
		if reason, _ := reply["refused"].(string); reason != "" {
			return "", quiz.Quiz{}, quiz.Refusal(reason)
		}
		questionnaire, err := quiz.ParseQuiz(reply["quiz"])
		if err != nil {
			logger.Error("quiz runner returned an invalid quiz", err)
			return "", quiz.Quiz{}, quiz.RefusedFailed
		}
		return runner, questionnaire, ""
	}
	if failures > 0 {
		return "", quiz.Quiz{}, quiz.RefusedFailed
	}
	return "", quiz.Quiz{}, quiz.RefusedUnsupported
}

// answer puts every question to Ningo in batches, retrying a batch's unanswered items once. A
// question left unanswered stops the run: submitting a partial questionnaire would misreport him.
func (h *QuizDomainHandler) answer(ctx context.Context, questions []quiz.Question) (map[string]string, string) {
	answers := map[string]string{}
	for _, batch := range quiz.Batches(questions, quiz.BatchSize) {
		pending := batch
		for attempt := 0; attempt < 2 && len(pending) > 0; attempt++ {
			reply, err := h.answerBatch(ctx, pending)
			if err != nil {
				if ctx.Err() != nil {
					return nil, quiz.RefusedFailed
				}
				continue
			}
			got, missing := quiz.ParseAnswers(reply, pending)
			for id, option := range got {
				answers[id] = option
			}
			pending = quiz.Pending(pending, missing)
		}
		if len(pending) > 0 {
			logger.Info(fmt.Sprintf("quiz run left questions unanswered count=%d", len(pending)))
			return nil, quiz.RefusedUnanswered
		}
	}
	return answers, ""
}

// comment is Ningo's reaction to the verdict. Without one the link still stands.
func (h *QuizDomainHandler) comment(ctx context.Context, result quiz.Result, language string) string {
	resultMap, err := generateLightweightText(ctx, h.generator, []map[string]string{
		{"role": "system", "content": h.persona()},
		{"role": "user", "content": quiz.CommentPrompt(result, language)},
	}, nil)
	if err != nil {
		logger.Error("quiz comment was not generated", err)
		return ""
	}
	return strings.TrimSpace(chatReplyFromResult(resultMap))
}

func (h *QuizDomainHandler) answerBatch(ctx context.Context, questions []quiz.Question) (string, error) {
	task, ok := h.generator.(taskJSONGenerator)
	if !ok {
		return h.generate(ctx, quiz.BatchPrompt(questions))
	}
	schema := map[string]any{
		"type": "object", "additionalProperties": false, "required": []string{"answers"},
		"properties": map[string]any{"answers": map[string]any{
			"type": "array", "minItems": 1, "maxItems": len(questions),
			"items": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"id", "option"},
				"properties": map[string]any{"id": map[string]any{"type": "string"}, "option": map[string]any{"type": "string"}},
			},
		}},
	}
	result, err := task.GenerateTaskJSON(ctx, "lightweight", schema, []map[string]string{
		{"role": "system", "content": h.persona()}, {"role": "user", "content": quiz.BatchPrompt(questions)},
	}, func(reply map[string]any) error {
		text, _ := reply["reply"].(string)
		answers, _ := quiz.ParseAnswers(text, questions)
		if len(answers) == 0 {
			return fmt.Errorf("no valid quiz answers")
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	text, _ := result["reply"].(string)
	return text, nil
}

func (h *QuizDomainHandler) generate(ctx context.Context, prompt string) (string, error) {
	result, err := h.generator.GenerateMessages(ctx, []map[string]string{
		{"role": "system", "content": h.persona()},
		{"role": "user", "content": prompt},
	})
	if err != nil {
		return "", err
	}
	reply, _ := result["reply"].(string)
	if strings.TrimSpace(reply) == "" {
		return "", fmt.Errorf("empty reply")
	}
	return reply, nil
}

// persona is Ningo's own: the public persona and, when configured, his private worldview.
func (h *QuizDomainHandler) persona() string {
	persona := chatPersona
	if h.visions != "" {
		persona += "\n\n" + fmt.Sprintf(visionsNote, h.visions)
	}
	return persona
}

func (h *QuizDomainHandler) invoke(ctx context.Context, runner, action string, input map[string]any) (map[string]any, error) {
	return h.feeds.Dispatch(ctx, "feeds.invoke", map[string]any{
		"capability": quiz.Capability, "action": action, "plugin": runner, "input": input,
	})
}

// runners lists the enabled providers of the capability by id, so the order never depends on the
// plugins directory.
func (h *QuizDomainHandler) runners(ctx context.Context) ([]string, error) {
	status, err := h.feeds.Dispatch(ctx, "feeds.status", nil)
	if err != nil {
		return nil, err
	}
	rows, _ := status["providers"].([]any)
	var ids []string
	for _, row := range rows {
		provider, _ := row.(map[string]any)
		id, _ := provider["id"].(string)
		if enabled, _ := provider["enabled"].(bool); !enabled || id == "" {
			continue
		}
		provides, _ := provider["provides"].([]any)
		for _, capability := range provides {
			if capability == quiz.Capability {
				ids = append(ids, id)
				break
			}
		}
	}
	sort.Strings(ids)
	if len(ids) > maxQuizRunners {
		ids = ids[:maxQuizRunners]
	}
	return ids, nil
}

// quizLanguage is the language of Ningo's comment: the interface's locale, Portuguese by default.
func quizLanguage(event core.Event) string {
	locale, _ := event.Payload["locale"].(string)
	if strings.HasPrefix(strings.ToLower(locale), "en") {
		return "English"
	}
	return "Brazilian Portuguese"
}

func quizRefused(reason string) map[string]any {
	return map[string]any{"status": "refused", "refused": reason}
}
