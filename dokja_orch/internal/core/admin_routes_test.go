package core

import (
	"context"
	"testing"
)

func TestAdminEventsAreRoutedAndPlanned(t *testing.T) {
	cases := []struct {
		eventType string
		workflow  string
		domain    Domain
		action    string
	}{
		{"discord.send", "discord-send", DomainSystem, "send-discord-message"},
		{"meme.screen", "meme", DomainMeme, "screen-meme"},
		{"services.set", "admin", DomainSystem, "set-service"},
		{"scheduler.jobs.set", "admin", DomainSystem, "set-job"},
		{"scheduler.jobs.announce", "admin", DomainSystem, "record-job-announce"},
		{"chat.profiles.list", "admin", DomainSystem, "list-chat-profiles"},
		{"chat.profile.use", "admin", DomainSystem, "use-chat-profile"},
	}

	for _, tc := range cases {
		route, err := NewRuleBasedEventRouter().Route(context.Background(), Event{Type: tc.eventType})
		if err != nil {
			t.Fatalf("%s: route error: %v", tc.eventType, err)
		}
		if route.Workflow != tc.workflow || len(route.Domains) != 1 || route.Domains[0] != tc.domain {
			t.Fatalf("%s: unexpected route %#v", tc.eventType, route)
		}

		workflow, err := NewDefaultWorkflowEngine().Plan(context.Background(), Event{Type: tc.eventType}, route)
		if err != nil {
			t.Fatalf("%s: plan error: %v", tc.eventType, err)
		}
		if len(workflow.Steps) != 1 || workflow.Steps[0].Action != tc.action {
			t.Fatalf("%s: expected action %q, got %#v", tc.eventType, tc.action, workflow.Steps)
		}
	}
}
