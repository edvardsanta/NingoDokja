package clients

import (
	"context"
	"fmt"
	knowledge "read_books/dokja_domain/dokja_knowledge"
	"strings"
)

type proposalChatTransport interface {
	GenerateMessages(context.Context, []map[string]string) (map[string]any, error)
}

// KnowledgeProposalClient reuses Runtime's selected provider/model and isolated,
// tool-disabled chat transport. It neither retries nor validates domain meaning.
type KnowledgeProposalClient struct{ chat proposalChatTransport }

func NewKnowledgeProposalClient(chat proposalChatTransport) *KnowledgeProposalClient {
	return &KnowledgeProposalClient{chat: chat}
}
func (c *KnowledgeProposalClient) Propose(ctx context.Context, prompt knowledge.ProposalPrompt) (knowledge.ProposalResponse, error) {
	if c == nil || c.chat == nil {
		return knowledge.ProposalResponse{}, fmt.Errorf("application inference is not configured")
	}
	if ctx.Err() != nil {
		return knowledge.ProposalResponse{}, ctx.Err()
	}
	if strings.TrimSpace(prompt.Instructions) == "" || strings.TrimSpace(prompt.Data) == "" || len(prompt.Instructions)+len(prompt.Data) > knowledge.MaxProposalPromptBytes {
		return knowledge.ProposalResponse{}, fmt.Errorf("invalid application inference prompt")
	}
	response, err := c.chat.GenerateMessages(ctx, []map[string]string{{"role": "system", "content": prompt.Instructions}, {"role": "user", "content": prompt.Data}})
	if ctx.Err() != nil {
		return knowledge.ProposalResponse{}, ctx.Err()
	}
	if err != nil {
		return knowledge.ProposalResponse{}, fmt.Errorf("application inference is unavailable")
	}
	body, _ := response["reply"].(string)
	provider, _ := response["provider"].(string)
	model, _ := response["requested_model"].(string)
	if strings.TrimSpace(body) == "" || len(body) > knowledge.MaxProposalReplyBytes || strings.TrimSpace(provider) == "" || len(provider) > 200 || strings.TrimSpace(model) == "" || len(model) > 200 {
		return knowledge.ProposalResponse{}, fmt.Errorf("invalid application inference response")
	}
	return knowledge.ProposalResponse{Body: body, Provider: provider, RequestedModel: model}, nil
}
