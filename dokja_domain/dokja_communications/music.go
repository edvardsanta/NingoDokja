package communications

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// AudioCapability is the Feeds capability every music source provides.
const AudioCapability = "audio.resolve.v1"

// AudioSources reaches the music sources the owner installed as Feeds plugins. Sources are
// listed in the order they are asked; a source answers only for what it recognizes.
type AudioSources interface {
	AudioProviders(ctx context.Context) ([]string, error)
	InvokeAudio(ctx context.Context, plugin, action string, input map[string]any) (map[string]any, error)
}

// Track is one song as a source describes it. A track that is not Playable names a song (a
// catalogue knows its title, artist and length) that has to be found on a playable source.
type Track struct {
	ID           string
	Source       string
	URL          string
	Title        string
	Uploader     string
	DurationS    int
	Live         bool
	Playable     bool
	RequestedURL string
}

// TrackRequest is what someone asked to play: a few words, or a link.
type TrackRequest struct {
	Query string
	URL   string
}

// ErrTrackRefused is a request a source declined for a reason the person can act on.
var ErrTrackRefused = errors.New("refused")

// ErrTrackNotFound means no installed source could give a playable track.
var ErrTrackNotFound = errors.New("not found")

// durationSlack is how far apart, in seconds, the same recording can be on two services.
const durationSlack = 7

// otherVersions are words that make a result another version of the song, unless the song
// itself has them.
var otherVersions = map[string]bool{
	"cover": true, "karaoke": true, "instrumental": true, "remix": true, "live": true, "ao": true,
	"vivo": true, "acoustic": true, "acustico": true, "slowed": true, "sped": true, "reverb": true,
	"8d": true, "nightcore": true, "tutorial": true, "lesson": true,
}

// ResolveTrack finds one playable track for a request. Words go to the sources in order, and
// the first that finds songs gives the first of them. A link goes to the first source that
// recognizes it; when that source only names the song, the playable sources are searched for
// the same recording.
func ResolveTrack(ctx context.Context, sources AudioSources, request TrackRequest) (Track, error) {
	query := strings.Join(strings.Fields(request.Query), " ")
	link := strings.TrimSpace(request.URL)
	if (query == "") == (link == "") {
		return Track{}, fmt.Errorf("%w: give a few words or a link", ErrTrackRefused)
	}
	providers, err := sources.AudioProviders(ctx)
	if err != nil {
		return Track{}, err
	}
	if len(providers) == 0 {
		return Track{}, fmt.Errorf("%w: no music source is installed", ErrTrackNotFound)
	}
	if query != "" {
		return firstSearched(ctx, sources, providers, query)
	}
	named, err := firstRead(ctx, sources, providers, link)
	if err != nil || named.Playable {
		return named, err
	}
	found, err := sameRecording(ctx, sources, providers, named)
	if err != nil {
		return Track{}, err
	}
	found.RequestedURL = link
	return found, nil
}

func firstSearched(ctx context.Context, sources AudioSources, providers []string, query string) (Track, error) {
	var failures []string
	for _, provider := range providers {
		tracks, handled, err := ask(ctx, sources, provider, "search", map[string]any{"query": query, "limit": 1})
		if errors.Is(err, ErrTrackRefused) {
			return Track{}, err
		}
		if err != nil {
			failures = append(failures, err.Error())
			continue
		}
		if !handled {
			continue
		}
		for _, track := range tracks {
			if track.Playable {
				return track, nil
			}
		}
	}
	return Track{}, notFound(fmt.Sprintf("nothing found for %q", query), failures)
}

func firstRead(ctx context.Context, sources AudioSources, providers []string, link string) (Track, error) {
	var failures []string
	for _, provider := range providers {
		tracks, handled, err := ask(ctx, sources, provider, "read", map[string]any{"url": link})
		if errors.Is(err, ErrTrackRefused) {
			return Track{}, err
		}
		if err != nil {
			failures = append(failures, err.Error())
			continue
		}
		if !handled {
			continue
		}
		if len(tracks) == 0 {
			return Track{}, notFound("that link names no song", nil)
		}
		track := tracks[0]
		track.RequestedURL = link
		return track, nil
	}
	return Track{}, notFound("no installed source recognizes that link", failures)
}

