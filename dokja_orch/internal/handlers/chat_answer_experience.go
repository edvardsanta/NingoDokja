package handlers

import (
	"context"
	"fmt"

	conversation "read_books/dokja_domain/dokja_conversation"
	memorydomain "read_books/dokja_domain/dokja_memory"
	"read_books/internal/logger"
)

// ChatAnswerExperience notes each answer completed in an archived conversation (Desktop, CLI, TUI)
// as a memory experience, with the chance of it being found useful frozen before the owner could
// judge it. The owner's verdict later resolves it through memory.resolve; that pair is what the
// Notebook's scorecard and step 08.3 learn from.
//
// Only owner surfaces reach here: group chats have no verdict to give and their questions are other
// people's. Slow or stopped memory never touches the reply: the work runs after the turn is
// settled, in the background, within experienceBudget.
type ChatAnswerExperience struct {
	domain  *memorydomain.Domain
	enabled func() bool
	spawn   func(func())
}

// NewChatAnswerExperience wires the notes to the memory service. enabled is the operator's memory
// switch, asked on every call; it may be nil.
func NewChatAnswerExperience(service memorydomain.Service, enabled func() bool) *ChatAnswerExperience {
	return &ChatAnswerExperience{domain: memorydomain.New(service), enabled: enabled, spawn: func(work func()) { go work() }}
}

func (e *ChatAnswerExperience) active() bool {
	return e != nil && e.domain != nil && (e.enabled == nil || e.enabled())
}

// Answered notes a completed turn. cited says whether the answer stood on the owner's documents.
// The question goes to memory, never to the log.
func (e *ChatAnswerExperience) Answered(turn conversation.Turn, cited bool) {
	if !e.active() || turn.State != conversation.TurnCompleted {
		return
	}
	answer, ok := memorydomain.PrepareChatAnswer(turn.ID, turn.Content, cited)
	if !ok {
		return
	}
	e.spawn(func() {
		ctx, cancel := context.WithTimeout(context.Background(), experienceBudget)
		defer cancel()
		var snapshot *memorydomain.HashtagPrediction
		if values, err := e.domain.Handle(ctx, answer.PredictionRequest()); err != nil {
			logger.Info(fmt.Sprintf("chat answer prediction unavailable: %v", err))
		} else {
			predicted, okP := numberValue(values["predicted_p"])
			baseline, okB := numberValue(values["baseline_p"])
			if okP && okB {
				snapshot = &memorydomain.HashtagPrediction{PredictedP: predicted, BaselineP: baseline}
			}
		}
		if _, err := e.domain.Handle(ctx, answer.RecordRequest(snapshot)); err != nil {
			logger.Info(fmt.Sprintf("chat answer experience not recorded: %v", err))
		}
	})
}

// feedbackRef is the experience a completed turn's verdict resolves. Turns that are not completed
// have no answer to judge.
func feedbackRef(turn conversation.Turn) string {
	if turn.State != conversation.TurnCompleted || turn.ID == "" {
		return ""
	}
	return memorydomain.ChatAnswerRef(turn.ID)
}
