package handlers

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	communications "read_books/dokja_domain/dokja_communications"
	"read_books/internal/core"
)

// fakeMusicSources is one playable source that finds a song named after the words, counts its
// searches, can fail, and can hold a search until released.
type fakeMusicSources struct {
	searches int
	fail     error
	hold     chan struct{}
	started  chan struct{}
}

func (f *fakeMusicSources) AudioProviders(context.Context) ([]string, error) {
	if f.fail != nil {
		return nil, f.fail
	}
	return []string{"10-music"}, nil
}

func (f *fakeMusicSources) InvokeAudio(ctx context.Context, _, action string, input map[string]any) (map[string]any, error) {
	if action != "search" {
		return map[string]any{"handled": false}, nil
	}
	f.searches++
	if f.started != nil {
		f.started <- struct{}{}
	}
	if f.hold != nil {
		select {
		case <-f.hold:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	query, _ := input["query"].(string)
	if query == "nothing" {
		return map[string]any{"handled": true, "tracks": []any{}}, nil
	}
	return map[string]any{"handled": true, "tracks": []any{map[string]any{
		"id": query, "url": "https://play.example/" + query, "title": query, "uploader": "Band", "duration_s": 200.0, "playable": true,
	}}}, nil
}

func musicEvent(eventType, content, guild string) core.Event {
	event := core.Event{Type: eventType, User: core.EventUser{ID: "person"}, Payload: map[string]any{"content": content}, Context: map[string]any{}}
	if guild != "" {
		event.Context["guild_id"] = guild
	}
	return event
}

func doMusic(t *testing.T, h *CommunicationsDomainHandler, action, content, guild string) map[string]any {
	t.Helper()
	result, err := h.Handle(context.Background(), musicEvent("music.x", content, guild), core.WorkflowStep{Domain: core.DomainCommunications, Action: action})
	if err != nil {
		t.Fatal(err)
	}
	return result["music"].(map[string]any)
}

func musicHandler(sources communications.AudioSources) *CommunicationsDomainHandler {
	return NewCommunicationsDomainHandler(nil, "").WithMusic(sources)
}

func TestPlayQueuesUpToThreeAndRefusesTheFourthWithoutSearching(t *testing.T) {
	sources := &fakeMusicSources{}
	h := musicHandler(sources)
	for place, words := range []string{"one", "two", "three", "four"} {
		reply := doMusic(t, h, "play-music", words, "guild")
		if reply["ok"] != true || reply["place"] != place || reply["added"].(map[string]any)["title"] != words {
			t.Fatal(words, reply)
		}
	}
	reply := doMusic(t, h, "play-music", "five", "guild")
	if reply["ok"] != false || !strings.Contains(reply["reason"].(string), "full") || sources.searches != 4 {
		t.Fatal("a full queue refuses before searching", reply, sources.searches)
	}
	queue := doMusic(t, h, "show-music-queue", "", "guild")
	if queue["now_playing"].(map[string]any)["title"] != "one" || len(queue["waiting"].([]any)) != 3 {
		t.Fatal(queue)
	}
	if other := doMusic(t, h, "show-music-queue", "", "other-guild"); other["now_playing"] != nil {
		t.Fatal("each server has its own queue", other)
	}
}

func TestQueueControlsAndTheirRefusals(t *testing.T) {
	h := musicHandler(&fakeMusicSources{})
	if reply := doMusic(t, h, "skip-music", "", "guild"); reply["ok"] != false || reply["reason"] != "nothing is playing" {
		t.Fatal(reply)
	}
	doMusic(t, h, "play-music", "one", "guild")
	doMusic(t, h, "play-music", "two", "guild")
	if reply := doMusic(t, h, "pause-music", "", "guild"); reply["paused"] != true {
		t.Fatal(reply)
	}
	if reply := doMusic(t, h, "resume-music", "", "guild"); reply["paused"] != false {
		t.Fatal(reply)
	}
	if reply := doMusic(t, h, "set-music-volume", "40%", "guild"); reply["volume"] != 40 {
		t.Fatal(reply)
	}
	if reply := doMusic(t, h, "set-music-volume", "loud", "guild"); reply["ok"] != false {
		t.Fatal(reply)
	}
	if reply := doMusic(t, h, "skip-music", "", "guild"); reply["now_playing"].(map[string]any)["title"] != "two" {
		t.Fatal(reply)
	}
	if reply := doMusic(t, h, "stop-music", "", "guild"); reply["now_playing"] != nil || reply["volume"] != 40 {
		t.Fatal(reply)
	}
}

func TestATrackEndedReportAdvancesOnlyForTheTrackPlaying(t *testing.T) {
	h := musicHandler(&fakeMusicSources{})
	first := doMusic(t, h, "play-music", "one", "guild")["added"].(map[string]any)["entry"].(int)
	doMusic(t, h, "play-music", "two", "guild")
	ended := func(entry int) map[string]any {
		event := musicEvent("music.ended", "", "guild")
		event.Payload["entry"] = float64(entry)
		result, err := h.Handle(context.Background(), event, core.WorkflowStep{Action: "end-music-track"})
		if err != nil {
			t.Fatal(err)
		}
		return result["music"].(map[string]any)
	}
	if reply := ended(first + 1); reply["advanced"] != false {
		t.Fatal("a report about a waiting track changes nothing", reply)
	}
	if reply := ended(first); reply["advanced"] != true || reply["now_playing"].(map[string]any)["title"] != "two" {
		t.Fatal(reply)
	}
}

func TestPlayRefusalsAreAnswersAndBrokenSourcesAreErrors(t *testing.T) {
	h := musicHandler(&fakeMusicSources{})
	for content, guild := range map[string]string{"one": "", "": "guild", "nothing": "guild"} {
		if reply := doMusic(t, h, "play-music", content, guild); reply["ok"] != false || reply["reason"] == "" {
			t.Fatal(content, guild, reply)
		}
	}
	broken := musicHandler(&fakeMusicSources{fail: errors.New("feeds down")})
	if _, err := broken.Handle(context.Background(), musicEvent("music.play", "one", "guild"), core.WorkflowStep{Action: "play-music"}); err == nil {
		t.Fatal("a source that cannot be reached is an error, not a refusal")
	}
	if reply := doMusic(t, NewCommunicationsDomainHandler(nil, ""), "play-music", "one", "guild"); reply["reason"] != "music is not set up" {
		t.Fatal(reply)
	}
}

func TestASlowSearchDoesNotHoldTheQueue(t *testing.T) {
	sources := &fakeMusicSources{}
	h := musicHandler(sources)
	doMusic(t, h, "play-music", "one", "guild")
	sources.hold, sources.started = make(chan struct{}), make(chan struct{}, 1)
	// The goroutines only hand their results back; the test checks them here.
	run := func(action, content string) chan map[string]any {
		out := make(chan map[string]any, 1)
		go func() {
			result, _ := h.Handle(context.Background(), musicEvent("music.x", content, "guild"), core.WorkflowStep{Action: action})
			music, _ := result["music"].(map[string]any)
			out <- music
		}()
		return out
	}
	done := run("play-music", "two")
	<-sources.started
	paused := run("pause-music", "")
	select {
	case reply := <-paused:
		if reply["paused"] != true {
			t.Fatal(reply)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pause waited for a search")
	}
	close(sources.hold)
	if reply := <-done; reply["place"] != 1 {
		t.Fatal(reply)
	}
}

func TestAnEndedReportFromTheBridgeAdvancesAndRepliesCarryEpochAndVersion(t *testing.T) {
	h := musicHandler(&fakeMusicSources{})
	first := doMusic(t, h, "play-music", "one", "guild")
	doMusic(t, h, "play-music", "two", "guild")
	entry := first["added"].(map[string]any)["entry"].(int)
	reply := doMusic(t, h, "end-music-track", strconv.Itoa(entry), "guild")
	if reply["advanced"] != true || reply["now_playing"].(map[string]any)["title"] != "two" {
		t.Fatal(reply)
	}
	if reply["epoch"] == "" || reply["epoch"] != first["epoch"] || reply["version"].(int) <= first["version"].(int) {
		t.Fatal("one process keeps its epoch, and every change raises the version", first, reply)
	}
	if other := musicHandler(&fakeMusicSources{}); doMusic(t, other, "show-music-queue", "", "guild")["epoch"] == first["epoch"] {
		t.Fatal("another process (a restart) has another epoch")
	}
}

func TestEveryMusicRefusalCarriesACodeForTheInterfaceToSayInItsLanguage(t *testing.T) {
	h := musicHandler(&fakeMusicSources{})
	for _, check := range []struct{ action, content, guild, code string }{
		{"play-music", "one", "", "no_server"},
		{"play-music", "", "guild", "empty_request"},
		{"play-music", "nothing", "guild", "not_found"},
		{"skip-music", "", "guild", "nothing_playing"},
		{"set-music-volume", "loud", "guild", "bad_volume"},
	} {
		if reply := doMusic(t, h, check.action, check.content, check.guild); reply["ok"] != false || reply["code"] != check.code || reply["reason"] == "" {
			t.Fatal(check, reply)
		}
	}
	for range 4 {
		doMusic(t, h, "play-music", "song", "full")
	}
	if reply := doMusic(t, h, "play-music", "song", "full"); reply["code"] != "queue_full" {
		t.Fatal(reply)
	}
	if reply := doMusic(t, NewCommunicationsDomainHandler(nil, ""), "play-music", "x", "guild"); reply["code"] != "not_set_up" {
		t.Fatal(reply)
	}
	refused := refusedFor(fmt.Errorf("%w: only track links are supported, not albums", communications.ErrTrackRefused))["music"].(map[string]any)
	if refused["code"] != "track_refused" || refused["reason"] != "only track links are supported, not albums" {
		t.Fatal("a source's refusal keeps only its own words", refused)
	}
}
