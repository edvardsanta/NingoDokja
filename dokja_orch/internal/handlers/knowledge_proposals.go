package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	digest "read_books/dokja_domain/dokja_digest"
	knowledgedomain "read_books/dokja_domain/dokja_knowledge"
	"read_books/dokja_store"
	"read_books/internal/core"
	"read_books/internal/logger"
)

// maxProposalsPerProposer bounds how much one person may hold pending at once,
// so an open channel cannot stack submissions faster than the owner decides them.
const maxProposalsPerProposer = 20

// proposalStore is what the proposals handler needs from the shared database.
type proposalStore interface {
	AddProposal(store.KnowledgeProposal) error
	ListProposals() ([]store.KnowledgeProposal, error)
	GetProposal(id string) (store.KnowledgeProposal, bool, error)
	DeleteProposal(id string) error
	CountProposalsBy(proposedBy string) (int, error)
}

// proposalKindWorkflow is what one kind of proposal needs to be decided. Approve
// stores what was proposed through the kind's existing ingestion path; a refusal
// returns a fixed reason and leaves the proposal pending. Current returns the
// stored record a decision would affect, for the queue to show next to it.
type proposalKindWorkflow struct {
	Approve func(ctx context.Context, proposal knowledgedomain.Proposal, edit map[string]any) (map[string]any, error)
	Current func(ctx context.Context, payload map[string]any) (map[string]any, bool)
}

var proposalKindWorkflows = map[string]proposalKindWorkflow{}

// RegisterProposalKindWorkflow registers a kind's payload rules with the
// Knowledge domain and its decision flow with the orchestrator. It runs once at
// startup wiring; a kind nobody registered is not a kind a proposal can have.
func RegisterProposalKindWorkflow(rules knowledgedomain.ProposalKindRules, workflow proposalKindWorkflow) error {
	if workflow.Approve == nil || workflow.Current == nil {
		return fmt.Errorf("proposal kind %q needs an approve flow and a current lookup", rules.Kind)
	}
	if err := knowledgedomain.RegisterProposalKind(rules); err != nil {
		return err
	}
	proposalKindWorkflows[rules.Kind] = workflow
	return nil
}

// KnowledgeProposalsHandler sequences proposals for what Ningo learns. The
// Knowledge domain decides validity, duplicates and precedence; this handler
// only sequences the store, the kind's approval flow and the owner-facing
// views. Nothing here announces a proposal, an approval or a rejection:
// pending is invisible outside the owner's queue.
type KnowledgeProposalsHandler struct {
	store     proposalStore
	knowledge *knowledgedomain.Domain
	web       digest.Service
	reader    DocumentReader
	now       func() time.Time
}

func NewKnowledgeProposalsHandler(db proposalStore, knowledge knowledgedomain.Service, web digest.Service, reader DocumentReader) *KnowledgeProposalsHandler {
	// The built-in kinds register their payload rules once; a registration from
	// an earlier wiring wins, the rules are pure and identical.
	for _, rules := range []knowledgedomain.ProposalKindRules{webArticleKindRules(), documentKindRules()} {
		if _, exists := knowledgedomain.ProposalKindRulesFor(rules.Kind); exists {
			continue
		}
		if err := knowledgedomain.RegisterProposalKind(rules); err != nil {
			logger.Error("register a knowledge proposal kind; its proposals are refused", err)
		}
	}
	return &KnowledgeProposalsHandler{store: db, knowledge: knowledgedomain.New(knowledge), web: web, reader: reader, now: time.Now}
}

func proposalWorkflowFor(kind string) (proposalKindWorkflow, bool) {
	workflow, ok := proposalKindWorkflows[kind]
	return workflow, ok
}

// Propose validates one submission and holds it pending. It returns the
// proposal id and whether the submission was already pending. The caller in a
// chat turn stays silent either way.
func (h *KnowledgeProposalsHandler) Propose(ctx context.Context, kind string, payload map[string]any, source knowledgedomain.ProposalSource, proposedBy, sentBy string) (string, bool, error) {
	if h == nil || h.store == nil {
		return "", false, fmt.Errorf("knowledge proposals are not configured")
	}
	proposal, err := knowledgedomain.ValidateProposal(kind, payload, proposedBy, sentBy)
	if err != nil {
		return "", false, err
	}
	rules, _ := knowledgedomain.ProposalKindRulesFor(kind)
	rows, err := h.store.ListProposals()
	if err != nil {
		return "", false, err
	}
	pending := make([]knowledgedomain.Proposal, 0, len(rows))
	for _, row := range rows {
		held, err := proposalFromRow(row)
		if err != nil {
			return "", false, err
		}
		pending = append(pending, held)
	}
	if duplicate, ok := knowledgedomain.PendingDuplicate(pending, kind, rules.Identity(proposal.Payload)); ok {
		return duplicate.ID, true, nil
	}
	if count, err := h.store.CountProposalsBy(proposal.ProposedBy); err != nil {
		return "", false, err
	} else if count >= maxProposalsPerProposer {
		return "", false, fmt.Errorf("too many pending proposals for %s", proposal.ProposedBy)
	}
	proposal.Source = source
	proposal.ProposedAt = h.now().UTC()
	encoded, err := json.Marshal(proposal.Payload)
	if err != nil {
		return "", false, err
	}
	row := store.KnowledgeProposal{
		ID: proposal.ID, Kind: proposal.Kind, Payload: string(encoded),
		ProposedBy: proposal.ProposedBy, SentBy: proposal.SentBy,
		SourceChannel: proposal.Source.ChannelID, SourceMessage: proposal.Source.MessageID,
		SourcePermalink: proposal.Source.Permalink, ProposedAt: proposal.ProposedAt.Format(time.RFC3339Nano),
	}
	if err := h.store.AddProposal(row); err != nil {
		return "", false, err
	}
	logger.Info(fmt.Sprintf("knowledge proposal held id=%s kind=%s proposed_by=%s", proposal.ID, proposal.Kind, proposal.ProposedBy))
	return proposal.ID, false, nil
}

