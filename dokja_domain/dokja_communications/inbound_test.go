package communications

import "testing"

func TestInboundDiscordEligibility(t *testing.T) {
	p := InboundPolicy{DiscordGuildIDs: []string{"guild"}, DiscordChannels: []string{"reading"}, DiscordDMOpen: true, DiscordNeedMention: true}
	base := InboundIntent{Platform: "discord", Entry: "message", ChannelID: "reading", GuildID: "guild", Mentioned: true}
	if !p.AuthorizeInbound(base) {
		t.Fatal("allowed guild message refused")
	}
	for name, change := range map[string]func(*InboundIntent){
		"guild":         func(r *InboundIntent) { r.GuildID = "other" },
		"channel":       func(r *InboundIntent) { r.ChannelID = "other" },
		"empty channel": func(r *InboundIntent) { r.ChannelID = "" },
		"mention":       func(r *InboundIntent) { r.Mentioned = false },
		"entry":         func(r *InboundIntent) { r.Entry = "unknown" },
	} {
		t.Run(name, func(t *testing.T) {
			r := base
			change(&r)
			if p.AuthorizeInbound(r) {
				t.Fatal("disallowed message accepted")
			}
		})
	}
	command := base
	command.Entry, command.Mentioned = "command", false
	if !p.AuthorizeInbound(command) {
		t.Fatal("command incorrectly requires mention")
	}
	direct := base
	direct.Direct, direct.GuildID, direct.ChannelID = true, "", "private"
	if !p.AuthorizeInbound(direct) {
		t.Fatal("open DM refused")
	}
	p.DiscordDMOpen = false
	if p.AuthorizeInbound(direct) {
		t.Fatal("closed DM accepted")
	}
}

func TestInboundWhatsAppEligibility(t *testing.T) {
	p := InboundPolicy{WhatsAppSelfChat: true, WhatsAppNumbers: []string{"5511987654321"}, WhatsAppGroups: []string{"120363000000000001@g.us"}}
	base := InboundIntent{Platform: "whatsapp", Entry: "message", ChatKind: "direct", ChannelID: "friend@s.whatsapp.net", SenderNumbers: []string{"551187654321"}}
	if !p.AuthorizeInbound(base) {
		t.Fatal("equivalent mobile spelling refused")
	}
	base.SenderNumbers = nil
	if p.AuthorizeInbound(base) {
		t.Fatal("anonymous direct message accepted")
	}
	base.SelfChat = true
	if !p.AuthorizeInbound(base) {
		t.Fatal("self chat refused")
	}
	p.WhatsAppSelfChat = false
	if p.AuthorizeInbound(base) {
		t.Fatal("disabled self chat accepted")
	}
	group := InboundIntent{Platform: "whatsapp", Entry: "message", ChatKind: "group", ChannelID: p.WhatsAppGroups[0], Mentioned: true}
	if !p.AuthorizeInbound(group) {
		t.Fatal("allowed mentioned group refused")
	}
	group.Mentioned = false
	if p.AuthorizeInbound(group) {
		t.Fatal("unmentioned group accepted")
	}
	group.Mentioned, group.ChannelID = true, "other@g.us"
	if p.AuthorizeInbound(group) {
		t.Fatal("unlisted group accepted")
	}
	group.ChatKind = "other"
	if p.AuthorizeInbound(group) {
		t.Fatal("other chat kind accepted")
	}
}

func TestParseInboundPolicyNormalizesAndRefusesInvalidConfiguration(t *testing.T) {
	p, err := ParseInboundPolicy(map[string]string{
		"DISCORD_ALLOWED_CHANNELS":  " reading, memes ",
		"DISCORD_DM_POLICY":         "open",
		"DOKJA_WHATSAPP_SELF_CHAT":  "FALSE",
		"DOKJA_WHATSAPP_ALLOW_FROM": "+55 (11) 98765-4321",
	})
	if err != nil || !p.DiscordDMOpen || len(p.DiscordChannels) != 2 || len(p.WhatsAppNumbers) != 1 || p.WhatsAppNumbers[0] != "5511987654321" || p.WhatsAppSelfChat {
		t.Fatalf("unexpected parsed policy %#v: %v", p, err)
	}
	for _, values := range []map[string]string{
		{"DOKJA_WHATSAPP_ALLOW_FROM": "not-a-number"},
		{"DOKJA_WHATSAPP_ALLOWED_GROUPS": "not-a-group"},
		{"DOKJA_WHATSAPP_SELF_CHAT": "perhaps"},
		{"DISCORD_REQUIRE_MENTION": "perhaps"},
	} {
		if _, err := ParseInboundPolicy(values); err == nil {
			t.Fatalf("accepted invalid policy %#v", values)
		}
	}
}

func TestInboundDiscordAcceptsEveryConfiguredServer(t *testing.T) {
	p, err := ParseInboundPolicy(map[string]string{"DISCORD_GUILD_ID": " first , second ", "DISCORD_ALLOWED_CHANNELS": "a,b"})
	if err != nil {
		t.Fatal(err)
	}
	for guild, want := range map[string]bool{"first": true, "second": true, "third": false} {
		intent := InboundIntent{Platform: "discord", Entry: "command", ChannelID: "b", GuildID: guild}
		if p.AuthorizeInbound(intent) != want {
			t.Fatal(guild)
		}
	}
	single, err := ParseInboundPolicy(map[string]string{"DISCORD_GUILD_ID": "only"})
	if err != nil || len(single.DiscordGuildIDs) != 1 || single.DiscordGuildIDs[0] != "only" {
		t.Fatal("one server, as before", single, err)
	}
	if !(InboundPolicy{}).AuthorizeInbound(InboundIntent{Platform: "discord", Entry: "command", ChannelID: "a", GuildID: "any"}) {
		t.Fatal("no servers configured means any server")
	}
}
