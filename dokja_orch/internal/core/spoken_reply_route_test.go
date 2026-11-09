package core

import (
	"context"
	"testing"
)

// A spoken Desktop reply is a chat step; every other voice event keeps the
// communications voice-audio workflow it had before.
func TestSpokenReplyRoutesToChatAndOtherVoiceEventsStay(t *testing.T) {
	cases := []struct {
		eventType string
		workflow  string
		domain    Domain
		action    string
	}{
		{"voice.reply.speak", "spoken-reply", DomainChat, "speak-reply"},
		{"voice.transcribe", "voice-audio", DomainCommunications, "transcribe-voice"},
		{"voice.synthesize", "voice-audio", DomainCommunications, "synthesize-voice"},
	}
	router := NewRuleBasedEventRouter()
	engine := NewDefaultWorkflowEngine()
	for _, tc := range cases {
		t.Run(tc.eventType, func(t *testing.T) {
			event := Event{Type: tc.eventType, Source: SourceDesktop}
			route, err := router.Route(context.Background(), event)
			if err != nil {
				t.Fatal(err)
			}
			workflow, err := engine.Plan(context.Background(), event, route)
			if err != nil {
				t.Fatal(err)
			}
			if workflow.Name != tc.workflow || len(workflow.Steps) != 1 ||
				workflow.Steps[0].Domain != tc.domain || workflow.Steps[0].Action != tc.action {
				t.Fatalf("expected %s -> %s/%s, got %#v", tc.workflow, tc.domain, tc.action, workflow)
			}
		})
	}
}
