package communications

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
)

// MaxWaiting is how many tracks may wait behind the one playing (owner, 2026-10-26).
const MaxWaiting = 3

// MaxVolume is the loudest a queue plays, in percent.
const MaxVolume = 100

// maxCommentRunes bounds Ningo's remark on a song: a line on the panel, not a review.
const maxCommentRunes = 200

// ErrQueueFull refuses a track while MaxWaiting tracks already wait.
var ErrQueueFull = errors.New("the queue is full")

// ErrNothingPlaying refuses what needs a track playing.
var ErrNothingPlaying = errors.New("nothing is playing")

// ErrBadVolume refuses a volume outside 0 to MaxVolume.
var ErrBadVolume = errors.New("the volume goes from 0 to 100")

// QueuedTrack is a track in a queue. Entry numbers each addition, so a report about one play
// of a song cannot advance another play of the same song.
type QueuedTrack struct {
	Track
	Entry       int
	RequestedBy string
}

// MusicQueue is one voice channel's queue: the track playing and the ones waiting. Anyone in
// the channel may control it. It holds no lock; whoever keeps queues serializes their use.
type MusicQueue struct {
	now     *QueuedTrack
	waiting []QueuedTrack
	paused  bool
	volume  int
	entries int
	// version goes up on every change, so whoever plays the queue can tell a newer state from
	// an older reply that arrived late.
	version int
}

// QueueState is a copy of a queue, safe to hand out.
type QueueState struct {
	NowPlaying *QueuedTrack
	Waiting    []QueuedTrack
	Paused     bool
	Volume     int
	Version    int
}

func NewMusicQueue() *MusicQueue { return &MusicQueue{volume: MaxVolume} }

// Full reports whether an addition would be refused. Checked before resolving a track, so a
// full queue refuses at once instead of after a search.
func (q *MusicQueue) Full() bool { return q.now != nil && len(q.waiting) >= MaxWaiting }

// Add plays the track when nothing is playing, or puts it to wait. Its place is 0 when it
// plays now, otherwise its position among those waiting, from 1.
func (q *MusicQueue) Add(track Track, requestedBy string) (QueuedTrack, int, error) {
	if q.Full() {
		return QueuedTrack{}, 0, ErrQueueFull
	}
	q.entries++
	q.version++
	queued := QueuedTrack{Track: track, Entry: q.entries, RequestedBy: requestedBy}
	if q.now == nil {
		q.now, q.paused = &queued, false
		return queued, 0, nil
	}
	q.waiting = append(q.waiting, queued)
	return queued, len(q.waiting), nil
}

// Skip ends the track playing and starts the next, if any.
func (q *MusicQueue) Skip() (*QueuedTrack, error) {
	if q.now == nil {
		return nil, ErrNothingPlaying
	}
	return q.advance(), nil
}

// Ended advances the queue when the track playing finished. A report about any other entry
// (late, repeated, or about a track already skipped) changes nothing.
func (q *MusicQueue) Ended(entry int) (*QueuedTrack, bool) {
	if q.now == nil || q.now.Entry != entry {
		return nil, false
	}
	return q.advance(), true
}

func (q *MusicQueue) advance() *QueuedTrack {
	q.version++
	q.paused = false
	if len(q.waiting) == 0 {
		q.now = nil
		return nil
	}
	next := q.waiting[0]
	q.waiting = append([]QueuedTrack(nil), q.waiting[1:]...)
	q.now = &next
	return q.now
}

func (q *MusicQueue) Pause() error {
	if q.now == nil {
		return ErrNothingPlaying
	}
	q.paused = true
	q.version++
	return nil
}

func (q *MusicQueue) Resume() error {
	if q.now == nil {
		return ErrNothingPlaying
	}
	q.paused = false
	q.version++
	return nil
}

// Stop ends the track playing and empties the queue. The volume stays.
func (q *MusicQueue) Stop() {
	q.now, q.waiting, q.paused = nil, nil, false
	q.version++
}

func (q *MusicQueue) SetVolume(volume int) error {
	if volume < 0 || volume > MaxVolume {
		return ErrBadVolume
	}
	q.volume = volume
	q.version++
	return nil
}

// Active reports whether anything is playing or waiting; the radio is refused while it is.
func (q *MusicQueue) Active() bool { return q.now != nil }

func (q *MusicQueue) State() QueueState {
	state := QueueState{Paused: q.paused, Volume: q.volume, Version: q.version, Waiting: append([]QueuedTrack(nil), q.waiting...)}
	if q.now != nil {
		now := *q.now
		state.NowPlaying = &now
	}
	return state
}

// CommentDue says whether Ningo remarks on this queue entry: about one song in three, from the first
// (owner, 2026-10-26: "de vez em quando").
func CommentDue(entry int) bool { return entry > 0 && (entry-1)%3 == 0 }

// CommentLine is a model's remark as the panel shows it: one line, at most maxCommentRunes, with no
// surrounding quotes. Nothing usable is an empty line.
func CommentLine(text string) string {
	line := strings.Join(strings.Fields(text), " ")
	line = strings.Trim(line, "\"'“”‘’`")
	if runes := []rune(line); len(runes) > maxCommentRunes {
		line = strings.TrimSpace(string(runes[:maxCommentRunes-1])) + "…"
	}
	return line
}

// TrackRequestFromText reads what someone typed after the play command: one link is a link,
// anything else is a few words to search.
func TrackRequestFromText(text string) TrackRequest {
	fields := strings.Fields(text)
	if len(fields) == 1 {
		if parsed, err := url.Parse(fields[0]); err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != "" {
			return TrackRequest{URL: fields[0]}
		}
	}
	return TrackRequest{Query: strings.Join(fields, " ")}
}

// VolumeFromText reads a volume typed as a number, with or without a percent sign.
func VolumeFromText(text string) (int, error) {
	volume, err := strconv.Atoi(strings.TrimSuffix(strings.TrimSpace(text), "%"))
	if err != nil {
		return 0, ErrBadVolume
	}
	return volume, nil
}
