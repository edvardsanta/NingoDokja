package communications

import (
	"reflect"
	"strings"
	"testing"
)

func TestChannelPlanPreservesOrderAndValidatesAllTargets(t *testing.T) {
	policy := DeliveryPolicy{Channels: []string{"one", "two"}}
	plan, err := policy.PlanChannelSend(SendRequest{Content: " hello ", Targets: []string{"two", "two", "one"}})
	if err != nil || !reflect.DeepEqual(plan.Targets, []string{"two", "one"}) || plan.Content != "hello" {
		t.Fatal(plan, err)
	}
	for _, targets := range [][]string{nil, {"one", "unknown"}} {
		if _, err = policy.PlanChannelSend(SendRequest{Content: "hello", Targets: targets}); err == nil {
			t.Fatal("accepted invalid target set", targets)
		}
	}
	plan, err = policy.PlanChannelSend(SendRequest{Content: "hello", All: true, Targets: []string{"unknown"}})
	if err != nil || !reflect.DeepEqual(plan.Targets, policy.Channels) {
		t.Fatal(plan, err)
	}
	plan.Targets[0] = "mutated"
	if policy.Channels[0] != "one" {
		t.Fatal("plan aliases configuration")
	}
}
func TestDisabledAndSafetyPolicy(t *testing.T) {
	p := DeliveryPolicy{Channels: []string{"one"}, SafeOnly: map[string]struct{}{"one": {}}, Disabled: map[string]bool{}}
	if p.Eligible("one", false, false) || !p.Eligible("one", true, false) || !p.Eligible("one", false, true) {
		t.Fatal("incorrect safety policy")
	}
	if p.Eligible("unknown", true, true) {
		t.Fatal("unconfigured target authorized")
	}
	p.Disabled["discord"] = true
	if p.Eligible("one", true, true) {
		t.Fatal("bypass enabled a disabled platform")
	}
	if _, err := p.PlanChannelSend(SendRequest{Content: "hello", All: true}); err == nil || !strings.Contains(err.Error(), "switched off") {
		t.Fatal(err)
	}
	p.Channels = []string{"one", "whatsapp:friend"}
	plan, err := p.PlanChannelSend(SendRequest{Content: "hello", All: true})
	if err != nil || !reflect.DeepEqual(plan.Targets, []string{"whatsapp:friend"}) || !reflect.DeepEqual(plan.SwitchedOff, []string{"one"}) {
		t.Fatal(plan, err)
	}
}
func TestExplicitBypassOriginMatrix(t *testing.T) {
	p := DeliveryPolicy{Channels: []string{"one"}}
	for _, source := range []string{"desktop", "cli", "api", "discord", "whatsapp"} {
		for _, action := range []string{"communications.send", "discord.send", "meme.dispatch.scheduled"} {
			for _, operator := range []string{"desktop", "tui", "", "discord"} {
				expected := (source == "desktop" && action == "communications.send" && operator == "desktop") || (source == "cli" && action == "discord.send" && operator == "tui")
				_, err := p.PlanChannelSend(SendRequest{Source: source, Action: action, Interface: operator, Targets: []string{"one"}, AttachmentURL: "https://media.example/item.png", ForceNSFW: true})
				if (err == nil) != expected {
					t.Fatal(source, action, operator, err)
				}
			}
		}
	}
	if _, err := p.PlanChannelSend(SendRequest{Content: "text", All: true, ForceNSFW: true}); err == nil {
		t.Fatal("bypass without image accepted")
	}
}
func TestNamedTextIntentRestrictions(t *testing.T) {
	valid := SendRequest{Action: "communications.send", Targets: []string{"whatsapp:friend"}, Content: " hello "}
	plan, err := ValidateConversationSend(valid)
	if err != nil || plan.Content != "hello" {
		t.Fatal(plan, err)
	}
	for _, change := range []func(*SendRequest){
		func(r *SendRequest) { r.Action = "discord.send" }, func(r *SendRequest) { r.All = true },
		func(r *SendRequest) { r.Targets = []string{"one"} },
		func(r *SendRequest) { r.Targets = []string{"whatsapp:friend", "one"} },
		func(r *SendRequest) { r.AttachmentURL = "https://media.example/item.png" },
		func(r *SendRequest) { r.ForceNSFW = true }, func(r *SendRequest) { r.Content = " " },
	} {
		r := valid
		change(&r)
		if _, err = ValidateConversationSend(r); err == nil {
			t.Fatal("accepted invalid named send", r)
		}
	}
	if name, err := NamedConversation("whatsapp:friend", []Conversation{{Alias: "friend"}}); err != nil || name != "friend" {
		t.Fatal(name, err)
	}
	if _, err := NamedConversation("whatsapp:unknown", []Conversation{{Alias: "friend"}}); err == nil {
		t.Fatal("unknown alias accepted")
	}
}

func TestConversationAuthorizationRespectsRoutingState(t *testing.T) {
	p := DeliveryPolicy{Disabled: map[string]bool{"whatsapp": true}}
	if err := p.AuthorizeConversation("whatsapp:friend"); err == nil || !strings.Contains(err.Error(), "switched off") {
		t.Fatalf("disabled conversation was authorized: %v", err)
	}
	p.Disabled["whatsapp"] = false
	if err := p.AuthorizeConversation("whatsapp:friend"); err != nil {
		t.Fatal(err)
	}
	if err := p.AuthorizeConversation("discord-channel"); err == nil {
		t.Fatal("a channel was authorized as a conversation")
	}
}

func TestManualTargetsAreForOneExplicitSendAndNeverJoinAll(t *testing.T) {
	policy := DeliveryPolicy{Channels: []string{"meme"}, ManualTargets: []string{"chat"}}
	if plan, err := policy.PlanChannelSend(SendRequest{Content: "hi", Targets: []string{"chat"}}); err != nil || !reflect.DeepEqual(plan.Targets, []string{"chat"}) {
		t.Fatal(plan, err)
	}
	plan, err := policy.PlanChannelSend(SendRequest{Content: "meme", All: true, Targets: []string{"chat"}})
	if err != nil || !reflect.DeepEqual(plan.Targets, []string{"meme"}) {
		t.Fatal("a scheduled or 'all' delivery goes only to the delivery channels", plan, err)
	}
}
