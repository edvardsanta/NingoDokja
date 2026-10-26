package core

import (
	"context"
	"testing"
)

// Step 03 baseline (steps.json): the public feature events that ADR 0006 and ADR 0007
// keep as compatibility facades. A migration to faculty workflows must keep each event
// reaching the same action, or record the change deliberately.
func TestBaselineFeatureEventsKeepTheirWorkflowAndAction(t *testing.T) {
	cases := []struct {
		eventType string
		workflow  string
		domain    Domain
		action    string
	}{
		{"book.summary.requested", "book", DomainBook, "summarize-book"},
		{"book.resource.classify", "book", DomainBook, "classify-book-resource"},
		{"book.other", "book", DomainBook, "handle-book-event"},
		{"meme.fetch", "meme", DomainMeme, "fetch-memes"},
		{"meme.pool.refresh", "meme", DomainMeme, "refresh-meme-pool"},
		{"meme.status", "meme", DomainMeme, "inspect-meme-service"},
		{"meme.screen", "meme", DomainMeme, "screen-meme"},
		{"meme.list", "meme", DomainMeme, "list-memes"},
		{"meme.hashtag.tag", "meme", DomainMeme, "tag-meme-hashtag"},
		{"meme.hashtag.suggest", "meme", DomainMeme, "suggest-meme-hashtag"},
		{"meme.hashtag.list", "meme", DomainMeme, "list-meme-hashtags"},
		{"meme.hashtag.untag", "meme", DomainMeme, "untag-meme-hashtag"},
		{"meme.dispatch.scheduled", "meme-dispatch", DomainSystem, "deliver-scheduled-memes"},
		{"digest.items", "digest", DomainDigest, "read-digest"},
		{"conversation.list", "conversation-archive", DomainConversation, "manage-conversation"},
		{"conversation.read", "conversation-archive", DomainConversation, "manage-conversation"},
		{"digest.status", "digest", DomainDigest, "inspect-digest"},
		{"knowledge.application.prepare", "knowledge", DomainKnowledge, "prepare-knowledge-application"},
		{"knowledge.ingest", "knowledge", DomainKnowledge, "ingest-knowledge"},
		{"knowledge.search", "knowledge", DomainKnowledge, "search-knowledge"},
		{"knowledge.list", "knowledge", DomainKnowledge, "list-knowledge"},
		{"knowledge.delete", "knowledge", DomainKnowledge, "delete-knowledge"},
		{"knowledge.status", "knowledge", DomainKnowledge, "inspect-knowledge"},
		{"knowledge.reindex", "knowledge", DomainKnowledge, "reindex-knowledge"},
	}
	router := NewRuleBasedEventRouter()
	engine := NewDefaultWorkflowEngine()
	for _, tc := range cases {
		t.Run(tc.eventType, func(t *testing.T) {
			event := Event{Type: tc.eventType}
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
