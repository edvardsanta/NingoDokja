package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func statusResult(schedulerOn, discordOn bool, jobs ...map[string]any) any {
	rows := make([]any, len(jobs))
	for i, job := range jobs {
		rows[i] = job
	}
	return map[string]any{"result": map[string]any{"system": map[string]any{
		"services": map[string]any{
			"scheduler": map[string]any{"status": "ok", "enabled": schedulerOn},
			"discord":   map[string]any{"kind": "interface", "enabled": discordOn},
		},
		"jobs":     rows,
		"channels": map[string]any{"meme": []any{"a", "b"}},
	}}}
}

func TestSchedulerNoticeNamesTheJobsThatWillPost(t *testing.T) {
	status := statusResult(true, true,
		map[string]any{"name": "meme.dispatch", "enabled": true, "interval": "2h0m0s", "next_at": "2026-10-21T18:00:00Z"},
		map[string]any{"name": "meme.refresh", "enabled": false, "interval": "45m0s"})
	got := strings.Join(DeliveryNotice("scheduler", status, nil), "\n")
	if !strings.Contains(got, "meme.dispatch runs every 2h0m0s, next at 2026-10-21T18:00:00Z: posts memes to 2 Discord channel(s)") || strings.Contains(got, "meme.refresh") {
		t.Fatal(got)
	}
}

func TestSchedulerNoticeWhenNothingWillRun(t *testing.T) {
	if got := DeliveryNotice("scheduler", statusResult(false, true), nil); !strings.Contains(got[0], "switch is off") {
		t.Fatal(got)
	}
	paused := statusResult(true, true, map[string]any{"name": "meme.dispatch", "enabled": false})
	if got := DeliveryNotice("scheduler", paused, nil); !strings.Contains(got[0], "paused") {
		t.Fatal(got)
	}
}

func TestDiscordNoticeFollowsItsSwitch(t *testing.T) {
	if got := strings.Join(DeliveryNotice("discord", statusResult(true, true), nil), " "); !strings.Contains(got, "answers mentions") {
		t.Fatal(got)
	}
	if got := strings.Join(DeliveryNotice("discord", statusResult(true, false), nil), " "); !strings.Contains(got, "switch is off") {
		t.Fatal(got)
	}
}

func TestAnUnreadableStatusSaysTheSavedSettingsApply(t *testing.T) {
	got := DeliveryNotice("scheduler", nil, errors.New("timeout"))
	if len(got) != 1 || !strings.Contains(got[0], "already saved") {
		t.Fatal(got)
	}
}

func TestOnlyAnExplicitYesConfirms(t *testing.T) {
	for answer, want := range map[string]bool{"y\n": true, "sim\n": true, "\n": false, "": false, "no\n": false} {
		var out bytes.Buffer
		if Confirm(strings.NewReader(answer), &out, "Start?", []string{"line"}) != want {
			t.Fatalf("%q", answer)
		}
		if !strings.Contains(out.String(), "line") {
			t.Fatal(out.String())
		}
	}
}
