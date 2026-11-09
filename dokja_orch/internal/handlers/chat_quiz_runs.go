package handlers

import (
	"context"

	quiz "read_books/dokja_domain/dokja_quiz"
	"read_books/internal/core"
	"read_books/internal/logger"
)

// QuizRunReader reads the questionnaires Ningo took.
type QuizRunReader interface {
	Recent(ctx context.Context, limit int) ([]quiz.Run, error)
}

// WithQuizRuns lets every chat turn remember the questionnaires Ningo took, as his own experience.
func (h *ChatDomainHandler) WithQuizRuns(runs QuizRunReader) *ChatDomainHandler {
	h.quizRuns = runs
	return h
}

// attachQuizRuns gives the turn the recent runs. Spoken turns get the results without the answers,
// to stay short; written turns get the newest run's answers too, so a follow-up such as "why?"
// still has them. Only the handler grants this key: withOwnerInterface drops an interface's copy.
func (h *ChatDomainHandler) attachQuizRuns(ctx context.Context, event core.Event) {
	if h.quizRuns == nil || event.Context == nil {
		return
	}
	runs, err := h.quizRuns.Recent(ctx, quiz.RememberedRuns)
	if err != nil {
		logger.Error("read remembered quiz runs", err)
		return
	}
	_, spoken := event.Context["voice_turns"]
	if data := quiz.MemoryData(runs, !spoken && !spokenDesktopTurn(event)); data != "" {
		event.Context["ningo_quiz_runs"] = data
	}
}
