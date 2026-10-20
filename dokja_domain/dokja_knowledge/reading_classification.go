package knowledge

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ReadingProposal is one document's classifier output (document.classify.v1), as the
// reading host checked its shape. It is untrusted until resolved here.
type ReadingProposal struct {
	Form                 Form
	ContentKind          ContentKind
	UncertainForm        bool
	UncertainContentKind bool
	Topics               []string
	Entities             []Entity
	DateAnchor           string
}

// ClassificationCandidates names the stored documents of a reading that a classifier may
// see (ADR 0008). Only content stored just now qualifies: an unchanged reread keeps what was
// decided before and never asks again. A document is skipped when nothing is left to
// propose for its axes. Every form may be sent, notes and conversations included: enabling
// a classifier is the owner's permission to send it what was read (ADR 0008).
func ClassificationCandidates(stored []map[string]any) []int {
	var candidates []int
	for i, result := range stored {
		if changed, _ := result["changed"].(bool); !changed {
			continue
		}
		metadata, _ := result["metadata"].(map[string]any)
		if metadata == nil {
			continue
		}
		if awaitingClassifier(metadata, "form_choice") || awaitingClassifier(metadata, "kind_choice") {
			candidates = append(candidates, i)
		}
	}
	return candidates
}

// awaitingClassifier is an axis nobody has decided: not the user, not the source, and not
// a question already put to the user (only the user answers that one).
func awaitingClassifier(metadata map[string]any, choiceField string) bool {
	choice, _ := metadata[choiceField].(map[string]any)
	origin, _ := choice["origin"].(string)
	reason, _ := choice["fallback_reason"].(string)
	return ClassificationOrigin(origin) == OriginUserRequired && reason == "classification_missing"
}

// ApplyClassification resolves each candidate's proposal and patches its stored metadata,
// bound to the revision just read, so an answer the user gives meanwhile is never
// overwritten. Content and embeddings are untouched. stored is updated in place with the
// effective metadata; documents that could not be patched keep their pending questions and
// the first error is returned after every document was tried.
func (d *Domain) ApplyClassification(ctx context.Context, stored []map[string]any, candidates []int, proposals []ReadingProposal, classifierID, classifierVersion string) error {
	if d == nil || d.service == nil {
		return fmt.Errorf("knowledge service is not configured")
	}
	if len(candidates) != len(proposals) {
		return fmt.Errorf("%d proposals for %d documents", len(proposals), len(candidates))
	}
	classifierID, classifierVersion = strings.TrimSpace(classifierID), strings.TrimSpace(classifierVersion)
	if classifierID == "" || classifierVersion == "" {
		return fmt.Errorf("classifier identity and version are required")
	}
	var errs []error
	for n, i := range candidates {
		if i < 0 || i >= len(stored) {
			return fmt.Errorf("candidate %d is not a stored document", i)
		}
		sourceID, _ := stored[i]["source_id"].(string)
		snapshot, err := d.service.Dispatch(ctx, "knowledge.metadata.get", map[string]any{"source_id": sourceID})
		if err != nil {
			errs = append(errs, err)
			continue
		}
		metadata, _ := snapshot["metadata"].(map[string]any)
		patch := classifierPatch(metadata, proposals[n], classifierID, classifierVersion)
		result, err := d.service.Dispatch(ctx, "knowledge.metadata.patch", map[string]any{
			"source_id": sourceID, "revision": snapshot["revision"], "patch": patch,
		})
		if err != nil {
			errs = append(errs, err)
			continue
		}
		stored[i]["metadata"] = result["metadata"]
		stored[i]["classified"] = true
		annotateClassification(stored[i])
	}
	return errors.Join(errs...)
}

// classifierPatch fills only what nobody decided. An axis the classifier is unsure of, or
// names with a label Knowledge does not have, becomes a question for the user. Topics,
// entities and the date anchor come from the classifier only when the request gave none.
func classifierPatch(metadata map[string]any, proposal ReadingProposal, id, version string) map[string]any {
	resolved, _ := ResolveClassification(ClassificationInput{Classifier: &ClassifierProposal{
		Values: ClassificationValues{Form: proposal.Form, ContentKind: proposal.ContentKind},
		ID:     id, Version: version,
		UncertainForm: proposal.UncertainForm, UncertainContentKind: proposal.UncertainContentKind,
	}})
	patch := map[string]any{"classifier": map[string]any{"id": id, "version": version}}
	if awaitingClassifier(metadata, "form_choice") {
		patch["form"], patch["form_choice"] = axisValue(string(resolved.Form)), choiceMap(resolved.FormChoice)
	}
	if awaitingClassifier(metadata, "kind_choice") {
		patch["content_kind"], patch["kind_choice"] = axisValue(string(resolved.ContentKind)), choiceMap(resolved.KindChoice)
	}
	if _, given := metadata["topics"]; !given {
		var topics []string
		for _, topic := range proposal.Topics {
			if topic = strings.TrimSpace(topic); topic != "" {
				topics = append(topics, topic)
			}
		}
		if len(topics) > 0 {
			patch["topics"] = topics
		}
	}
	if _, given := metadata["entities"]; !given {
		var entities []map[string]any
		for _, entity := range proposal.Entities {
			if name := strings.TrimSpace(entity.Name); name != "" {
				entry := map[string]any{"name": name}
				if kind := strings.TrimSpace(entity.Type); kind != "" {
					entry["type"] = kind
				}
				entities = append(entities, entry)
			}
		}
		if len(entities) > 0 {
			patch["entities"] = entities
		}
	}
	if _, given := metadata["date_anchor"]; !given && proposal.DateAnchor != "" {
		if _, err := time.Parse("2006-01-02", proposal.DateAnchor); err == nil {
			patch["date_anchor"] = proposal.DateAnchor
		}
	}
	return patch
}

func axisValue(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func choiceMap(choice ClassificationChoice) map[string]any {
	result := map[string]any{"origin": string(choice.Origin)}
	if choice.ClassifierID != "" {
		result["classifier_id"], result["classifier_version"] = choice.ClassifierID, choice.ClassifierVersion
	}
	if choice.FallbackReason != "" {
		result["fallback_reason"] = choice.FallbackReason
	}
	return result
}
