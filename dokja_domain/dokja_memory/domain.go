// Package memory is the experience memory's business rules: what counts as an experience,
// how a prediction is made from the closest earlier ones and how it is scored.
//
// An experience is "in this context the bot took this action, and this was the outcome".
// The domain never touches storage or embeddings; the service behind Service only stores
// experiences and finds the closest ones. It answers these events:
//
//	memory.record    {ref, action, context, detail?, predicted_p?, baseline_p?, matched_score?, matched_context?}
//	                                                                    -> {created, embedded, degraded}
//	memory.resolve   {ref, outcome}                                     -> {found, resolved, outcome}
//	memory.get       {ref}  -> {found, action, detail, outcome, predicted_p, baseline_p, matched_score,
//	                     created_at, resolved_at}
//	memory.recall    {context, action?, k?, include_context?}
//	                 -> {neighbors[{ref, action, detail, outcome, similarity, context_snippet?}],
//	                     outcomes{outcome: count}, degraded, reason}
//	memory.list      {action?, state?, limit?, offset?, include_context?}
//	                 -> {experiences[{ref, action, detail, outcome, predicted_p, baseline_p, matched_score,
//	                     created_at, resolved_at, context_snippet?, matched_snippet?}], total, limit, offset}
//	memory.resolved  {action?, limit?}       -> {experiences[{ref, action, outcome, predicted_p, baseline_p}]}
//	memory.forget    {ref}                                              -> {deleted}
//	memory.status    {}                                                 -> counts and embedder state
//	memory.reindex   {limit?}                                           -> {embedded, remaining}
//
// detail is what exactly the bot did (the label it suggested, say), so a later observation can be
// compared with it. A context is the user's own text: nothing here logs it, and it comes back in one
// case only, as a short snippet when a caller sets include_context (a listing, or the evidence of a
// prediction), so an operator can recognize an experience whose ref is a hash.
//
// matched_score and matched_context are what a choice rested on: how close the earlier example was
// and the start of its text. The text follows the same rule as a context (matched_snippet, only
// with include_context); the score is a number and always comes back.
package memory

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	ActionRecordExperience  = "record-experience"
	ActionResolveExperience = "resolve-experience"
	ActionRecallExperience  = "recall-experience"
	ActionGetExperience     = "get-experience"
	ActionListExperience    = "list-experience"
	ActionPredictExperience = "predict-experience"
	ActionScoreExperience   = "score-experience"
	ActionForgetExperience  = "forget-experience"
	ActionInspectMemory     = "inspect-memory"
	ActionReindexMemory     = "reindex-memory"
)

const (
	maxRefLen        = 512
	maxContextRunes  = 4000
	maxDetailRunes   = 200
	maxMatchedRunes  = 200
	defaultRecallK   = 10
	maxRecallK       = 50
	predictNeighbors = 20
	scoreWindow      = 5000
	maxReindexBatch  = 500
	defaultListLimit = 20
	maxListLimit     = 100
	maxListOffset    = 1_000_000
)

// actionName is what a caller calls the thing it did, such as "hashtag.suggest".
var actionName = regexp.MustCompile(`^[a-z][a-z0-9_.:-]{0,63}$`)

type Event struct {
	ID      string
	Source  string
	Type    string
	Payload map[string]any
	Context map[string]any
}

type Request struct {
	Event  Event
	Action string
}

// Service is the experience store. Dispatch sends one memory.* event to it.
type Service interface {
	Dispatch(ctx context.Context, eventType string, payload map[string]any) (map[string]any, error)
}

type Domain struct {
	service Service
	config  Config
}

func New(service Service) *Domain {
	return &Domain{service: service, config: DefaultConfig()}
}

// WithConfig replaces the prediction thresholds, which are provisional until there is
// real data to calibrate them against.
func (d *Domain) WithConfig(config Config) *Domain {
	d.config = config
	return d
}

