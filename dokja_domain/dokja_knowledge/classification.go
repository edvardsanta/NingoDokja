package knowledge

import (
	"fmt"
	"strings"
)

// Form describes a document's structure, independently of its subject matter.
type Form string

const (
	FormGeneral      Form = "general"
	FormBook         Form = "book"
	FormArticle      Form = "article"
	FormNote         Form = "note"
	FormConversation Form = "conversation"
	FormMeme         Form = "meme"
)

// ContentKind describes how knowledge is discussed, independently of its source format.
type ContentKind string

const (
	ContentGeneral   ContentKind = "general"
	ContentNews      ContentKind = "news"
	ContentReference ContentKind = "reference"
	ContentResearch  ContentKind = "research"
	ContentNarrative ContentKind = "narrative"
)

type ClassificationOrigin string

const (
	OriginUser       ClassificationOrigin = "user"
	OriginClassifier ClassificationOrigin = "classifier"
	OriginSource     ClassificationOrigin = "source_default"
	OriginGeneral    ClassificationOrigin = "general"
)

// ClassificationValues may name either axis or both. Empty values make no claim.
// File formats such as pdf and epub are source provenance, not classification values.
type ClassificationValues struct {
	Form        Form
	ContentKind ContentKind
}

// ClassifierProposal is untrusted output from a provider. Its identity is required
// before either axis can be attributed to it. Validation happens per axis, so an
// invalid content kind does not discard a valid form (or vice versa).
type ClassifierProposal struct {
	Values  ClassificationValues
	ID      string
	Version string
}

type ClassificationInput struct {
	Explicit      ClassificationValues
	Classifier    *ClassifierProposal
	SourceDefault ClassificationValues
}

// ClassificationChoice keeps provenance separate from both the selected value and
// free-form tags. FallbackReason is a fixed code, never raw provider output.
type ClassificationChoice struct {
	Origin            ClassificationOrigin `json:"origin"`
	ClassifierID      string               `json:"classifier_id,omitempty"`
	ClassifierVersion string               `json:"classifier_version,omitempty"`
	FallbackReason    string               `json:"fallback_reason,omitempty"`
}

// Classification is the resolved structured contract. Each axis has its own
// provenance: a user-selected form must not
// accidentally make a classifier-selected content kind look user supplied.
type Classification struct {
	Form        Form                 `json:"form"`
	ContentKind ContentKind          `json:"content_kind"`
	FormChoice  ClassificationChoice `json:"form_choice"`
	KindChoice  ClassificationChoice `json:"kind_choice"`
}

// ResolveClassification applies user > classifier > source default > general per
// axis. Invalid explicit values and source configuration are errors; invalid provider
// values fall back. It does not call a model, infer labels from text, or write storage.
func ResolveClassification(input ClassificationInput) (Classification, error) {
	form, err := resolveAxis(string(input.Explicit.Form), string(input.SourceDefault.Form),
		proposalAxis(input.Classifier, true), input.Classifier, validForm, "form")
	if err != nil {
		return Classification{}, err
	}
	kind, err := resolveAxis(string(input.Explicit.ContentKind), string(input.SourceDefault.ContentKind),
		proposalAxis(input.Classifier, false), input.Classifier, validContentKind, "content kind")
	if err != nil {
		return Classification{}, err
	}
	return Classification{
		Form: Form(form.value), ContentKind: ContentKind(kind.value),
		FormChoice: form.choice, KindChoice: kind.choice,
	}, nil
}

type axisResolution struct {
	value  string
	choice ClassificationChoice
}

func resolveAxis(explicit, source, proposed string, classifier *ClassifierProposal, valid func(string) bool, axis string) (axisResolution, error) {
	explicit, source, proposed = normalizeLabel(explicit), normalizeLabel(source), normalizeLabel(proposed)
	if explicit != "" {
		if !valid(explicit) {
			return axisResolution{}, fmt.Errorf("unsupported explicit %s", axis)
		}
		return axisResolution{value: explicit, choice: ClassificationChoice{Origin: OriginUser}}, nil
	}

	reason := ""
	if proposed != "" {
		switch {
		case !valid(proposed):
			reason = "classifier_value_invalid"
		case classifier == nil || strings.TrimSpace(classifier.ID) == "" || strings.TrimSpace(classifier.Version) == "":
			reason = "classifier_identity_missing"
		default:
			return axisResolution{value: proposed, choice: ClassificationChoice{
				Origin: OriginClassifier, ClassifierID: strings.TrimSpace(classifier.ID),
				ClassifierVersion: strings.TrimSpace(classifier.Version),
			}}, nil
		}
	}
	if source != "" {
		if !valid(source) {
			return axisResolution{}, fmt.Errorf("unsupported source-default %s", axis)
		}
		return axisResolution{value: source, choice: ClassificationChoice{Origin: OriginSource, FallbackReason: reason}}, nil
	}
	return axisResolution{value: "general", choice: ClassificationChoice{Origin: OriginGeneral, FallbackReason: reason}}, nil
}

func normalizeLabel(value string) string { return strings.ToLower(strings.TrimSpace(value)) }

func proposalAxis(proposal *ClassifierProposal, form bool) string {
	if proposal == nil {
		return ""
	}
	if form {
		return string(proposal.Values.Form)
	}
	return string(proposal.Values.ContentKind)
}

func validForm(value string) bool {
	switch Form(value) {
	case FormGeneral, FormBook, FormArticle, FormNote, FormConversation, FormMeme:
		return true
	}
	return false
}

func validContentKind(value string) bool {
	switch ContentKind(value) {
	case ContentGeneral, ContentNews, ContentReference, ContentResearch, ContentNarrative:
		return true
	}
	return false
}
