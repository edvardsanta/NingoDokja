package core

import (
	"crypto/subtle"
	"fmt"
	"strings"
)

// New communications routes are opt-in and authenticated across every ingress. The
// credential is removed before context building, dispatch and verbose responses.
func (s *Service) WithCommunicationsToken(token string) *Service {
	s.communicationsToken = token
	return s
}
func (s *Service) authorizeCommunications(event *Event) error {
	supplied, _ := event.Context["communications_token"].(string)
	clean := make(map[string]any, len(event.Context))
	for key, value := range event.Context {
		if key != "communications_token" {
			clean[key] = value
		}
	}
	event.Context = clean
	if !strings.HasPrefix(event.Type, "communications.") {
		return nil
	}
	token := s.communicationsToken
	if len(token) < 32 || strings.HasPrefix(token, "CHANGE_ME") {
		return fmt.Errorf("communications access is not configured")
	}
	if subtle.ConstantTimeCompare([]byte(supplied), []byte(token)) != 1 {
		return fmt.Errorf("communications access denied")
	}
	return nil
}
