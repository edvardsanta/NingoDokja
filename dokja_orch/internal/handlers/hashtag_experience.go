package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	memorydomain "read_books/dokja_domain/dokja_memory"
	"read_books/internal/logger"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// hashtagSuggestAction names what the bot did when it appended a learned hashtag.
	hashtagSuggestAction = "hashtag.suggest"
	// experienceBudget is how long one memory call may take. The calls run in the background and
	// are best effort: a slow or unreachable memory never delays or fails a delivery.
	experienceBudget  = 5 * time.Second
	maxExperienceText = 4000
	// maxMatchedText is how much of the earlier example's text a suggestion keeps as its evidence.
	maxMatchedText = 200
)

// HashtagMatch is what a suggestion rested on: the earlier tagged meme it was closest to. A zero
// value means the service did not say.
type HashtagMatch struct {
	// Score is how close that meme was, 0 to 1.
	Score float64
	// Text is what was read from that meme. It is the operator's own material.
	Text string
}

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
	text, hashtag = truncateRunes(strings.TrimSpace(text), maxExperienceText), strings.TrimSpace(hashtag)
	if !e.active() || url == "" || text == "" || hashtag == "" {
		return
	}
	e.spawn(func() { e.recordSuggestion(experienceRef(url), text, hashtag, match) })
}

func (e *HashtagExperience) Tagged(url, hashtag string) {
	hashtag = strings.TrimSpace(hashtag)
	if !e.active() || url == "" || hashtag == "" {
		return
	}
	e.spawn(func() { e.resolveSuggestion(experienceRef(url), hashtag) })
}

func (e *HashtagExperience) recordSuggestion(ref, text, hashtag string, match HashtagMatch) {
	ctx, cancel := context.WithTimeout(context.Background(), experienceBudget)
	defer cancel()

	record := map[string]any{"ref": ref, "action": hashtagSuggestAction, "context": text, "detail": hashtag}
	if match.Score > 0 {
		record["matched_score"] = math.Min(match.Score, 1)
	}
	if matched := truncateRunes(strings.TrimSpace(match.Text), maxMatchedText); matched != "" {
		record["matched_context"] = matched
	}
	// The prediction is made before the experience exists, so it only uses what came earlier. If
	// it cannot be made the experience is still worth keeping: it just has nothing to score.
	prediction, err := e.domain.Handle(ctx, memoryRequest(memorydomain.ActionPredictExperience, map[string]any{
		"action": hashtagSuggestAction, "context": text,
	}))
	scored := err == nil
	if scored {
		record["predicted_p"], record["baseline_p"] = prediction["predicted_p"], prediction["baseline_p"]
	} else {
		logger.Info(fmt.Sprintf("hashtag experience could not predict, recording without a score: %v", err))
	}

	answer, err := e.domain.Handle(ctx, memoryRequest(memorydomain.ActionRecordExperience, record))
	if err != nil {
		logger.Info(fmt.Sprintf("hashtag experience could not be recorded: %v", err))
		return
	}
	logger.Info(fmt.Sprintf("hashtag experience recorded created=%v scored=%t", answer["created"], scored))
}

func (e *HashtagExperience) resolveSuggestion(ref, hashtag string) {
	ctx, cancel := context.WithTimeout(context.Background(), experienceBudget)
	defer cancel()

	answer, err := e.domain.Handle(ctx, memoryRequest(memorydomain.ActionResolveExperience, map[string]any{
		"ref": ref, "observed": hashtag,
	}))
	if err != nil {
		logger.Info(fmt.Sprintf("hashtag experience could not be resolved: %v", err))
		return
	}
	logger.Info(fmt.Sprintf(
		"hashtag experience resolved found=%v resolved=%v matched=%v", answer["found"], answer["resolved"], answer["matched"],
	))
}

func memoryRequest(action string, payload map[string]any) memorydomain.Request {
	return memorydomain.Request{Action: action, Event: memorydomain.Event{Source: "orchestrator", Payload: payload}}
}

// experienceRef identifies a meme without keeping its address: the same address always gives the
// same ref, so the tag that comes later finds the suggestion that came first.
func experienceRef(url string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(url)))
	return "hashtag:" + hex.EncodeToString(sum[:])[:32]
}

func truncateRunes(value string, limit int) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	return string([]rune(value)[:limit])
}
