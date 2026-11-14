package ingress

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	chat "read_books/dokja_domain/dokja_chat"
	"read_books/internal/core"
)

func bridgeContext(t *testing.T, source string, payload map[string]any, ownerID string) map[string]any {
	t.Helper()
	moderation := &identityPass{domain: core.DomainModeration}
	service := core.DefaultService(&identityPass{domain: core.DomainCommunications}, moderation, &identityPass{domain: core.DomainChat})
	app := fiber.New()
	app.Post("/orchestrator", (&HTTPBridgeIngress{service: service, token: "fixture-secret", discordOwnerID: ownerID}).handle)
	body, _ := json.Marshal(map[string]any{"event_type": "message.created", "interface": source, "payload": payload})
	request := httptest.NewRequest("POST", "/orchestrator", strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer fixture-secret")
	response, err := app.Test(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("unexpected bridge status %d", response.StatusCode)
	}
	return moderation.event.Context
}

func TestBridgeCarriesTheNamesOfTaggedDiscordAccountsWhetherOrNotAnOwnerIsConfigured(t *testing.T) {
	payload := map[string]any{"userId": "fixture-member", "channelId": "fixture-channel", "content": "oi <@111111111111111111>",
		"mentionedUsers": []map[string]string{{"id": "111111111111111111", "name": "Pedro"}}}
	for _, ownerID := range []string{"", "fixture-owner"} {
		mentions, _ := bridgeContext(t, "discord", payload, ownerID)["mentioned_users"].([]chat.Mention)
		if len(mentions) != 1 || mentions[0].ID != "111111111111111111" || mentions[0].Name != "Pedro" {
			t.Fatalf("owner %q: tagged account names did not reach the event: %#v", ownerID, mentions)
		}
	}
}

func TestBridgeCapsTheTaggedAccountsAndIgnoresThemOffDiscord(t *testing.T) {
	many := make([]map[string]string, 0, chat.MaxMentions+5)
	for i := 0; i < chat.MaxMentions+5; i++ {
		many = append(many, map[string]string{"id": "1000000000000000" + string(rune('a'+i)), "name": "x"})
	}
	payload := map[string]any{"userId": "fixture-member", "channelId": "fixture-channel", "content": "oi", "mentionedUsers": many}
	if mentions, _ := bridgeContext(t, "discord", payload, "")["mentioned_users"].([]chat.Mention); len(mentions) != chat.MaxMentions {
		t.Fatalf("got %d tagged accounts, want the cap of %d", len(mentions), chat.MaxMentions)
	}
	if _, present := bridgeContext(t, "whatsapp", payload, "")["mentioned_users"]; present {
		t.Fatal("tagged accounts were taken from a source that is not Discord")
	}
}
