package memory

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxRefLen       = 512
	maxContextRunes = 4000
	maxDetailRunes  = 200
	defaultK        = 10
	maxK            = 50
	defaultReindex  = 32
	maxReindex      = 500
	embedBatch      = 32
	defaultResolved = 5000
	maxResolved     = 20000
)

var (
	actionName  = regexp.MustCompile(`^[a-z][a-z0-9_.:-]{0,63}$`)
	outcomeName = regexp.MustCompile(`^[a-z][a-z_]{0,31}$`)
)

// Service answers the memory.* events. Validation here is structural only (sizes and shapes):
// which outcomes exist and what they mean is the domain's business.
type Service struct {
	store    *Store
	embedder Embedder
	model    string
}

// NewService builds the service. embedder may be nil, which switches similarity off.
func NewService(store *Store, embedder Embedder, model string) *Service {
	return &Service{store: store, embedder: embedder, model: model}
}

func (s *Service) Dispatch(ctx context.Context, eventType string, payload map[string]any) (map[string]any, error) {
	switch eventType {
	case "memory.record":
		return s.record(ctx, payload)
	case "memory.resolve":
		return s.resolve(payload)
	case "memory.get":
		return s.get(payload)
	case "memory.recall":
		return s.recall(ctx, payload)
	case "memory.resolved":
		return s.resolved(payload)
	case "memory.forget":
		return s.forget(payload)
	case "memory.status":
		return s.status(ctx)
	case "memory.reindex":
		return s.reindex(ctx, payload)
	}
	return nil, fmt.Errorf("unsupported memory event type: %s", eventType)
}

func (s *Service) record(ctx context.Context, payload map[string]any) (map[string]any, error) {
	experience, err := parseNew(payload)
	if err != nil {
		return nil, err
	}
	exists, err := s.store.Exists(experience.Ref)
	if err != nil {
		return nil, err
	}
	if exists {
		// Immutable: nothing to embed or store, and the earlier prediction stays as it was.
		return map[string]any{"created": false, "embedded": false, "degraded": false}, nil
	}

	vector, reason := s.embedOne(ctx, experience.Context)
	created, err := s.store.Insert(experience, vector, s.model)
	if err != nil {
		return nil, err
	}
	result := map[string]any{"created": created, "embedded": vector != nil, "degraded": vector == nil}
	if reason != "" {
		result["reason"] = reason
	}
	return result, nil
}

func (s *Service) resolve(payload map[string]any) (map[string]any, error) {
	ref, err := requiredRef(payload)
	if err != nil {
		return nil, err
	}
	outcome := text(payload, "outcome")
	if !outcomeName.MatchString(outcome) {
		return nil, fmt.Errorf("outcome must be a short lowercase word such as accepted")
	}
	found, resolved, current, err := s.store.Resolve(ref, outcome)
	if err != nil {
		return nil, err
	}
	return map[string]any{"found": found, "resolved": resolved, "outcome": current}, nil
}

func (s *Service) get(payload map[string]any) (map[string]any, error) {
	ref, err := requiredRef(payload)
	if err != nil {
		return nil, err
	}
	stored, ok, err := s.store.Get(ref)
	if err != nil {
		return nil, err
	}
	if !ok {
		return map[string]any{"found": false}, nil
	}
	return map[string]any{
		"found": true, "ref": stored.Ref, "action": stored.Action, "detail": stored.Detail, "outcome": stored.Outcome,
		"predicted_p": nullable(stored.Predicted), "baseline_p": nullable(stored.Baseline),
		"created_at": stored.CreatedAt, "resolved_at": stored.ResolvedAt,
	}, nil
}

func (s *Service) recall(ctx context.Context, payload map[string]any) (map[string]any, error) {
	query := text(payload, "context")
	if query == "" {
		return nil, fmt.Errorf("context is required")
	}
	if utf8.RuneCountInString(query) > maxContextRunes {
		return nil, fmt.Errorf("context is longer than %d characters", maxContextRunes)
	}
	action, err := optionalAction(payload)
	if err != nil {
		return nil, err
	}
	k, err := boundedInt(payload, "k", defaultK, 1, maxK)
	if err != nil {
		return nil, err
	}

	counts, err := s.store.OutcomeCounts(action)
	if err != nil {
		return nil, err
	}
	outcomes := make(map[string]any, len(counts))
	for outcome, count := range counts {
		outcomes[outcome] = count
	}
	result := map[string]any{"neighbors": []any{}, "outcomes": outcomes, "degraded": false}

	vector, reason := s.embedOne(ctx, query)
	if vector == nil {
		// The counts still give a baseline; only the similarity is missing.
		result["degraded"], result["reason"] = true, reason
		return result, nil
	}
	nearest, err := s.store.Nearest(vector, action, s.model, k)
	if err != nil {
		return nil, err
	}
	neighbors := make([]any, 0, len(nearest))
	for _, n := range nearest {
		neighbors = append(neighbors, map[string]any{
			"ref": n.Ref, "action": n.Action, "outcome": n.Outcome,
			"similarity": n.Similarity, "created_at": n.CreatedAt,
		})
	}
	result["neighbors"] = neighbors
	return result, nil
}

func (s *Service) resolved(payload map[string]any) (map[string]any, error) {
	action, err := optionalAction(payload)
	if err != nil {
		return nil, err
	}
	limit, err := boundedInt(payload, "limit", defaultResolved, 1, maxResolved)
	if err != nil {
		return nil, err
	}
	rows, err := s.store.Resolved(action, limit)
	if err != nil {
		return nil, err
	}
	experiences := make([]any, 0, len(rows))
	for _, row := range rows {
		experiences = append(experiences, map[string]any{
			"ref": row.Ref, "action": row.Action, "outcome": row.Outcome,
			"predicted_p": nullable(row.Predicted), "baseline_p": nullable(row.Baseline),
			"resolved_at": row.ResolvedAt,
		})
	}
	return map[string]any{"experiences": experiences}, nil
}