// forwarded actions need no rule of their own beyond a valid request: the domain checks
// it, then sends a clean copy (only the known fields, trimmed) to the service.
var forwarded = map[string]struct {
	eventType string
	build     func(payload map[string]any) (map[string]any, error)
}{
	ActionRecordExperience: {"memory.record", buildRecord},
	ActionRecallExperience: {"memory.recall", buildRecall},
	ActionGetExperience:    {"memory.get", buildGet},
	ActionListExperience:   {"memory.list", buildList},
	ActionForgetExperience: {"memory.forget", buildForget},
	ActionInspectMemory:    {"memory.status", func(map[string]any) (map[string]any, error) { return map[string]any{}, nil }},
	ActionReindexMemory:    {"memory.reindex", buildReindex},
}

func (d *Domain) Handle(ctx context.Context, request Request) (map[string]any, error) {
	if d == nil {
		return nil, fmt.Errorf("memory domain is nil")
	}
	if d.service == nil {
		return nil, fmt.Errorf("memory service is not configured")
	}

	payload := request.Event.Payload
	switch request.Action {
	case ActionResolveExperience:
		return d.resolve(ctx, payload)
	case ActionPredictExperience:
		return d.predict(ctx, payload)
	case ActionScoreExperience:
		return d.score(ctx, payload)
	}

	route, ok := forwarded[request.Action]
	if !ok {
		return nil, fmt.Errorf("unsupported memory action %q for event type %q", request.Action, request.Event.Type)
	}
	clean, err := route.build(payload)
	if err != nil {
		return nil, err
	}
	return d.service.Dispatch(ctx, route.eventType, clean)
}

// resolve gives an experience its outcome, either stated (outcome) or worked out from what
// was observed afterwards (observed): the bot's choice was accepted when the observation is
// the very thing it did, and replaced when it is something else.
func (d *Domain) resolve(ctx context.Context, payload map[string]any) (map[string]any, error) {
	ref, err := requiredRef(payload)
	if err != nil {
		return nil, err
	}
	outcome, observed := text(payload, "outcome"), text(payload, "observed")
	switch {
	case outcome != "" && observed != "":
		return nil, fmt.Errorf("send either outcome or observed, not both")
	case outcome == "" && observed == "":
		return nil, fmt.Errorf("outcome or observed is required")
	case outcome != "":
		if !validOutcome(outcome) {
			return nil, fmt.Errorf("outcome must be one of %s", strings.Join(Outcomes, ", "))
		}
		return d.service.Dispatch(ctx, "memory.resolve", map[string]any{"ref": ref, "outcome": outcome})
	}

	if utf8.RuneCountInString(observed) > maxDetailRunes || strings.IndexFunc(observed, unicode.IsControl) >= 0 {
		return nil, fmt.Errorf("observed must be at most %d characters and have no control characters", maxDetailRunes)
	}
	stored, err := d.service.Dispatch(ctx, "memory.get", map[string]any{"ref": ref})
	if err != nil {
		return nil, err
	}
	if found, _ := stored["found"].(bool); !found {
		return map[string]any{"found": false, "resolved": false}, nil
	}
	detail := text(stored, "detail")
	if detail == "" {
		return nil, fmt.Errorf("this experience recorded no detail to compare with: send an outcome")
	}

	matched := strings.EqualFold(detail, observed)
	outcome = OutcomeReplaced
	if matched {
		outcome = OutcomeAccepted
	}
	answer, err := d.service.Dispatch(ctx, "memory.resolve", map[string]any{"ref": ref, "outcome": outcome})
	if err != nil {
		return nil, err
	}
	if answer == nil {
		answer = map[string]any{}
	}
	answer["matched"] = matched
	return answer, nil
}