// proposalFromRow restores one pending proposal with its canonical payload.
func proposalFromRow(row store.KnowledgeProposal) (knowledgedomain.Proposal, error) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
		return knowledgedomain.Proposal{}, fmt.Errorf("pending proposal %s has an invalid payload", row.ID)
	}
	proposedAt, err := time.Parse(time.RFC3339Nano, row.ProposedAt)
	if err != nil {
		return knowledgedomain.Proposal{}, fmt.Errorf("pending proposal %s has an invalid time", row.ID)
	}
	return knowledgedomain.Proposal{
		ID: row.ID, Kind: row.Kind, Payload: payload,
		ProposedBy: row.ProposedBy, SentBy: row.SentBy,
		Source: knowledgedomain.ProposalSource{
			ChannelID: row.SourceChannel, MessageID: row.SourceMessage, Permalink: row.SourcePermalink,
		},
		ProposedAt: proposedAt,
	}, nil
}

func (h *KnowledgeProposalsHandler) Handle(ctx context.Context, event core.Event, workflow core.WorkflowStep) (map[string]any, error) {
	if h == nil || h.store == nil {
		return nil, fmt.Errorf("knowledge proposals are not configured")
	}
	switch workflow.Action {
	case "propose-knowledge":
		payload, _ := event.Payload["proposal_payload"].(map[string]any)
		source := knowledgedomain.ProposalSource{
			ChannelID: resultString(event.Payload, "source_channel"), MessageID: resultString(event.Payload, "source_message"),
			Permalink: resultString(event.Payload, "source_permalink"),
		}
		id, duplicate, err := h.Propose(ctx, resultString(event.Payload, "kind"), payload, source, resultString(event.Payload, "proposed_by"), resultString(event.Payload, "sent_by"))
		if err != nil {
			return nil, err
		}
		return map[string]any{"proposal_id": id, "duplicate": duplicate}, nil
	case "list-knowledge-proposals":
		return h.list(ctx)
	case "approve-knowledge-proposal":
		return h.decide(ctx, resultString(event.Payload, "proposal_id"), event.Payload["edit"], false)
	case "reject-knowledge-proposal":
		return h.decide(ctx, resultString(event.Payload, "proposal_id"), nil, true)
	default:
		return nil, fmt.Errorf("unsupported proposals action %q", workflow.Action)
	}
}

// list is the owner-facing queue: every pending proposal with the stored record
// its decision would affect, so an approval that replaces something says so.
func (h *KnowledgeProposalsHandler) list(ctx context.Context) (map[string]any, error) {
	rows, err := h.store.ListProposals()
	if err != nil {
		return nil, err
	}
	views := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		proposal, err := proposalFromRow(row)
		if err != nil {
			return nil, err
		}
		view := map[string]any{
			"id": proposal.ID, "kind": proposal.Kind, "payload": proposal.Payload,
			"proposed_by": proposal.ProposedBy, "sent_by": proposal.SentBy,
			"source": map[string]any{
				"channel_id": proposal.Source.ChannelID, "message_id": proposal.Source.MessageID,
				"permalink": proposal.Source.Permalink,
			},
			"proposed_at": proposal.ProposedAt.Format(time.RFC3339Nano),
		}
		if workflow, ok := h.workflowFor(proposal.Kind); ok {
			if current, found := workflow.Current(ctx, proposal.Payload); found {
				view["current"] = current
			}
		}
		views = append(views, view)
	}
	return map[string]any{"proposals": views}, nil
}

// decide approves or rejects one proposal. Approving runs the kind's flow and
// removes the proposal only when the record is stored; a refusal leaves it
// pending with its fixed reason. Rejecting deletes it, keeping nothing.
func (h *KnowledgeProposalsHandler) decide(ctx context.Context, id string, edit any, reject bool) (map[string]any, error) {
	if id == "" {
		return nil, fmt.Errorf("proposal_id is required")
	}
	row, ok, err := h.store.GetProposal(id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("pending proposal %s was not found", id)
	}
	if reject {
		if err := h.store.DeleteProposal(id); err != nil {
			return nil, err
		}
		return map[string]any{"proposal_id": id, "rejected": true}, nil
	}
	proposal, err := proposalFromRow(row)
	if err != nil {
		return nil, err
	}
	workflow, ok := h.workflowFor(proposal.Kind)
	if !ok {
		return nil, fmt.Errorf("proposal kind %q has no approval flow", proposal.Kind)
	}
	editPayload, _ := edit.(map[string]any)
	outcome, err := workflow.Approve(ctx, proposal, editPayload)
	if err != nil {
		return nil, err
	}
	if refused, _ := outcome["refused"].(string); refused != "" {
		return map[string]any{"proposal_id": id, "approved": false, "reason": refused}, nil
	}
	if err := h.store.DeleteProposal(id); err != nil {
		return nil, err
	}
	answer := map[string]any{"proposal_id": id, "approved": true}
	for key, value := range outcome {
		answer[key] = value
	}
	logger.Info(fmt.Sprintf("knowledge proposal approved id=%s kind=%s", id, proposal.Kind))
	return answer, nil
}
