package core

import (
	"context"
	"fmt"
	"read_books/internal/sourceaccess"
	"strings"
)

func (s *Service) WithSourceSupervisionToken(token string) *Service {
	s.sourceSupervisionToken = token
	return s
}
func (s *Service) authorizeSourceSupervision(ctx context.Context, event *Event) (context.Context, error) {
	supplied, _ := event.Context["source_supervision_token"].(string)
	clean := make(map[string]any, len(event.Context))
	for key, value := range event.Context {
		if key != "source_supervision_token" {
			clean[key] = value
		}
	}
	event.Context = clean
	if !strings.HasPrefix(event.Type, "knowledge.application.supervision.") {
		return ctx, nil
	}
	if event.Source != SourceDesktop {
		return ctx, fmt.Errorf("source supervision requires the Desktop operator")
	}
	return sourceaccess.Authenticate(ctx, s.sourceSupervisionToken, supplied)
}