func (d *Domain) predict(ctx context.Context, payload map[string]any) (map[string]any, error) {
	experienceContext, err := requiredContext(payload)
	if err != nil {
		return nil, err
	}
	action, err := requiredAction(payload)
	if err != nil {
		return nil, err
	}

	recall := map[string]any{"context": experienceContext, "action": action, "k": predictNeighbors}
	// Asking for snippets lets the evidence say what each earlier experience was about. Only the
	// caller who needs to show it asks.
	if include, _ := payload["include_context"].(bool); include {
		recall["include_context"] = true
	}
	recalled, err := d.service.Dispatch(ctx, "memory.recall", recall)
	if err != nil {
		return nil, err
	}
	prediction := Predict(action, neighborsFrom(recalled), outcomeCounts(recalled), d.config)

	evidence := make([]map[string]any, 0, len(prediction.Evidence))
	for _, neighbor := range prediction.Evidence {
		entry := map[string]any{"ref": neighbor.Ref, "similarity": neighbor.Similarity, "outcome": neighbor.Outcome}
		if neighbor.Detail != "" {
			entry["detail"] = neighbor.Detail
		}
		if neighbor.Snippet != "" {
			entry["context_snippet"] = neighbor.Snippet
		}
		evidence = append(evidence, entry)
	}
	degraded, _ := recalled["degraded"].(bool)
	return map[string]any{
		"action":       action,
		"predicted_p":  prediction.P,
		"baseline_p":   prediction.Baseline,
		"support":      prediction.Support,
		"resolved":     prediction.Resolved,
		"insufficient": prediction.Insufficient,
		"reason":       prediction.Reason,
		"degraded":     degraded,
		"evidence":     evidence,
	}, nil
}

func (d *Domain) score(ctx context.Context, payload map[string]any) (map[string]any, error) {
	request := map[string]any{"limit": scoreWindow}
	action := text(payload, "action")
	if action != "" {
		if !actionName.MatchString(action) {
			return nil, fmt.Errorf("action must be a short lowercase name such as hashtag.suggest")
		}
		request["action"] = action
	}

	answer, err := d.service.Dispatch(ctx, "memory.resolved", request)
	if err != nil {
		return nil, err
	}
	card := Score(resolvedFrom(answer), d.config.MinScored)
	return map[string]any{
		"action":           action,
		"scored":           card.Scored,
		"unscored":         card.Unscored,
		"brier_prediction": card.BrierPrediction,
		"brier_baseline":   card.BrierBaseline,
		"skill":            card.Skill,
		"beats_baseline":   card.BeatsBaseline,
		"enough_data":      card.EnoughData,
		"min_scored":       d.config.MinScored,
	}, nil
}

// ---- requests --------------------------------------------------------------------------

func buildRecord(payload map[string]any) (map[string]any, error) {
	ref, err := requiredRef(payload)
	if err != nil {
		return nil, err
	}
	action, err := requiredAction(payload)
	if err != nil {
		return nil, err
	}
	experienceContext, err := requiredContext(payload)
	if err != nil {
		return nil, err
	}
	clean := map[string]any{"ref": ref, "action": action, "context": experienceContext}
	if detail := text(payload, "detail"); detail != "" {
		if utf8.RuneCountInString(detail) > maxDetailRunes || strings.IndexFunc(detail, unicode.IsControl) >= 0 {
			return nil, fmt.Errorf("detail must be at most %d characters and have no control characters", maxDetailRunes)
		}
		clean["detail"] = detail
	}

	predicted, hasPredicted := number(payload, "predicted_p")
	baseline, hasBaseline := number(payload, "baseline_p")
	if hasPredicted != hasBaseline {
		return nil, fmt.Errorf("predicted_p and baseline_p go together: a prediction is only scored against its baseline")
	}
	if hasPredicted {
		if !isProbability(predicted) || !isProbability(baseline) {
			return nil, fmt.Errorf("predicted_p and baseline_p must be between 0 and 1")
		}
		clean["predicted_p"] = predicted
		clean["baseline_p"] = baseline
	}

	if matchedScore, ok := number(payload, "matched_score"); ok {
		if !isProbability(matchedScore) {
			return nil, fmt.Errorf("matched_score must be between 0 and 1")
		}
		clean["matched_score"] = matchedScore
	}
	if matchedContext := text(payload, "matched_context"); matchedContext != "" {
		if utf8.RuneCountInString(matchedContext) > maxMatchedRunes {
			return nil, fmt.Errorf("matched_context must be at most %d characters", maxMatchedRunes)
		}
		clean["matched_context"] = matchedContext
	}
	return clean, nil
}

func buildGet(payload map[string]any) (map[string]any, error) {
	ref, err := requiredRef(payload)
	if err != nil {
		return nil, err
	}
	return map[string]any{"ref": ref}, nil
}

