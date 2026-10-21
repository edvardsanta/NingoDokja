package knowledge

import (
	"context"
	"fmt"
	"strings"
)

const ActionReclassifyKnowledge = "reclassify-knowledge"

// Reclassification holds the exact content/metadata revision sent to a provider.
// It is kept outside the task workspace because the text is private and bounded
// by the storage service, rather than part of the orchestration trace.
type Reclassification struct {
	snapshot map[string]any
}

func (r *Reclassification) Document() map[string]any {
	return map[string]any{"title": r.snapshot["title"], "text": r.snapshot["text"]}
}

// reclassificationMetadata makes only previously automatic decisions eligible
// again. User answers, source defaults, and questions awaiting the user stay put.
func reclassificationMetadata(metadata map[string]any) (map[string]any, bool) {
	copy := make(map[string]any, len(metadata))
	for key, value := range metadata {
		copy[key] = value
	}
	eligible := false
	for _, axis := range []struct{ field, value string }{{"form_choice", "form"}, {"kind_choice", "content_kind"}} {
		field := axis.field
		choice, _ := metadata[field].(map[string]any)
		origin, _ := choice["origin"].(string)
		if origin == string(OriginClassifier) || awaitingClassifier(metadata, field) || (len(choice) == 0 && metadata[axis.value] == nil) {
			copy[field] = map[string]any{"origin": string(OriginUserRequired), "fallback_reason": "classification_missing"}
			eligible = true
		}
	}
	return copy, eligible
}

func (d *Domain) PrepareReclassification(ctx context.Context, payload map[string]any) (*Reclassification, error) {
	id, _ := payload["source_id"].(string)
	if strings.TrimSpace(id) == "" || len(payload) != 1 {
		return nil, fmt.Errorf("reclassification requires only source_id")
	}
	snapshot, err := d.service.Dispatch(ctx, "knowledge.classification.sample", map[string]any{"source_id": strings.TrimSpace(id)})
	if err != nil {
		return nil, err
	}
	text, _ := snapshot["text"].(string)
	revision, _ := snapshot["revision"].(string)
	if strings.TrimSpace(text) == "" || len([]rune(text)) > 16000 || revision == "" {
		return nil, fmt.Errorf("invalid classification snapshot")
	}
	return &Reclassification{snapshot: snapshot}, nil
}

func (r *Reclassification) Eligible() bool {
	metadata, _ := r.snapshot["metadata"].(map[string]any)
	_, eligible := reclassificationMetadata(metadata)
	return eligible
}

// KeepReclassification never refreshes the revision after inference. Any content
// edit, deletion or user answer during inference must reject the stale proposal.
func (d *Domain) KeepReclassification(ctx context.Context, r *Reclassification, proposal ReadingProposal, id, version string) (map[string]any, error) {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(version) == "" {
		return nil, fmt.Errorf("classifier identity and version are required")
	}
	metadata, _ := r.snapshot["metadata"].(map[string]any)
	eligible, ok := reclassificationMetadata(metadata)
	if !ok {
		return nil, fmt.Errorf("no automatic classification to revise")
	}
	result, err := d.service.Dispatch(ctx, "knowledge.metadata.patch", map[string]any{
		"source_id": r.snapshot["source_id"], "revision": r.snapshot["revision"],
		"patch": classifierPatch(eligible, proposal, id, version),
	})
	if err == nil {
		annotateClassification(result)
	}
	return result, err
}
