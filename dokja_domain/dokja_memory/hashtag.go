package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"strings"
	"unicode/utf8"
)

const hashtagSuggestAction = "hashtag.suggest"

// HashtagMatch describes the labelled example a suggestion rested on.
type HashtagMatch struct {
	Score float64
	Text  string
}

// HashtagPrediction is the shadow snapshot frozen before delivery. It never
// selects content, and recording it must not make a new prediction.
type HashtagPrediction struct {
	Method       string
	PredictedP   float64
	BaselineP    float64
	Support      float64
	Resolved     float64
	Insufficient bool
	Reason       string
}

// HashtagExperience is an immutable, prepared observation of a used suggestion.
// The orchestrator decides when delivery has succeeded and supplies that fact.
type HashtagExperience struct {
	ref     string
	context string
	hashtag string
	match   HashtagMatch
}

// PrepareHashtagExperience preserves the existing optional-note behavior. Blank
// inputs produce no note; the ordinary Memory request validator still checks
// labels, evidence and probabilities before any storage call.
func PrepareHashtagExperience(sourceURL, context, hashtag string, match HashtagMatch) (HashtagExperience, bool) {
	context = boundedHashtagText(context, maxContextRunes)
	hashtag = strings.TrimSpace(hashtag)
	if sourceURL == "" || context == "" || hashtag == "" {
		return HashtagExperience{}, false
	}
	match.Text = boundedHashtagText(match.Text, maxMatchedRunes)
	return HashtagExperience{ref: HashtagExperienceRef(sourceURL), context: context, hashtag: hashtag, match: match}, true
}

// HashtagExperienceRef retains the legacy media-derived identity shared by
// recording, explicit feedback and task evidence. It does not retain the URL.
func HashtagExperienceRef(sourceURL string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(sourceURL)))
	return "hashtag:" + hex.EncodeToString(sum[:])[:32]
}

// HashtagPredictionRequest uses only bounded text and the existing action name.
func HashtagPredictionRequest(context string) Request {
	return hashtagMemoryRequest(ActionPredictExperience, map[string]any{
		"action": hashtagSuggestAction, "context": boundedHashtagText(context, maxContextRunes),
	})
}

func (e HashtagExperience) PredictionRequest() Request {
	return HashtagPredictionRequest(e.context)
}

// RecordRequest copies the supplied frozen probabilities into a fresh payload.
// A nil snapshot remains unscored; absence is never replaced by a prediction.
func (e HashtagExperience) RecordRequest(snapshot *HashtagPrediction) Request {
	record := map[string]any{
		"ref": e.ref, "action": hashtagSuggestAction, "context": e.context, "detail": e.hashtag,
	}
	if e.match.Score > 0 {
		record["matched_score"] = math.Min(e.match.Score, 1)
	}
	if e.match.Text != "" {
		record["matched_context"] = e.match.Text
	}
	if snapshot != nil {
		record["predicted_p"], record["baseline_p"] = snapshot.PredictedP, snapshot.BaselineP
	}
	return hashtagMemoryRequest(ActionRecordExperience, record)
}

// HashtagFeedbackRequest records an observation for the existing accepted versus
// replaced comparison. Preparing feedback never creates a stored experience.
func HashtagFeedbackRequest(sourceURL, hashtag string) (Request, bool) {
	hashtag = strings.TrimSpace(hashtag)
	if sourceURL == "" || hashtag == "" {
		return Request{}, false
	}
	return hashtagMemoryRequest(ActionResolveExperience, map[string]any{
		"ref": HashtagExperienceRef(sourceURL), "observed": hashtag,
	}), true
}

func hashtagMemoryRequest(action string, payload map[string]any) Request {
	return Request{Action: action, Event: Event{Source: "orchestrator", Payload: payload}}
}

func boundedHashtagText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) > limit {
		return string([]rune(value)[:limit])
	}
	return value
}