// sameRecording searches the sources for the song a catalogue named, and keeps the first source's
// best match.
func sameRecording(ctx context.Context, sources AudioSources, providers []string, song Track) (Track, error) {
	query := strings.TrimSpace(song.Uploader + " " + song.Title)
	var failures []string
	for _, provider := range providers {
		candidates, handled, err := ask(ctx, sources, provider, "search", map[string]any{"query": query, "limit": 5})
		if errors.Is(err, ErrTrackRefused) {
			return Track{}, err
		}
		if err != nil {
			failures = append(failures, err.Error())
			continue
		}
		if !handled {
			continue
		}
		if best, ok := BestMatch(song, candidates); ok {
			return best, nil
		}
	}
	return Track{}, notFound(fmt.Sprintf("could not find %q by %s to play", song.Title, song.Uploader), failures)
}

// BestMatch picks, among playable candidates, the same recording as the song: the same title,
// a length within a few seconds when both are known, and not another version of it. The
// artist only orders the survivors, because one singer is credited under different names on
// different services.
func BestMatch(song Track, candidates []Track) (Track, bool) {
	title := words(song.Title)
	if len(title) == 0 {
		return Track{}, false
	}
	artist := words(song.Uploader)
	var best Track
	bestShared, found := -1, false
	for _, candidate := range candidates {
		if !candidate.Playable {
			continue
		}
		name := words(candidate.Title)
		if !sameTitle(title, name) || anotherVersion(title, name) {
			continue
		}
		if song.DurationS > 0 && candidate.DurationS > 0 && abs(song.DurationS-candidate.DurationS) > durationSlack {
			continue
		}
		shared := 0
		for word := range words(candidate.Uploader) {
			if artist[word] {
				shared++
			}
		}
		if shared > bestShared {
			best, bestShared, found = candidate, shared, true
		}
	}
	return best, found
}

// sameTitle holds when every word of the song's title is in the candidate's: a candidate may add
// "Remastered" or the album, but not lose words.
func sameTitle(title, name map[string]bool) bool {
	for word := range title {
		if !name[word] {
			return false
		}
	}
	return true
}

func anotherVersion(title, name map[string]bool) bool {
	for word := range name {
		if otherVersions[word] && !title[word] {
			return true
		}
	}
	return false
}

func ask(ctx context.Context, sources AudioSources, provider, action string, input map[string]any) ([]Track, bool, error) {
	result, err := sources.InvokeAudio(ctx, provider, action, input)
	if err != nil {
		return nil, false, fmt.Errorf("%s: %w", provider, err)
	}
	if handled, _ := result["handled"].(bool); !handled {
		return nil, false, nil
	}
	if refused, _ := result["refused"].(string); refused != "" {
		return nil, true, fmt.Errorf("%w: %s", ErrTrackRefused, refused)
	}
	raw, _ := result["tracks"].([]any)
	tracks := make([]Track, 0, len(raw))
	for _, item := range raw {
		if fields, ok := item.(map[string]any); ok {
			if track, ok := trackOf(fields); ok {
				tracks = append(tracks, track)
			}
		}
	}
	return tracks, true, nil
}

func trackOf(fields map[string]any) (Track, bool) {
	text := func(key string) string { value, _ := fields[key].(string); return value }
	track := Track{
		ID: text("id"), Source: text("source"), URL: text("url"), Title: text("title"), Uploader: text("uploader"),
		RequestedURL: text("requested_url"),
	}
	track.Live, _ = fields["live"].(bool)
	track.Playable, _ = fields["playable"].(bool)
	if duration, ok := fields["duration_s"].(float64); ok && duration > 0 {
		track.DurationS = int(duration)
	}
	if track.Title == "" || (track.Playable && track.URL == "") {
		return Track{}, false
	}
	return track, true
}

func notFound(message string, failures []string) error {
	if len(failures) > 0 {
		return fmt.Errorf("%w: %s (sources failed: %s)", ErrTrackNotFound, message, strings.Join(failures, "; "))
	}
	return fmt.Errorf("%w: %s", ErrTrackNotFound, message)
}

// accents folds the accented letters of the languages Ningo meets, so "Antônio" and "Antonio"
// are one word.
var accents = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a", "å", "a",
	"ç", "c", "é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i", "ñ", "n",
	"ó", "o", "ò", "o", "ô", "o", "õ", "o", "ö", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u", "ý", "y", "ÿ", "y",
)

func words(value string) map[string]bool {
	folded := accents.Replace(strings.ToLower(value))
	set := map[string]bool{}
	for _, word := range strings.FieldsFunc(folded, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		set[word] = true
	}
	return set
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
