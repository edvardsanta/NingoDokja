package clients

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func conversationsServer(t *testing.T, handler http.HandlerFunc) (*WhatsAppConversationsClient, *[]string) {
	t.Helper()
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.RequestURI())
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	return NewWhatsAppConversationsClient(server.URL + "/status"), &seen
}

func TestConversationsListKeepsOnlyValidAliasesAndKinds(t *testing.T) {
	client, seen := conversationsServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"conversations": []any{
			map[string]any{"alias": "friend", "kind": "direct", "retention_days": 30},
			map[string]any{"alias": "team", "kind": "group", "retention_days": 9999},
			map[string]any{"alias": "../etc", "kind": "direct", "retention_days": 30},
			map[string]any{"alias": "other", "kind": "channel", "retention_days": 30},
		}})
	})

	listed, err := client.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(listed) != 2 || listed[0].Alias != "friend" || listed[0].RetentionDays != 30 || listed[1].RetentionDays != 0 {
		t.Fatalf("unexpected list %#v", listed)
	}
	if strings.Join(*seen, ",") != "GET /conversations" {
		t.Fatalf("unexpected requests %v", *seen)
	}
}

func TestConversationHistoryAsksByAliasAndCursorAndShapesTheMessages(t *testing.T) {
	long := strings.Repeat("a", 6000)
	client, seen := conversationsServer(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"alias": "friend",
			"messages": []any{
				map[string]any{"id": "7", "author": "owner", "bot": false, "content": "hi\x1b[31m there", "timestamp": "2026-10-11T12:00:00.000Z"},
				map[string]any{"id": "8", "author": "bot", "bot": false, "content": long, "timestamp": "2026-10-11T12:01:00.000Z"},
				map[string]any{"id": "x", "author": "owner", "content": "bad id"},
				map[string]any{"id": "9", "author": "<script>", "content": "bad author"},
			},
			"before": "7",
			"secret": "not passed on",
		})
	})

	page, err := client.History(context.Background(), "friend", "12")
	if err != nil {
		t.Fatal(err)
	}

	messages := page["messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("expected the two valid messages, got %d", len(messages))
	}
	first := messages[0].(map[string]any)
	if first["content"] != "hi[31m there" || first["bot"] != false || first["edited"] != false {
		t.Fatalf("unexpected first message %#v", first)
	}
	second := messages[1].(map[string]any)
	if second["bot"] != true || len(second["content"].(string)) != conversationsTextMax {
		t.Fatalf("the bot flag follows the author and the text is cut, got %#v", second)
	}
	if page["before"] != "7" {
		t.Fatalf("unexpected cursor %v", page["before"])
	}
	if _, leaked := page["secret"]; leaked {
		t.Fatal("only the page is passed on")
	}
	if strings.Join(*seen, ",") != "GET /history?alias=friend&before=12" {
		t.Fatalf("unexpected requests %v", *seen)
	}
}

func TestConversationHistoryRefusesWhatIsNotAnAliasAndACursor(t *testing.T) {
	client, seen := conversationsServer(t, func(w http.ResponseWriter, r *http.Request) { writeJSON(w, map[string]any{}) })

	for _, args := range [][2]string{{"../friend", ""}, {"Friend", ""}, {"friend", "1;drop"}, {"", ""}} {
		if _, err := client.History(context.Background(), args[0], args[1]); err == nil {
			t.Fatalf("%v must be refused", args)
		}
	}
	if len(*seen) != 0 {
		t.Fatalf("a refused request must not be sent, saw %v", *seen)
	}
}

func TestConversationsFailuresAreErrorsAndNilIsNotConfigured(t *testing.T) {
	client, _ := conversationsServer(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) })
	if _, err := client.List(context.Background()); err == nil {
		t.Fatal("a 404 must be an error")
	}
	var missing *WhatsAppConversationsClient
	if _, err := missing.List(context.Background()); err == nil {
		t.Fatal("a nil client must report it is not configured")
	}
	if NewWhatsAppConversationsClient("") != nil {
		t.Fatal("no address must give no client")
	}
}
