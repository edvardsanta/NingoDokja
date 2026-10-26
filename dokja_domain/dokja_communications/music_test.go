package communications

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// stubSources answers each source's actions from a table, records what was asked, and fails a
// source listed in broken.
type stubSources struct {
	order   []string
	replies map[string]map[string]any
	broken  map[string]bool
	asked   []string
}

func (s *stubSources) AudioProviders(context.Context) ([]string, error) { return s.order, nil }

func (s *stubSources) InvokeAudio(_ context.Context, plugin, action string, input map[string]any) (map[string]any, error) {
	s.asked = append(s.asked, plugin+" "+action)
	if s.broken[plugin] {
		return nil, errors.New("plugin crashed")
	}
	if reply, ok := s.replies[plugin+" "+action]; ok {
		return reply, nil
	}
	return map[string]any{"handled": false}, nil
}

func playable(id, title, uploader string, duration float64) map[string]any {
	return map[string]any{"id": id, "url": "https://play.example/" + id, "title": title, "uploader": uploader, "duration_s": duration, "playable": true}
}

func tracks(items ...map[string]any) map[string]any {
	list := make([]any, len(items))
	for i, item := range items {
		list[i] = item
	}
	return map[string]any{"handled": true, "tracks": list}
}

func TestTypedWordsPlayTheFirstSongOfTheFirstSourceThatFindsAny(t *testing.T) {
	sources := &stubSources{
		order: []string{"10-catalogue", "20-music", "30-pages"},
		replies: map[string]map[string]any{
			"20-music search": tracks(playable("mona1", "Monalisa", "Jorge Vercillo", 250)),
			"30-pages search": tracks(playable("video1", "Monalisa (clipe)", "Someone", 260)),
		},
	}
	track, err := ResolveTrack(context.Background(), sources, TrackRequest{Query: "  monalisa   jorge "})
	if err != nil || track.ID != "mona1" || !track.Playable {
		t.Fatal(track, err)
	}
	if !reflect.DeepEqual(sources.asked, []string{"10-catalogue search", "20-music search"}) {
		t.Fatal("sources after the first answer must not be asked:", sources.asked)
	}
}

func TestABrokenSourceIsSkippedAndNamedWhenNothingElseFinds(t *testing.T) {
	sources := &stubSources{
		order:   []string{"10-music", "20-pages"},
		broken:  map[string]bool{"10-music": true},
		replies: map[string]map[string]any{"20-pages search": tracks(playable("v1", "Monalisa", "Jorge", 250))},
	}
	if track, err := ResolveTrack(context.Background(), sources, TrackRequest{Query: "monalisa"}); err != nil || track.ID != "v1" {
		t.Fatal(track, err)
	}
	sources.replies = nil
	_, err := ResolveTrack(context.Background(), sources, TrackRequest{Query: "monalisa"})
	if !errors.Is(err, ErrTrackNotFound) || !strings.Contains(err.Error(), "10-music") {
		t.Fatal(err)
	}
}

func TestALinkPlaysFromTheSourceThatRecognizesIt(t *testing.T) {
	sources := &stubSources{
		order:   []string{"10-catalogue", "30-pages"},
		replies: map[string]map[string]any{"30-pages read": tracks(playable("p1", "A file", "", 60))},
	}
	track, err := ResolveTrack(context.Background(), sources, TrackRequest{URL: "https://files.example/song.mp3"})
	if err != nil || track.ID != "p1" || track.RequestedURL != "https://files.example/song.mp3" {
		t.Fatal(track, err)
	}
}

// Regression: a catalogue credits "Antônio Carlos Jobim" and the playable source "Tom Jobim".
// The same title at a length one second apart is the same recording; a cover and a song of
// another length are not.
func TestACatalogueLinkFindsTheSameRecordingUnderAnotherArtistName(t *testing.T) {
	link := "https://catalogue.example/track/1"
	sources := &stubSources{
		order: []string{"10-catalogue", "20-music"},
		replies: map[string]map[string]any{
			"10-catalogue read": tracks(map[string]any{"title": "Garota de Ipanema", "uploader": "Antônio Carlos Jobim", "duration_s": 242.0, "playable": false}),
			"20-music search": tracks(
				playable("cover1", "Garota de Ipanema (Cover)", "Someone", 242),
				playable("other1", "Garota de Ipanema", "Other Band", 400),
				playable("real1", "Garota de Ipanema", "Tom Jobim", 243),
			),
		},
	}
	track, err := ResolveTrack(context.Background(), sources, TrackRequest{URL: link})
	if err != nil || track.ID != "real1" || track.RequestedURL != link {
		t.Fatal(track, err)
	}
	if sources.asked[len(sources.asked)-1] != "20-music search" {
		t.Fatal(sources.asked)
	}
}

func TestASourceRefusalReachesThePersonAtOnce(t *testing.T) {
	sources := &stubSources{
		order: []string{"10-catalogue", "20-music"},
		replies: map[string]map[string]any{
			"10-catalogue read": {"handled": true, "refused": "only track links are supported, not albums"},
		},
	}
	_, err := ResolveTrack(context.Background(), sources, TrackRequest{URL: "https://catalogue.example/album/1"})
	if !errors.Is(err, ErrTrackRefused) || !strings.Contains(err.Error(), "not albums") || len(sources.asked) != 1 {
		t.Fatal(err, sources.asked)
	}
}

func TestNothingToPlayIsSaidPlainly(t *testing.T) {
	sources := &stubSources{
		order: []string{"10-catalogue", "20-music"},
		replies: map[string]map[string]any{
			"10-catalogue read": tracks(map[string]any{"title": "Garota de Ipanema", "uploader": "Antônio Carlos Jobim", "duration_s": 242.0}),
			"20-music search":   tracks(playable("cover1", "Garota de Ipanema (ao vivo)", "Someone", 242)),
		},
	}
	_, err := ResolveTrack(context.Background(), sources, TrackRequest{URL: "https://catalogue.example/track/1"})
	if !errors.Is(err, ErrTrackNotFound) || !strings.Contains(err.Error(), "Garota de Ipanema") {
		t.Fatal(err)
	}
	for _, request := range []TrackRequest{{}, {Query: "a", URL: "https://x.example"}} {
		if _, err := ResolveTrack(context.Background(), sources, request); !errors.Is(err, ErrTrackRefused) {
			t.Fatal(request, err)
		}
	}
	if _, err := ResolveTrack(context.Background(), &stubSources{}, TrackRequest{Query: "monalisa"}); !errors.Is(err, ErrTrackNotFound) {
		t.Fatal(err)
	}
}

func TestBestMatchKeepsTitleWordsAndAllowsWhatTheSongHasItself(t *testing.T) {
	song := Track{Title: "Monalisa (Ao Vivo)", Uploader: "Jorge Vercillo", DurationS: 260}
	candidates := []Track{
		{Title: "Monalisa", Uploader: "Jorge Vercillo", DurationS: 260, Playable: true, ID: "studio"},
		{Title: "Monalisa (Ao Vivo) - Remastered", Uploader: "Jorge Vercillo", DurationS: 262, Playable: true, ID: "live"},
	}
	if best, ok := BestMatch(song, candidates); !ok || best.ID != "live" {
		t.Fatal("a live song matches its live recording, not the studio one:", best, ok)
	}
	if _, ok := BestMatch(Track{Title: "Monalisa"}, []Track{{Title: "Monalisa", Playable: false}}); ok {
		t.Fatal("an unplayable candidate cannot be played")
	}
}