func buildRecall(payload map[string]any) (map[string]any, error) {
	experienceContext, err := requiredContext(payload)
	if err != nil {
		return nil, err
	}
	clean := map[string]any{"context": experienceContext, "k": defaultRecallK}
	if action := text(payload, "action"); action != "" {
		if !actionName.MatchString(action) {
			return nil, fmt.Errorf("action must be a short lowercase name such as hashtag.suggest")
		}
		clean["action"] = action
	}
	if k, ok := number(payload, "k"); ok {
		if k < 1 || k > maxRecallK {
			return nil, fmt.Errorf("k must be between 1 and %d", maxRecallK)
		}
		clean["k"] = int(k)
	}
	if include, _ := payload["include_context"].(bool); include {
		clean["include_context"] = true
	}
	return clean, nil
}

func buildList(payload map[string]any) (map[string]any, error) {
	clean := map[string]any{"limit": defaultListLimit, "offset": 0}
	if action := text(payload, "action"); action != "" {
		if !actionName.MatchString(action) {
			return nil, fmt.Errorf("action must be a short lowercase name such as hashtag.suggest")
		}
		clean["action"] = action
	}
	switch state := text(payload, "state"); state {
	case "":
	case "all", "pending", "resolved":
		clean["state"] = state
	default:
		return nil, fmt.Errorf("state must be all, pending or resolved")
	}
	if limit, ok := number(payload, "limit"); ok {
		if limit < 1 || limit > maxListLimit {
			return nil, fmt.Errorf("limit must be between 1 and %d", maxListLimit)
		}
		clean["limit"] = int(limit)
	}
	if offset, ok := number(payload, "offset"); ok {
		if offset < 0 || offset > maxListOffset {
			return nil, fmt.Errorf("offset must be between 0 and %d", maxListOffset)
		}
		clean["offset"] = int(offset)
	}
	if include, _ := payload["include_context"].(bool); include {
		clean["include_context"] = true
	}
	return clean, nil
}

func buildForget(payload map[string]any) (map[string]any, error) {
	ref, err := requiredRef(payload)
	if err != nil {
		return nil, err
	}
	return map[string]any{"ref": ref}, nil
}

func buildReindex(payload map[string]any) (map[string]any, error) {
	clean := map[string]any{}
	if limit, ok := number(payload, "limit"); ok {
		if limit < 1 || limit > maxReindexBatch {
			return nil, fmt.Errorf("limit must be between 1 and %d", maxReindexBatch)
		}
		clean["limit"] = int(limit)
	}
	return clean, nil
}

func requiredRef(payload map[string]any) (string, error) {
	ref := text(payload, "ref")
	if ref == "" {
		return "", fmt.Errorf("ref is required")
	}
	if len(ref) > maxRefLen || strings.IndexFunc(ref, unicode.IsControl) >= 0 {
		return "", fmt.Errorf("ref must be at most %d bytes and have no control characters", maxRefLen)
	}
	return ref, nil
}

func requiredAction(payload map[string]any) (string, error) {
	action := text(payload, "action")
	if action == "" {
		return "", fmt.Errorf("action is required")
	}
	if !actionName.MatchString(action) {
		return "", fmt.Errorf("action must be a short lowercase name such as hashtag.suggest")
	}
	return action, nil
}

func requiredContext(payload map[string]any) (string, error) {
	experienceContext := text(payload, "context")
	if experienceContext == "" {
		return "", fmt.Errorf("context is required")
	}
	if utf8.RuneCountInString(experienceContext) > maxContextRunes {
		return "", fmt.Errorf("context is longer than %d characters; send the part that matters", maxContextRunes)
	}
	return experienceContext, nil
}

func text(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return strings.TrimSpace(value)
}

// number reads a JSON number. Over the wire it is a float64; callers in Go may send an int.
func number(payload map[string]any, key string) (float64, bool) {
	switch value := payload[key].(type) {
	case float64:
		return value, true
	case int:
		return float64(value), true
	case int64:
		return float64(value), true
	}
	return 0, false
}

func isProbability(value float64) bool {
	return value >= 0 && value <= 1
}
