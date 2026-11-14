package knowledge

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Origin says where a stored knowledge record came from. Owner records outrank
// approved group records, which outrank values a source API returned; the rule
// matters per item, when more than one source claims it.
type Origin string

const (
	OriginOwner Origin = "owner"
	OriginGroup Origin = "group"
	OriginAPI   Origin = "api"
)

func (o Origin) Valid() bool {
	switch o {
	case OriginOwner, OriginGroup, OriginAPI:
		return true
	}
	return false
}

// Rank orders origins for one item: the owner's record wins over an approved
// group record, which wins over what a source API returned.
func (o Origin) Rank() int {
	switch o {
	case OriginOwner:
		return 3
	case OriginGroup:
		return 2
	case OriginAPI:
		return 1
	}
	return 0
}

// Provenance is assigned by Knowledge when a record is stored; user metadata
// never carries it. It keeps an approved submission apart from the owner's own
// records, so precedence and audits can tell them apart.
type Provenance struct {
	Origin     Origin `json:"origin"`
	ProposedBy string `json:"proposed_by,omitempty"`
	SentBy     string `json:"sent_by,omitempty"`
	Channel    string `json:"proposed_channel,omitempty"`
	Permalink  string `json:"proposed_permalink,omitempty"`
	ProposedAt string `json:"proposed_at,omitempty"`
}

const provenanceFieldLimit = 240

func (p Provenance) Validate() error {
	if !p.Origin.Valid() {
		return fmt.Errorf("provenance origin must be one of owner, group, api")
	}
	for _, value := range []string{p.ProposedBy, p.SentBy, p.Channel, p.Permalink, p.ProposedAt} {
		if len(value) > provenanceFieldLimit {
			return fmt.Errorf("provenance values are bounded at %d characters", provenanceFieldLimit)
		}
	}
	return nil
}

// MetadataOrigin reads where a stored record came from. Records stored before
// provenance existed carry none and read as the owner's own.
func MetadataOrigin(metadata map[string]any) Origin {
	raw, _ := metadata["provenance"].(map[string]any)
	origin, _ := raw["origin"].(string)
	if o := Origin(origin); o.Valid() {
		return o
	}
	return OriginOwner
}

// WithProvenance assigns where a stored record came from onto metadata Knowledge
// already resolved. It refuses a second assignment: origin is decided once.
func WithProvenance(metadata map[string]any, provenance Provenance) (map[string]any, error) {
	if metadata == nil {
		metadata = map[string]any{}
	}
	if err := provenance.Validate(); err != nil {
		return nil, err
	}
	if _, exists := metadata["provenance"]; exists {
		return nil, fmt.Errorf("provenance is already assigned")
	}
	assigned := make(map[string]any, len(metadata)+1)
	for key, value := range metadata {
		assigned[key] = value
	}
	assigned["provenance"] = provenance
	return assigned, nil
}

// ProposalSource is where the proposed material was sent from.
type ProposalSource struct {
	ChannelID string `json:"channel_id,omitempty"`
	MessageID string `json:"message_id,omitempty"`
	Permalink string `json:"permalink,omitempty"`
}

// Proposal is a pending claim about something for Ningo to learn. It exists only
// while pending: approving turns it into a stored record carrying its provenance
// and removes it; rejecting deletes it, keeping nothing.
type Proposal struct {
	ID         string
	Kind       string
	Payload    map[string]any
	ProposedBy string
	SentBy     string
	Source     ProposalSource
	ProposedAt time.Time
}

const maxProposalPayloadBytes = 64 * 1024

// ProposalKindRules are what a kind of proposal supplies: how its payload is
// validated and canonicalized, and the stable identity of the item it proposes,
// used for duplicates and for looking up the record a decision would affect.
type ProposalKindRules struct {
	Kind     string
	Validate func(map[string]any) (map[string]any, error)
	Identity func(map[string]any) string
}

var proposalKinds = map[string]ProposalKindRules{}

// RegisterProposalKind adds a kind of proposal. It runs once at startup wiring;
// a duplicate or incomplete registration is a programming error, not a request.
func RegisterProposalKind(rules ProposalKindRules) error {
	if rules.Kind == "" || rules.Validate == nil || rules.Identity == nil {
		return fmt.Errorf("proposal kind %q needs a kind, a validate func and an identity func", rules.Kind)
	}
	if _, exists := proposalKinds[rules.Kind]; exists {
		return fmt.Errorf("proposal kind %q is already registered", rules.Kind)
	}
	proposalKinds[rules.Kind] = rules
	return nil
}

// ProposalKindRulesFor returns the rules a kind registered, if any.
func ProposalKindRulesFor(kind string) (ProposalKindRules, bool) {
	rules, ok := proposalKinds[kind]
	return rules, ok
}

// ValidateProposal applies the shared requirements and the kind's own rules.
// The returned proposal carries the canonical payload and a fresh stable id.
func ValidateProposal(kind string, payload map[string]any, proposedBy, sentBy string) (Proposal, error) {
	rules, ok := ProposalKindRulesFor(kind)
	if !ok {
		return Proposal{}, fmt.Errorf("unknown proposal kind %q", kind)
	}
	proposedBy = strings.TrimSpace(proposedBy)
	if proposedBy == "" {
		return Proposal{}, fmt.Errorf("a proposal needs a proposer")
	}
	canonical, err := rules.Validate(payload)
	if err != nil {
		return Proposal{}, err
	}
	if canonical == nil {
		canonical = map[string]any{}
	}
	encoded, _ := json.Marshal(canonical)
	if len(encoded) > maxProposalPayloadBytes {
		return Proposal{}, fmt.Errorf("proposal payload exceeds %d bytes", maxProposalPayloadBytes)
	}
	identity := rules.Identity(canonical)
	if identity == "" {
		return Proposal{}, fmt.Errorf("proposal kind %q returned no item identity", kind)
	}
	return Proposal{
		ID:         "proposal:" + kind + ":" + identity,
		Kind:       kind,
		Payload:    canonical,
		ProposedBy: proposedBy,
		SentBy:     strings.TrimSpace(sentBy),
		ProposedAt: time.Now().UTC(),
	}, nil
}

// PendingDuplicate finds the pending proposal of the same kind for the same
// item identity, so a repeated submission reopens the same question instead of
// stacking copies.
func PendingDuplicate(pending []Proposal, kind, identity string) (Proposal, bool) {
	for _, proposal := range pending {
		if proposal.Kind != kind {
			continue
		}
		rules, ok := ProposalKindRulesFor(kind)
		if !ok {
			continue
		}
		if rules.Identity(proposal.Payload) == identity {
			return proposal, true
		}
	}
	return Proposal{}, false
}
