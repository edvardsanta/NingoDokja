package communications

import (
	"errors"
	"testing"
)

func song(id string) Track {
	return Track{ID: id, Title: id, URL: "https://play.example/" + id, Playable: true}
}

func TestTheFirstTrackPlaysAndAtMostThreeWait(t *testing.T) {
	q := NewMusicQueue()
	for i, id := range []string{"a", "b", "c", "d"} {
		queued, place, err := q.Add(song(id), "someone")
		if err != nil || place != i || queued.Entry != i+1 {
			t.Fatal(id, place, queued, err)
		}
	}
	if !q.Full() {
		t.Fatal("three waiting behind the one playing is full")
	}
	if _, _, err := q.Add(song("e"), "someone"); !errors.Is(err, ErrQueueFull) {
		t.Fatal(err)
	}
	state := q.State()
	if state.NowPlaying.ID != "a" || len(state.Waiting) != 3 || state.Waiting[2].ID != "d" || state.Volume != MaxVolume {
		t.Fatal(state)
	}
}

func TestSkipAndEndedAdvanceOnlyForTheTrackPlaying(t *testing.T) {
	q := NewMusicQueue()
	first, _, _ := q.Add(song("same"), "a")
	second, _, _ := q.Add(song("same"), "b")
	if next, advanced := q.Ended(second.Entry); advanced || next != nil || q.State().NowPlaying.Entry != first.Entry {
		t.Fatal("a report about the waiting play of the same song must not advance the queue")
	}
	next, advanced := q.Ended(first.Entry)
	if !advanced || next.Entry != second.Entry {
		t.Fatal(next, advanced)
	}
	if _, advanced = q.Ended(first.Entry); advanced {
		t.Fatal("a repeated report must not advance again")
	}
	if next, err := q.Skip(); err != nil || next != nil || q.Active() {
		t.Fatal("skipping the last track leaves nothing playing", next, err)
	}
	if _, err := q.Skip(); !errors.Is(err, ErrNothingPlaying) {
		t.Fatal(err)
	}
}

func TestPauseResumeStopAndVolume(t *testing.T) {
	q := NewMusicQueue()
	if err := q.Pause(); !errors.Is(err, ErrNothingPlaying) {
		t.Fatal(err)
	}
	q.Add(song("a"), "x")
	q.Add(song("b"), "x")
	if err := q.Pause(); err != nil || !q.State().Paused {
		t.Fatal(err)
	}
	if next, _ := q.Skip(); next.ID != "b" || q.State().Paused {
		t.Fatal("the next track starts playing, not paused")
	}
	if err := q.SetVolume(40); err != nil {
		t.Fatal(err)
	}
	for _, volume := range []int{-1, 101} {
		if err := q.SetVolume(volume); !errors.Is(err, ErrBadVolume) {
			t.Fatal(volume, err)
		}
	}
	q.Stop()
	if state := q.State(); state.NowPlaying != nil || len(state.Waiting) != 0 || state.Volume != 40 || q.Active() {
		t.Fatal("stop empties the queue and keeps the volume", state)
	}
}

func TestStateIsACopy(t *testing.T) {
	q := NewMusicQueue()
	q.Add(song("a"), "x")
	q.Add(song("b"), "x")
	state := q.State()
	state.NowPlaying.ID, state.Waiting[0].ID = "changed", "changed"
	if again := q.State(); again.NowPlaying.ID != "a" || again.Waiting[0].ID != "b" {
		t.Fatal("state aliases the queue")
	}
}

func TestWhatWasTypedAfterPlay(t *testing.T) {
	for text, want := range map[string]TrackRequest{
		"  monalisa   jorge ":               {Query: "monalisa jorge"},
		"https://files.example/a.mp3":       {URL: "https://files.example/a.mp3"},
		"https://files.example/a.mp3 extra": {Query: "https://files.example/a.mp3 extra"},
		"ftp://files.example/a.mp3":         {Query: "ftp://files.example/a.mp3"},
	} {
		if got := TrackRequestFromText(text); got != want {
			t.Fatal(text, got)
		}
	}
	for text, want := range map[string]int{"40": 40, " 75% ": 75} {
		if got, err := VolumeFromText(text); err != nil || got != want {
			t.Fatal(text, got, err)
		}
	}
	if _, err := VolumeFromText("loud"); !errors.Is(err, ErrBadVolume) {
		t.Fatal(err)
	}
}

func TestEveryChangeRaisesTheVersionAndARefusalDoesNot(t *testing.T) {
	q := NewMusicQueue()
	last := q.State().Version
	step := func(name string, change func() error) {
		t.Helper()
		if err := change(); err != nil {
			t.Fatal(name, err)
		}
		if now := q.State().Version; now <= last {
			t.Fatal(name, "did not raise the version", now, last)
		} else {
			last = now
		}
	}
	step("add", func() error { _, _, err := q.Add(song("a"), "x"); return err })
	step("add again", func() error { _, _, err := q.Add(song("b"), "x"); return err })
	step("pause", q.Pause)
	step("resume", q.Resume)
	step("volume", func() error { return q.SetVolume(50) })
	step("skip", func() error { _, err := q.Skip(); return err })
	step("ended", func() error { q.Ended(q.State().NowPlaying.Entry); return nil })
	step("stop", func() error { q.Stop(); return nil })
	if err := q.Pause(); err == nil || q.State().Version != last {
		t.Fatal("a refused change must not raise the version")
	}
	if _, advanced := q.Ended(999); advanced || q.State().Version != last {
		t.Fatal("an ignored report must not raise the version")
	}
}
