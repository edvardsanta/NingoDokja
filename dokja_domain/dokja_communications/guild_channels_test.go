package communications

import (
	"context"
	"errors"
	"testing"
)

// guildService answers the service's server actions from fixed facts and records each action.
type guildService struct {
	actions []string
	failAll bool
}

func (g *guildService) Dispatch(_ context.Context, action string, payload map[string]any) (map[string]any, error) {
	g.actions = append(g.actions, action)
	if g.failAll {
		return nil, errors.New("communications service is unavailable")
	}
	switch action {
	case "guild_channels":
		return map[string]any{
			"guilds": []any{
				map[string]any{"id": "10", "name": "resenhudos", "channels": []any{
					map[string]any{"id": "11", "name": "chat-geral"},
					map[string]any{"id": "12", "name": "musicas"},
					map[string]any{"id": "../x", "name": "bad"},
				}},
				map[string]any{"id": "99", "name": "not configured", "channels": []any{map[string]any{"id": "98", "name": "x"}}},
			},
			"unavailable_guilds": []any{"20"},
		}, nil
	case "channels":
		return map[string]any{"channels": []any{map[string]any{"id": "500", "name": "elsewhere"}}}, nil
	case "channel_info":
		facts := map[string]map[string]any{
			"11": {"guild_id": "10", "text": true},
			"13": {"guild_id": "10", "text": false},
			"98": {"guild_id": "99", "text": true},
		}
		return facts[payload["channel_id"].(string)], nil
	}
	return map[string]any{"ok": true}, nil
}

func TestChannelsAreListedPerServerWithTheConfiguredOnesKept(t *testing.T) {
	service := &guildService{}
	d := New(service, "11,500").WithGuilds("10, 20")
	result, err := d.Handle(context.Background(), ActionChannels, nil)
	if err != nil {
		t.Fatal(err)
	}
	channels := result["channels"].([]any)
	if len(channels) != 3 {
		t.Fatal(channels)
	}
	first := channels[0].(map[string]any)
	if first["id"] != "11" || first["guild_id"] != "10" || first["guild_name"] != "resenhudos" {
		t.Fatal(first)
	}
	if last := channels[2].(map[string]any); last["id"] != "500" || last["guild_id"] != nil {
		t.Fatal("a configured channel on no configured server is still listed, without a server", last)
	}
	if unavailable := result["unavailable_guilds"].([]any); len(unavailable) != 1 || unavailable[0] != "20" {
		t.Fatal(result)
	}
}

func TestAServerChannelIsReadableOnlyWhenItHoldsMessagesOnAConfiguredServer(t *testing.T) {
	d := New(&guildService{}, "500").WithGuilds("10")
	for channel, want := range map[string]bool{"500": true, "11": true, "13": false, "98": false, "x1": false} {
		if got, err := d.Readable(context.Background(), channel); err != nil || got != want {
			t.Fatal(channel, got, err)
		}
	}
	if _, err := d.Handle(context.Background(), ActionHistory, map[string]any{"channel_id": "98"}); err == nil {
		t.Fatal("history of another server's channel")
	}
	if _, err := d.Handle(context.Background(), ActionHistory, map[string]any{"channel_id": "11"}); err != nil {
		t.Fatal(err)
	}
}

func TestWithoutAServerOnlyTheConfiguredChannelsCount(t *testing.T) {
	service := &guildService{}
	d := New(service, "500").WithGuilds("")
	if ok, _ := d.Readable(context.Background(), "11"); ok {
		t.Fatal("no server configured must not mean every server")
	}
	result, err := d.Handle(context.Background(), ActionChannels, nil)
	if err != nil || len(result["channels"].([]any)) != 1 {
		t.Fatal(result, err)
	}
	for _, action := range service.actions {
		if action == "guild_channels" || action == "channel_info" {
			t.Fatal("asked Discord about servers nobody configured:", service.actions)
		}
	}
}

func TestAnUnreachableServiceStillFailsTheListAsBefore(t *testing.T) {
	d := New(&guildService{failAll: true}, "500").WithGuilds("10")
	if _, err := d.Handle(context.Background(), ActionChannels, nil); err == nil {
		t.Fatal("nothing to show and the service is down: the error stands")
	}
}
