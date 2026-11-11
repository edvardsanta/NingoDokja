package handlers

import (
	"context"
	"fmt"
	"strings"
)

type taskTextGenerator interface {
	GenerateTaskText(context.Context, string, []map[string]string, func(string) error) (map[string]any, error)
}

type taskJSONGenerator interface {
	GenerateTaskJSON(context.Context, string, map[string]any, []map[string]string, func(map[string]any) error) (map[string]any, error)
}

func generateLightweightText(ctx context.Context, generator ChatConversationGenerator, messages []map[string]string, accept func(string) error) (map[string]any, error) {
	if task, ok := generator.(taskTextGenerator); ok {
		return task.GenerateTaskText(ctx, "lightweight", messages, accept)
	}
	reply, err := generator.GenerateMessages(ctx, messages)
	if err != nil {
		return nil, err
	}
	text := chatReplyFromResult(reply)
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("empty task text")
	}
	if accept != nil {
		if err := accept(text); err != nil {
			return nil, err
		}
	}
	return reply, nil
}
