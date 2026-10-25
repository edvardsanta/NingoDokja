package handlers

import (
	"context"
	"fmt"
	memorydomain "read_books/dokja_domain/dokja_memory"
	"read_books/internal/logger"
	"time"
)

// experienceBudget bounds one background memory workflow. Slow memory never
// delays or fails a delivery.
const experienceBudget = 5 * time.Second

// HashtagMatch remains an alias for callers while Memory owns evidence rules.
type HashtagMatch = memorydomain.HashtagMatch

// HashtagExperienceNotes is what the handlers tell the experience memory about hashtags.
type HashtagExperienceNotes interface {
	// Suggested notes that hashtag was appended to the meme at url, whose text is text, and what
	// the suggestion rested on.
	Suggested(url, text, hashtag string, match HashtagMatch)
	// Tagged notes that the operator tagged the meme at url with hashtag.
	Tagged(url, hashtag string)
}

// HashtagExperience records the bot's hashtag suggestions in the experience memory and, when
// the operator later tags the same meme, resolves each as accepted (the same hashtag) or
// replaced (another one). It runs in shadow mode: it only records and scores what the
// suggestions would have been predicted to do; nothing it learns changes what is suggested.
//
// A meme's text and address are the user's own material: they go to the memory service and are
// never logged here, only the shape of what happened.
type HashtagExperience struct {
	domain  *memorydomain.Domain
	enabled func() bool
	spawn   func(func())
}

// NewHashtagExperience wires the notes to the memory service. enabled is asked on every call so
// the operator's switch takes effect without a restart; it may be nil.
func NewHashtagExperience(service memorydomain.Service, enabled func() bool) *HashtagExperience {
	return &HashtagExperience{
		domain:  memorydomain.New(service),
		enabled: enabled,
		spawn:   func(work func()) { go work() },
	}
}

func (e *HashtagExperience) active() bool {
	return e != nil && e.domain != nil && (e.enabled == nil || e.enabled())
}

func (e *HashtagExperience) Suggested(url, text, hashtag string, match HashtagMatch) {
	if !e.active() {
		return
	}
	observation, ok := memorydomain.PrepareHashtagExperience(url, text, hashtag, match)
	if !ok {
		return
	}
	e.spawn(func() {
		ctx, cancel := context.WithTimeout(context.Background(), experienceBudget)
		defer cancel()

		prediction, err := e.domain.Handle(ctx, observation.PredictionRequest())
		var snapshot *HashtagPrediction
		if err != nil {
			logger.Info(fmt.Sprintf("hashtag experience could not predict, recording without a score: %v", err))
		} else {
			predicted, _ := prediction["predicted_p"].(float64)
			baseline, _ := prediction["baseline_p"].(float64)
			snapshot = &HashtagPrediction{PredictedP: predicted, BaselineP: baseline}
		}
		e.recordIn(ctx, observation.RecordRequest(snapshot), snapshot != nil)
	})
}

// Executed records a delivered attempt using its frozen prediction. An attempt
// without a prediction stays unscored.
func (e *HashtagExperience) Executed(url string, attempt HashtagAttempt) {
	if !e.active() {
		return
	}
	observation, ok := memorydomain.PrepareHashtagExperience(url, attempt.QueryText, attempt.Hashtag, attempt.Match)
	if !ok {
		return
	}
	// Build the request before scheduling so later mutation of the attempt's
	// prediction cannot alter the recorded snapshot.
	request := observation.RecordRequest(attempt.Prediction)
	e.spawn(func() { e.record(request, attempt.Prediction != nil) })
}

func (e *HashtagExperience) Tagged(url, hashtag string) {
	if !e.active() {
		return
	}
	request, ok := memorydomain.HashtagFeedbackRequest(url, hashtag)
	if !ok {
		return
	}
	e.spawn(func() { e.resolveSuggestion(request) })
}

func (e *HashtagExperience) record(request memorydomain.Request, scored bool) {
	ctx, cancel := context.WithTimeout(context.Background(), experienceBudget)
	defer cancel()
	e.recordIn(ctx, request, scored)
}

func (e *HashtagExperience) recordIn(ctx context.Context, request memorydomain.Request, scored bool) {
	answer, err := e.domain.Handle(ctx, request)
	if err != nil {
		logger.Info(fmt.Sprintf("hashtag experience could not be recorded: %v", err))
		return
	}
	logger.Info(fmt.Sprintf("hashtag experience recorded created=%v scored=%t", answer["created"], scored))
}

func (e *HashtagExperience) resolveSuggestion(request memorydomain.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), experienceBudget)
	defer cancel()

	answer, err := e.domain.Handle(ctx, request)
	if err != nil {
		logger.Info(fmt.Sprintf("hashtag experience could not be resolved: %v", err))
		return
	}
	logger.Info(fmt.Sprintf(
		"hashtag experience resolved found=%v resolved=%v matched=%v", answer["found"], answer["resolved"], answer["matched"],
	))
}
