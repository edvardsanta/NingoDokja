package conversation

import (
	"context"
	"strings"
	"testing"
)

func TestOnlyTheLinkedAccountsSelfChatMaySendForTheOwner(t *testing.T) {
	cases := []struct {
		source   string
		selfChat bool
		want     bool
	}{
		{"desktop", false, true}, {"cli", false, true}, {"tui", false, true},
		{"whatsapp", true, true},
		{"whatsapp", false, false}, {"discord", true, false}, {"web", true, false}, {"", true, false},
	}
	for _, c := range cases {
		scope, ok := SendScopeFor(c.source, c.selfChat)
		if ok != c.want || ok && scope != OwnerScope {
			t.Errorf("SendScopeFor(%q, %t) = %q, %t", c.source, c.selfChat, scope, ok)
		}
	}
	// Sending is all the self-chat may do: the archive's other operations keep refusing WhatsApp.
	if _, ok := ScopeFor("whatsapp"); ok {
		t.Fatal("WhatsApp must not list, read, rename or delete conversations")
	}
}

type listing struct {
	recorder
	items []Conversation
}

func (l *listing) List(_ context.Context, _ Scope, _ string, _ int) ([]Conversation, string, error) {
	return l.items, "", nil
}

func TestASendWithoutAConversationContinuesTheLatestGeneralOne(t *testing.T) {
	store := &listing{items: []Conversation{
		{ID: "reading", Kind: KindReading, Turns: 4},
		{ID: "just-started", Kind: KindGeneral, Turns: 0},
		{ID: "older", Kind: KindGeneral, Turns: 9},
	}}
	got, found, err := New(store).Continued(context.Background(), OwnerScope)
	if err != nil || !found || got.ID != "just-started" {
		t.Fatal("a conversation just started is the one continued", got, found, err)
	}
	store.items = store.items[:1]
	if _, found, err := New(store).Continued(context.Background(), OwnerScope); err != nil || found {
		t.Fatal("with nothing to continue, a new conversation is started", found, err)
	}
	if _, _, err := New(store).Continued(context.Background(), Scope("other")); err != ErrInvalid {
		t.Fatal(err)
	}
}

func TestANewConversationIsNamedByTheStartOfItsFirstMessage(t *testing.T) {
	if got := TitleFrom("  what   is\n justice? "); got != "what is justice?" {
		t.Fatal(got)
	}
	long := TitleFrom(strings.Repeat("á", 80))
	if len([]rune(long)) != 60 || !strings.HasSuffix(long, "…") {
		t.Fatal(long)
	}
}

func TestTouchingNeedsTheCapabilityAndAValidRequest(t *testing.T) {
	archive := New(&recorder{})
	id := strings.Repeat("a", 32)
	if _, err := archive.Touch(context.Background(), OwnerScope, id, 1); err != ErrUnavailable {
		t.Fatal("a store without the capability must refuse", err)
	}
	for _, bad := range []struct {
		scope    Scope
		id       string
		revision uint64
	}{{Scope("other"), id, 1}, {OwnerScope, "short", 1}, {OwnerScope, id, 0}} {
		if _, err := archive.Touch(context.Background(), bad.scope, bad.id, bad.revision); err != ErrInvalid {
			t.Fatal(bad, err)
		}
	}
}
