package core

import "context"

type Workflow struct {
	Name  string
	Steps []WorkflowStep
}

type WorkflowStep struct {
	Domain Domain
	Action string
}

type WorkflowEngine interface {
	Plan(ctx context.Context, event Event, route Route) (Workflow, error)
}

type DefaultWorkflowEngine struct{}

func NewDefaultWorkflowEngine() *DefaultWorkflowEngine {
	return &DefaultWorkflowEngine{}
}

func (e *DefaultWorkflowEngine) Plan(_ context.Context, event Event, route Route) (Workflow, error) {
	steps := make([]WorkflowStep, 0, len(route.Domains))
	for _, domain := range route.Domains {
		steps = append(steps, WorkflowStep{
			Domain: domain,
			Action: actionFor(domain, event.Type),
		})
	}

	return Workflow{
		Name:  route.Workflow,
		Steps: steps,
	}, nil
}

func actionFor(domain Domain, eventType string) string {
	switch domain {
	case DomainSystem:
		switch eventType {
		case "ningo.status":
			return "inspect-ningo-platform"
		case "meme.dispatch.scheduled":
			return "deliver-scheduled-memes"
		default:
			return "inspect-system"
		}
	case DomainModeration:
		return "screen-event"
	case DomainChat:
		if eventType == "message.created" {
			return "generate-response"
		}
		return "handle-conversation"
	case DomainMeme:
		switch eventType {
		case "meme.fetch":
			return "fetch-memes"
		case "meme.pool.refresh":
			return "refresh-meme-pool"
		case "meme.status":
			return "inspect-meme-service"
		default:
			return "handle-meme-event"
		}
	case DomainMemory:
		return "sync-memory"
	case DomainAutomation:
		return "execute-automation"
	default:
		return "handle-event"
	}
}
