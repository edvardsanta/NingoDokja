package knowledge

import (
	"context"
	"strings"
	"testing"
)

func TestOriginRankOrdersOwnerOverGroupOverAPI(t *testing.T) {
	if !(OriginOwner.Rank() > OriginGroup.Rank() && OriginGroup.Rank() > OriginAPI.Rank()) {
		t.Fatalf("origin precedence broken: owner=%d group=%d api=%d", OriginOwner.Rank(), OriginGroup.Rank(), OriginAPI.Rank())
	}
	if Origin("bogus").Rank() != 0 || Origin("bogus").Valid() {
		t.Fatal("an unknown origin must rank zero and be invalid")
	}
}

func TestMetadataOriginReadsMissingProvenanceAsOwner(t *testing.T) {
	if got := MetadataOrigin(nil); got != OriginOwner {
		t.Fatalf("no metadata read as %q, want owner", got)
	}
	if got := MetadataOrigin(map[string]any{"form": "book"}); got != OriginOwner {
		t.Fatalf("a legacy record read as %q, want owner", got)
	}
	if got := MetadataOrigin(map[string]any{"provenance": map[string]any{"origin": "nonsense"}}); got != OriginOwner {
		t.Fatalf("a corrupt origin read as %q, want owner", got)
	}
	group := map[string]any{"provenance": map[string]any{"origin": "group", "proposed_by": "sam"}}
	if got := MetadataOrigin(group); got != OriginGroup {
		t.Fatalf("an approved group record read as %q, want group", got)
	}
}

func TestWithProvenanceAssignsOnce(t *testing.T) {
	assigned, err := WithProvenance(map[string]any{"form": "book"}, Provenance{Origin: OriginGroup, ProposedBy: "sam"})
	if err != nil {
		t.Fatal(err)
	}
	provenance, _ := assigned["provenance"].(Provenance)
	if provenance.Origin != OriginGroup || provenance.ProposedBy != "sam" {
		t.Fatalf("provenance was not assigned: %v", assigned["provenance"])
	}
	if _, err := WithProvenance(assigned, Provenance{Origin: OriginOwner}); err == nil {
		t.Fatal("a second provenance assignment was accepted")
	}
	if _, err := WithProvenance(nil, Provenance{Origin: Origin("bogus")}); err == nil {
		t.Fatal("an invalid origin was accepted")
	}
}

// proposalRulesFor tests is a registered kind like any real one supplies.
var testProposalOnce = func() bool {
	return RegisterProposalKind(ProposalKindRules{
		Kind: "test_item",
		Validate: func(payload map[string]any) (map[string]any, error) {
			name := strings.TrimSpace(stringOf(payload["name"]))
			if name == "" {
				return nil, errString("name is required")
			}
			return map[string]any{"name": name}, nil
		},
		Identity: func(payload map[string]any) string {
			return "test:" + stringOf(payload["name"])
		},
	}) == nil
}()

func stringOf(value any) string { s, _ := value.(string); return s }

type errString string

func (e errString) Error() string { return string(e) }

func TestValidateProposalAppliesKindAndSharedRules(t *testing.T) {
	if !testProposalOnce {
		t.Fatal("the test kind did not register")
	}
	if _, err := ValidateProposal("unknown_kind", map[string]any{"name": "x"}, "sam", ""); err == nil {
		t.Fatal("an unregistered kind was accepted")
	}
	if _, err := ValidateProposal("test_item", map[string]any{}, "sam", ""); err == nil {
		t.Fatal("an invalid payload was accepted")
	}
	if _, err := ValidateProposal("test_item", map[string]any{"name": "x"}, "  ", ""); err == nil {
		t.Fatal("a proposal without a proposer was accepted")
	}
	proposal, err := ValidateProposal("test_item", map[string]any{"name": "  spaced  "}, "sam", "tiago")
	if err != nil {
		t.Fatal(err)
	}
	if proposal.ID != "proposal:test_item:test:spaced" || proposal.ProposedBy != "sam" || proposal.SentBy != "tiago" {
		t.Fatalf("unexpected proposal: %+v", proposal)
	}
	if proposal.ProposedAt.IsZero() {
		t.Fatal("no proposal time")
	}
}

func TestPendingDuplicateMatchesKindAndIdentity(t *testing.T) {
	pending := []Proposal{
		{Kind: "test_item", Payload: map[string]any{"name": "alpha"}},
		{Kind: "test_item", Payload: map[string]any{"name": "beta"}},
	}
	if _, ok := PendingDuplicate(pending, "test_item", "test:alpha"); !ok {
		t.Fatal("the pending duplicate was not found")
	}
	if _, ok := PendingDuplicate(pending, "test_item", "test:gamma"); ok {
		t.Fatal("a non-duplicate matched")
	}
	if _, ok := PendingDuplicate(pending, "other_kind", "test:alpha"); ok {
		t.Fatal("a different kind matched")
	}
}

func TestStoreReadingCarriesProvenance(t *testing.T) {
	service := &ingestService{}
	provenance := Provenance{Origin: OriginGroup, ProposedBy: "sam", Channel: "c1"}
	reading := reading(map[string]any{}, ReadDocument{Title: "Memo", Text: "bounded text"})
	reading.Provenance = &provenance
	if _, err := New(service).StoreReading(context.Background(), reading); err != nil {
		t.Fatal(err)
	}
	metadata, _ := service.payloads[0]["metadata"].(map[string]any)
	stored, _ := metadata["provenance"].(Provenance)
	if stored.Origin != OriginGroup || stored.ProposedBy != "sam" || stored.Channel != "c1" {
		t.Fatalf("the stored reading lost its provenance: %v", metadata["provenance"])
	}
}

func TestStoreHashtagExampleWithProvenance(t *testing.T) {
	service := &hashtagStoreStub{}
	example, err := PrepareHashtagExample("https://example.com/meme.jpg", "Games")
	if err != nil {
		t.Fatal(err)
	}
	provenance := Provenance{Origin: OriginGroup, ProposedBy: "sam", SentBy: "tiago", Channel: "memes"}
	if _, err := New(service).StoreHashtagExampleWithProvenance(context.Background(), example, "labelled text", provenance); err != nil {
		t.Fatal(err)
	}
	metadata, _ := service.input["metadata"].(map[string]any)
	stored, _ := metadata["provenance"].(Provenance)
	if stored.Origin != OriginGroup || stored.ProposedBy != "sam" || stored.SentBy != "tiago" || stored.Channel != "memes" {
		t.Fatalf("the example lost its provenance: %v", service.input["metadata"])
	}
}
