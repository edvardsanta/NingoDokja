package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// ChatAnswerAction names an answer Ningo gave the owner in an archived conversation (Desktop, CLI,
// TUI). The owner's verdict on it is the outcome: useful is accepted, wrong is replaced, and no
// verdict expires unscored, never as wrong.
const ChatAnswerAction = "chat.answer"

// How an answer stood on the owner's documents, kept as the detail so later scoring can compare
// cited answers with uncited ones.
const (
	ChatAnswerCited   = "cited"
	ChatAnswerUncited = "uncited"
)

// ChatAnswer is an immutable, prepared observation of one completed answer.
type ChatAnswer struct {
	ref      string
	question string
	detail   string
}

// ChatAnswerRef names the experience of one archived turn. It is derived from the turn alone, so a
// replayed or retried send lands on the same experience and the Desktop can resolve a turn read
// back from history.
func ChatAnswerRef(turnID string) string {
	sum := sha256.Sum256([]byte(ChatAnswerAction + ":" + turnID))
	return "chat:" + hex.EncodeToString(sum[:])[:32]
}

// PrepareChatAnswer builds the observation. The question is the context later similarity works
// on; it is the owner's own text, bounded like any other experience context. Without a turn or a
// question there is nothing to note.
func PrepareChatAnswer(turnID, question string, cited bool) (ChatAnswer, bool) {
	question = boundedHashtagText(question, maxContextRunes)
	if strings.TrimSpace(turnID) == "" || question == "" {
		return ChatAnswer{}, false
	}
	detail := ChatAnswerUncited
	if cited {
		detail = ChatAnswerCited
	}
	return ChatAnswer{ref: ChatAnswerRef(turnID), question: question, detail: detail}, true
}

// Ref is the experience's name.
func (a ChatAnswer) Ref() string { return a.ref }

// PredictionRequest asks, before any verdict exists, how likely a similar question's answer was
// found useful.
func (a ChatAnswer) PredictionRequest() Request {
	return hashtagMemoryRequest(ActionPredictExperience, map[string]any{"action": ChatAnswerAction, "context": a.question})
}

// RecordRequest stores the answer with the prediction frozen before the owner could judge it. A
// nil snapshot stays unscored; absence is never replaced by a later prediction.
func (a ChatAnswer) RecordRequest(snapshot *HashtagPrediction) Request {
	record := map[string]any{"ref": a.ref, "action": ChatAnswerAction, "context": a.question, "detail": a.detail}
	if snapshot != nil {
		record["predicted_p"], record["baseline_p"] = snapshot.PredictedP, snapshot.BaselineP
	}
	return hashtagMemoryRequest(ActionRecordExperience, record)
}
