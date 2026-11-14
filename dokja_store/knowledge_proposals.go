package store

import (
	"database/sql"
	"fmt"
)

// KnowledgeProposal is one pending proposal row. The payload is the kind's
// canonical JSON; the Knowledge domain validates it before a row exists. The
// store keeps rows verbatim and never interprets them.
type KnowledgeProposal struct {
	ID              string
	Kind            string
	Payload         string
	ProposedBy      string
	SentBy          string
	SourceChannel   string
	SourceMessage   string
	SourcePermalink string
	ProposedAt      string
}

func (p KnowledgeProposal) validate() error {
	for field, value := range map[string]string{
		"id": p.ID, "kind": p.Kind, "payload": p.Payload, "proposed_by": p.ProposedBy, "proposed_at": p.ProposedAt,
	} {
		if value == "" {
			return fmt.Errorf("proposal %s is required", field)
		}
	}
	return nil
}

// AddProposal stores one pending proposal. The id is stable for the item, so a
// repeated submission of the same thing is a conflict, not a second row.
func (d *DB) AddProposal(p KnowledgeProposal) error {
	if err := p.validate(); err != nil {
		return err
	}
	_, err := d.db.Exec(
		`INSERT INTO knowledge_proposals(id, kind, payload, proposed_by, sent_by, source_channel, source_message, source_permalink, proposed_at)
		 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Kind, p.Payload, p.ProposedBy, p.SentBy, p.SourceChannel, p.SourceMessage, p.SourcePermalink, p.ProposedAt)
	return err
}

// UpdateProposalPayload replaces the payload of a proposal that is still pending, and says whether
// there was one. What reads a proposal's media in the background writes its result here; a proposal
// decided meanwhile is gone, and the write finds nothing.
func (d *DB) UpdateProposalPayload(id, payload string) (bool, error) {
	if id == "" || payload == "" {
		return false, fmt.Errorf("proposal id and payload are required")
	}
	result, err := d.db.Exec(`UPDATE knowledge_proposals SET payload = ? WHERE id = ?`, payload, id)
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	return changed > 0, err
}

// ListProposals returns every pending proposal, oldest first.
func (d *DB) ListProposals() ([]KnowledgeProposal, error) {
	rows, err := d.db.Query(
		`SELECT id, kind, payload, proposed_by, sent_by, source_channel, source_message, source_permalink, proposed_at
		 FROM knowledge_proposals ORDER BY proposed_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	proposals := []KnowledgeProposal{}
	for rows.Next() {
		var p KnowledgeProposal
		if err := rows.Scan(&p.ID, &p.Kind, &p.Payload, &p.ProposedBy, &p.SentBy, &p.SourceChannel, &p.SourceMessage, &p.SourcePermalink, &p.ProposedAt); err != nil {
			return nil, err
		}
		proposals = append(proposals, p)
	}
	return proposals, rows.Err()
}

// GetProposal returns one pending proposal by id.
func (d *DB) GetProposal(id string) (KnowledgeProposal, bool, error) {
	var p KnowledgeProposal
	err := d.db.QueryRow(
		`SELECT id, kind, payload, proposed_by, sent_by, source_channel, source_message, source_permalink, proposed_at
		 FROM knowledge_proposals WHERE id = ?`, id).
		Scan(&p.ID, &p.Kind, &p.Payload, &p.ProposedBy, &p.SentBy, &p.SourceChannel, &p.SourceMessage, &p.SourcePermalink, &p.ProposedAt)
	if err == sql.ErrNoRows {
		return KnowledgeProposal{}, false, nil
	}
	if err != nil {
		return KnowledgeProposal{}, false, err
	}
	return p, true, nil
}

// DeleteProposal removes a pending proposal. Approval and rejection both end
// here, after their effects happened elsewhere.
func (d *DB) DeleteProposal(id string) error {
	_, err := d.db.Exec(`DELETE FROM knowledge_proposals WHERE id = ?`, id)
	return err
}

// CountProposalsBy counts pending proposals of one proposer, for the per-person
// submission bound.
func (d *DB) CountProposalsBy(proposedBy string) (int, error) {
	var count int
	err := d.db.QueryRow(`SELECT COUNT(*) FROM knowledge_proposals WHERE proposed_by = ?`, proposedBy).Scan(&count)
	return count, err
}
