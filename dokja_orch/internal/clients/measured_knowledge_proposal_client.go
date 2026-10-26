package clients

import (
	"context"
	"fmt"
	"math"
	knowledge "read_books/dokja_domain/dokja_knowledge"
	"strings"
)

type measuredProposalTransport interface {
	GenerateMeasuredMessages(context.Context, []map[string]string, map[string]int64) (map[string]any, error)
}
type MeasuredKnowledgeProposalClient struct{ chat measuredProposalTransport }

func NewMeasuredKnowledgeProposalClient(chat measuredProposalTransport) *MeasuredKnowledgeProposalClient {
	return &MeasuredKnowledgeProposalClient{chat: chat}
}
func (c *MeasuredKnowledgeProposalClient) Propose(ctx context.Context, prompt knowledge.ProposalPrompt) (knowledge.ProposalResponse, error) {
	reply := knowledge.ProposalResponse{BudgetMode: "post_generation"}
	if c == nil || c.chat == nil {
		return reply, fmt.Errorf("measured application inference is not configured")
	}
	if ctx.Err() != nil {
		return reply, ctx.Err()
	}
	if strings.TrimSpace(prompt.Instructions) == "" || strings.TrimSpace(prompt.Data) == "" || len(prompt.Instructions)+len(prompt.Data) > knowledge.MaxProposalPromptBytes {
		return reply, fmt.Errorf("invalid application inference prompt")
	}
	result, err := c.chat.GenerateMeasuredMessages(ctx, []map[string]string{{"role": "system", "content": prompt.Instructions}, {"role": "user", "content": prompt.Data}}, map[string]int64{"max_input_tokens": knowledge.ProposalMaxInputTokens, "max_output_tokens": knowledge.ProposalMaxOutputTokens})
	if ctx.Err() != nil {
		return reply, ctx.Err()
	}
	if err != nil {
		return reply, fmt.Errorf("measured application inference unavailable")
	}
	reply.Provider, _ = result["provider"].(string)
	reply.RequestedModel, _ = result["requested_model"].(string)
	reply.BudgetStatus, _ = result["status"].(string)
	if result["budget_mode"] != "post_generation" || strings.TrimSpace(reply.Provider) == "" || len(reply.Provider) > 200 || strings.TrimSpace(reply.RequestedModel) == "" || len(reply.RequestedModel) > 200 {
		return knowledge.ProposalResponse{}, fmt.Errorf("invalid measured inference metadata")
	}
	count := func(value any) *int64 {
		n, ok := value.(float64)
		if !ok || n < 0 || n > 9007199254740991 || math.Trunc(n) != n {
			return nil
		}
		v := int64(n)
		return &v
	}
	if usage, ok := result["usage"].(map[string]any); ok {
		reply.InputTokens = count(usage["input_tokens"])
		reply.OutputTokens = count(usage["output_tokens"])
	}
	switch reply.BudgetStatus {
	case "ok", "over_budget", "usage_unavailable", "provider_error", "invalid_reply":
	default:
		reply.BudgetStatus = "invalid_response"
		return reply, fmt.Errorf("invalid measured inference status")
	}
	if reply.BudgetStatus != "ok" {
		return reply, fmt.Errorf("measured inference did not satisfy acceptance budget")
	}
	if reply.InputTokens == nil || reply.OutputTokens == nil {
		reply.BudgetStatus = "usage_unavailable"
		return reply, fmt.Errorf("measured inference lacks valid usage")
	}
	if *reply.InputTokens > knowledge.ProposalMaxInputTokens || *reply.OutputTokens > knowledge.ProposalMaxOutputTokens {
		reply.BudgetStatus = "over_budget"
		return reply, fmt.Errorf("measured inference exceeds acceptance budget")
	}
	reply.Body, _ = result["reply"].(string)
	if strings.TrimSpace(reply.Body) == "" || len(reply.Body) > knowledge.MaxProposalReplyBytes {
		reply.Body = ""
		return reply, fmt.Errorf("invalid measured inference response")
	}
	return reply, nil
}