func (s *Service) forget(payload map[string]any) (map[string]any, error) {
	ref, err := requiredRef(payload)
	if err != nil {
		return nil, err
	}
	deleted, err := s.store.Forget(ref)
	if err != nil {
		return nil, err
	}
	return map[string]any{"deleted": deleted}, nil
}

func (s *Service) status(ctx context.Context) (map[string]any, error) {
	stats, err := s.store.Stats(s.model)
	if err != nil {
		return nil, err
	}
	reachable := s.embedder != nil && s.embedder.Reachable(ctx)
	return map[string]any{
		"experiences":        stats.Experiences,
		"pending":            stats.Pending,
		"resolved":           stats.Resolved,
		"expired":            stats.Expired,
		"embedded":           stats.Embedded,
		"needs_reindex":      stats.NeedsReindex,
		"embed_model":        s.model,
		"embeddings":         s.embedder != nil,
		"embedder_reachable": reachable,
		"degraded":           !reachable,
	}, nil
}

// reindex embeds experiences that have no vector (recorded while the embedder was down) or
// one from another model, a bounded batch at a time so one call never holds the service long.
func (s *Service) reindex(ctx context.Context, payload map[string]any) (map[string]any, error) {
	limit, err := boundedInt(payload, "limit", defaultReindex, 1, maxReindex)
	if err != nil {
		return nil, err
	}
	if s.embedder == nil {
		return nil, fmt.Errorf("embeddings are switched off (DOKJA_EMBED=off)")
	}
	pending, err := s.store.NeedingEmbedding(s.model, limit)
	if err != nil {
		return nil, err
	}

	embedded := 0
	for start := 0; start < len(pending); start += embedBatch {
		batch := pending[start:min(start+embedBatch, len(pending))]
		texts := make([]string, len(batch))
		ids := make([]int64, len(batch))
		for i, row := range batch {
			texts[i], ids[i] = row.Context, row.ID
		}
		vectors, err := s.embedder.Embed(ctx, texts)
		if err != nil {
			return nil, err
		}
		if err := s.store.SetEmbeddings(ids, vectors, s.model); err != nil {
			return nil, err
		}
		embedded += len(batch)
	}

	stats, err := s.store.Stats(s.model)
	if err != nil {
		return nil, err
	}
	return map[string]any{"embedded": embedded, "remaining": stats.NeedsReindex}, nil
}

// embedOne embeds one text. When it cannot, it returns no vector and why: a failure here
// degrades the service, it never fails the request.
func (s *Service) embedOne(ctx context.Context, content string) ([]float32, string) {
	if s.embedder == nil {
		return nil, "embeddings are switched off"
	}
	vectors, err := s.embedder.Embed(ctx, []string{content})
	if err != nil {
		if errors.Is(err, ErrEmbedUnavailable) {
			return nil, err.Error()
		}
		return nil, "embedding failed"
	}
	if len(vectors) != 1 {
		return nil, "embedding failed"
	}
	return vectors[0], ""
}

// ---- payloads -------------------------------------------------------------------------

func parseNew(payload map[string]any) (NewExperience, error) {
	ref, err := requiredRef(payload)
	if err != nil {
		return NewExperience{}, err
	}
	action := text(payload, "action")
	if !actionName.MatchString(action) {
		return NewExperience{}, fmt.Errorf("action must be a short lowercase name such as hashtag.suggest")
	}
	experienceContext := text(payload, "context")
	if experienceContext == "" {
		return NewExperience{}, fmt.Errorf("context is required")
	}
	if utf8.RuneCountInString(experienceContext) > maxContextRunes {
		return NewExperience{}, fmt.Errorf("context is longer than %d characters", maxContextRunes)
	}

	experience := NewExperience{Ref: ref, Action: action, Context: experienceContext, Detail: text(payload, "detail")}
	if utf8.RuneCountInString(experience.Detail) > maxDetailRunes || strings.IndexFunc(experience.Detail, unicode.IsControl) >= 0 {
		return NewExperience{}, fmt.Errorf("detail must be at most %d characters and have no control characters", maxDetailRunes)
	}
	predicted, hasPredicted := number(payload, "predicted_p")
	baseline, hasBaseline := number(payload, "baseline_p")
	if hasPredicted != hasBaseline {
		return NewExperience{}, fmt.Errorf("predicted_p and baseline_p go together")
	}
	if hasPredicted {
		if predicted < 0 || predicted > 1 || baseline < 0 || baseline > 1 {
			return NewExperience{}, fmt.Errorf("predicted_p and baseline_p must be between 0 and 1")
		}
		experience.Predicted, experience.Baseline = &predicted, &baseline
	}
	return experience, nil
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

func optionalAction(payload map[string]any) (string, error) {
	action := text(payload, "action")
	if action != "" && !actionName.MatchString(action) {
		return "", fmt.Errorf("action must be a short lowercase name such as hashtag.suggest")
	}
	return action, nil
}

func boundedInt(payload map[string]any, key string, fallback, lowest, highest int) (int, error) {
	value, ok := number(payload, key)
	if !ok {
		return fallback, nil
	}
	if value < float64(lowest) || value > float64(highest) {
		return 0, fmt.Errorf("%s must be between %d and %d", key, lowest, highest)
	}
	return int(value), nil
}

func text(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return strings.TrimSpace(value)
}

// number reads a JSON number. Over the wire it is a float64; Go callers may send an int.
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

func nullable(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}
