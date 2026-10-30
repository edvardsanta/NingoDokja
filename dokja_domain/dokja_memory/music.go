package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

// musicListenAction names one person's listening: each person has their own action, so one
// person's songs teach nothing about another's and a listing shows only theirs.
const musicListenAction = "music.listen:"

// How a song ended for the person who asked for it. They are kept in detail, and map to the
// outcomes every experience shares: accepted is the only success.
const (
	ListenFinished = "finished"
	ListenSkipped  = "skipped"
	ListenStopped  = "stopped"
)

var listenOutcomes = map[string]string{
	ListenFinished: OutcomeAccepted,
	ListenSkipped:  OutcomeReplaced,
	ListenStopped:  OutcomeIgnored,
}

// listener is a Discord user id: the only part of the action a person's identity decides.
var listener = regexp.MustCompile(`^[0-9]{1,20}$`)

// MusicListen is an immutable, prepared observation of one song played for one person.
type MusicListen struct {
	ref     string
	action  string
	song    string
	ended   string
	outcome string
}

// MusicPlay is one line of a person's history.
type MusicPlay struct {
	Song  string
	Ended string
}

// PrepareMusicListen builds the observation. play names this one playing, so a repeated report
// lands on the same experience. Without a known person, a song or a known ending there is no note.
func PrepareMusicListen(play, person, title, uploader, ended string) (MusicListen, bool) {
	outcome, known := listenOutcomes[ended]
	song := musicSong(title, uploader)
	if !known || !listener.MatchString(person) || song == "" || strings.TrimSpace(play) == "" {
		return MusicListen{}, false
	}
	sum := sha256.Sum256([]byte(play))
	return MusicListen{
		ref:     "music:" + hex.EncodeToString(sum[:])[:32],
		action:  musicListenAction + person,
		song:    song,
		ended:   ended,
		outcome: outcome,
	}, true
}

func musicSong(title, uploader string) string {
	title, uploader = strings.TrimSpace(title), strings.TrimSpace(uploader)
	if title != "" && uploader != "" {
		return boundedHashtagText(title+" – "+uploader, maxMatchedRunes)
	}
	return boundedHashtagText(title+uploader, maxMatchedRunes)
}

// RecordRequest stores the song as the context, which is what later similarity works on.
func (l MusicListen) RecordRequest() Request {
	return hashtagMemoryRequest(ActionRecordExperience, map[string]any{
		"ref": l.ref, "action": l.action, "context": l.song, "detail": l.ended,
	})
}

// ResolveRequest settles it at once: a song's ending is known when it is noted. A resolved
// experience never expires.
func (l MusicListen) ResolveRequest() Request {
	return hashtagMemoryRequest(ActionResolveExperience, map[string]any{"ref": l.ref, "outcome": l.outcome})
}

// MusicHistoryRequest lists one person's songs, newest first. person must come from the verified
// identity of whoever asks, never from what they typed.
func MusicHistoryRequest(person string, limit int) (Request, bool) {
	if !listener.MatchString(person) {
		return Request{}, false
	}
	return hashtagMemoryRequest(ActionListExperience, map[string]any{
		"action": musicListenAction + person, "state": "resolved", "limit": float64(limit), "include_context": true,
	}), true
}

// MusicHistory reads a listing back.
func MusicHistory(listing map[string]any) []MusicPlay {
	raw, _ := listing["experiences"].([]any)
	plays := make([]MusicPlay, 0, len(raw))
	for _, item := range raw {
		experience, _ := item.(map[string]any)
		song, _ := experience["context_snippet"].(string)
		ended, _ := experience["detail"].(string)
		if song != "" {
			plays = append(plays, MusicPlay{Song: song, Ended: ended})
		}
	}
	return plays
}
