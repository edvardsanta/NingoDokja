package handlers

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"read_books/internal/core"
)

func listeningHandler(memory *recordingMemory, enabled func() bool) *CommunicationsDomainHandler {
	h := NewCommunicationsDomainHandler(nil, "").WithMusic(&fakeMusicSources{}).WithMusicExperience(memory, enabled)
	h.music.listens.spawn = func(work func()) { work() }
	return h
}

func doMusicAs(t *testing.T, h *CommunicationsDomainHandler, person, action, content string) map[string]any {
	t.Helper()
	event := musicEvent("music.x", content, "guild")
	event.User.ID = person
	result, err := h.Handle(context.Background(), event, core.WorkflowStep{Domain: core.DomainCommunications, Action: action})
	if err != nil {
		t.Fatal(err)
	}
	return result["music"].(map[string]any)
}

func TestEachEndingIsNotedForWhoAskedNotWhoPressed(t *testing.T) {
	memory := &recordingMemory{answers: map[string]map[string]any{"memory.record": {"created": true}, "memory.resolve": {"found": true, "resolved": true}}}
	h := listeningHandler(memory, nil)
	doMusicAs(t, h, "1", "play-music", "one")
	doMusicAs(t, h, "2", "play-music", "two")
	doMusicAs(t, h, "3", "play-music", "three")

	doMusicAs(t, h, "1", "end-music-track", "1")
	doMusicAs(t, h, "9", "skip-music", "")
	doMusicAs(t, h, "9", "stop-music", "")

	calls := memory.sent()
	if len(calls) != 6 {
		t.Fatalf("each song recorded and settled, got %#v", calls)
	}
	want := []struct{ action, detail, outcome string }{
		{"music.listen:1", "finished", "accepted"},
		{"music.listen:2", "skipped", "replaced"},
		{"music.listen:3", "stopped", "ignored"},
	}
	for i, song := range want {
		record, resolve := calls[2*i], calls[2*i+1]
		if record.eventType != "memory.record" || record.payload["action"] != song.action || record.payload["detail"] != song.detail {
			t.Fatalf("%d: %#v", i, record)
		}
		if resolve.eventType != "memory.resolve" || resolve.payload["ref"] != record.payload["ref"] || resolve.payload["outcome"] != song.outcome {
			t.Fatalf("%d: %#v", i, resolve)
		}
	}
}

func TestOnlyASongThatPlayedIsNoted(t *testing.T) {
	memory := &recordingMemory{}
	h := listeningHandler(memory, nil)
	doMusicAs(t, h, "1", "stop-music", "")
	doMusicAs(t, h, "1", "skip-music", "")
	doMusicAs(t, h, "1", "play-music", "one")
	doMusicAs(t, h, "1", "play-music", "waiting")
	doMusicAs(t, h, "1", "end-music-track", strconv.Itoa(7))
	if calls := memory.sent(); len(calls) != 0 {
		t.Fatalf("nothing ended: %#v", calls)
	}
	doMusicAs(t, h, "1", "end-music-track", "1")
	doMusicAs(t, h, "1", "end-music-track", "1")
	if calls := memory.sent(); len(calls) != 2 {
		t.Fatalf("a song leaves once, and the one waiting has not played: %#v", calls)
	}
}

func TestMemoryNeverTouchesTheMusic(t *testing.T) {
	failing := &recordingMemory{errors: map[string]error{"memory.record": errors.New("memory is down")}}
	h := listeningHandler(failing, nil)
	doMusicAs(t, h, "1", "play-music", "one")
	if reply := doMusicAs(t, h, "1", "skip-music", ""); reply["ok"] != true {
		t.Fatal(reply)
	}
	off := &recordingMemory{}
	switched := listeningHandler(off, func() bool { return false })
	doMusicAs(t, switched, "1", "play-music", "one")
	doMusicAs(t, switched, "1", "stop-music", "")
	if calls := off.sent(); len(calls) != 0 {
		t.Fatalf("the operator's switch: %#v", calls)
	}
	if reply := doMusicAs(t, switched, "1", "show-music-history", ""); reply["ok"] != false || reply["code"] != "no_memory" {
		t.Fatal(reply)
	}
	plain := NewCommunicationsDomainHandler(nil, "").WithMusic(&fakeMusicSources{})
	doMusicAs(t, plain, "1", "play-music", "one")
	if reply := doMusicAs(t, plain, "1", "stop-music", ""); reply["ok"] != true {
		t.Fatal(reply)
	}
}

func TestAHistoryShowsOnlyTheSongsOfWhoeverAsks(t *testing.T) {
	memory := &recordingMemory{answers: map[string]map[string]any{"memory.list": {"experiences": []any{
		map[string]any{"detail": "finished", "context_snippet": "One – Band"},
	}}}}
	h := listeningHandler(memory, nil)
	reply := doMusicAs(t, h, "42", "show-music-history", "music.listen:7")
	history := reply["history"].([]any)
	if len(history) != 1 || history[0].(map[string]any)["song"] != "One – Band" || history[0].(map[string]any)["ended"] != "finished" {
		t.Fatal(reply)
	}
	calls := memory.sent()
	if len(calls) != 1 || calls[0].payload["action"] != "music.listen:42" {
		t.Fatalf("what is typed never picks the person: %#v", calls)
	}
	if reply := doMusicAs(t, h, "someone", "show-music-history", ""); reply["ok"] != false {
		t.Fatal("no id, no history", reply)
	}
}
