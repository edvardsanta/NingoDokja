package handlers

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	communications "read_books/dokja_domain/dokja_communications"
	"read_books/internal/core"
)

// musicResolveTimeout bounds the search for a track, below the Discord adapter's own wait for the
// orchestrator (180 seconds), so the person always gets an answer.
const musicResolveTimeout = 150 * time.Second

// musicDesk keeps one queue per server, in memory: a restart empties it (ADR 0003). The rules are
// the Communications domain's; the desk only serializes each server's commands.
type musicDesk struct {
	sources communications.AudioSources
	timeout time.Duration

	mu     sync.Mutex
	queues map[string]*guildQueue
}

type guildQueue struct {
	mu    sync.Mutex
	queue *communications.MusicQueue
}

func newMusicDesk(sources communications.AudioSources) *musicDesk {
	return &musicDesk{sources: sources, timeout: musicResolveTimeout, queues: map[string]*guildQueue{}}
}

func (d *musicDesk) guild(id string) *guildQueue {
	d.mu.Lock()
	defer d.mu.Unlock()
	queue, ok := d.queues[id]
	if !ok {
		queue = &guildQueue{queue: communications.NewMusicQueue()}
		d.queues[id] = queue
	}
	return queue
}

var musicActions = map[string]bool{
	"play-music": true, "skip-music": true, "pause-music": true, "resume-music": true,
	"stop-music": true, "show-music-queue": true, "set-music-volume": true, "end-music-track": true,
}

// handle answers every refusal as a result the person reads ({ok: false, reason}); an error is
// left for a broken source or transport.
func (d *musicDesk) handle(ctx context.Context, event core.Event, action string) (map[string]any, error) {
	// The server comes from the bridge's context, never from what the person typed.
	guildID, _ := event.Context["guild_id"].(string)
	if strings.TrimSpace(guildID) == "" {
		return refusedMusic("music plays only in a server's voice channel"), nil
	}
	guild := d.guild(guildID)
	content, _ := event.Payload["content"].(string)
	if action == "play-music" {
		return d.play(ctx, guild, communications.TrackRequestFromText(content), event.User.ID)
	}
	guild.mu.Lock()
	defer guild.mu.Unlock()
	queue := guild.queue
	var err error
	reply := map[string]any{}
	switch action {
	case "skip-music":
		_, err = queue.Skip()
	case "pause-music":
		err = queue.Pause()
	case "resume-music":
		err = queue.Resume()
	case "stop-music":
		queue.Stop()
	case "set-music-volume":
		var volume int
		if volume, err = communications.VolumeFromText(content); err == nil {
			err = queue.SetVolume(volume)
		}
	case "end-music-track":
		entry, _ := event.Payload["entry"].(float64)
		_, advanced := queue.Ended(int(entry))
		reply["advanced"] = advanced
	}
	if err != nil {
		return refusedMusic(err.Error()), nil
	}
	return musicReply(queue.State(), reply), nil
}

// play checks room before searching, searches without holding the server's queue (a search can
// take long, and skip or pause must not wait for it), then checks room again before adding.
func (d *musicDesk) play(ctx context.Context, guild *guildQueue, request communications.TrackRequest, requestedBy string) (map[string]any, error) {
	if request.Query == "" && request.URL == "" {
		return refusedMusic("say what to play: a few words or a link"), nil
	}
	guild.mu.Lock()
	full := guild.queue.Full()
	guild.mu.Unlock()
	if full {
		return refusedMusic(communications.ErrQueueFull.Error()), nil
	}
	if d.sources == nil {
		return refusedMusic("no music source is set up"), nil
	}
	resolveCtx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()
	track, err := communications.ResolveTrack(resolveCtx, d.sources, request)
	if errors.Is(err, communications.ErrTrackRefused) || errors.Is(err, communications.ErrTrackNotFound) {
		return refusedMusic(err.Error()), nil
	}
	if err != nil {
		return nil, err
	}
	guild.mu.Lock()
	defer guild.mu.Unlock()
	queued, place, err := guild.queue.Add(track, requestedBy)
	if err != nil {
		return refusedMusic(err.Error()), nil
	}
	return musicReply(guild.queue.State(), map[string]any{"added": queuedMap(queued), "place": place}), nil
}

func refusedMusic(reason string) map[string]any {
	return map[string]any{"music": map[string]any{"ok": false, "reason": reason}}
}

func musicReply(state communications.QueueState, extra map[string]any) map[string]any {
	music := map[string]any{"ok": true, "paused": state.Paused, "volume": state.Volume}
	if state.NowPlaying != nil {
		music["now_playing"] = queuedMap(*state.NowPlaying)
	}
	waiting := make([]any, len(state.Waiting))
	for i, track := range state.Waiting {
		waiting[i] = queuedMap(track)
	}
	music["waiting"] = waiting
	for key, value := range extra {
		music[key] = value
	}
	return map[string]any{"music": music}
}

func queuedMap(track communications.QueuedTrack) map[string]any {
	queued := map[string]any{
		"entry": track.Entry, "id": track.ID, "source": track.Source, "url": track.URL,
		"title": track.Title, "uploader": track.Uploader, "live": track.Live, "requested_by": track.RequestedBy,
	}
	if track.DurationS > 0 {
		queued["duration_s"] = track.DurationS
	}
	if track.RequestedURL != "" {
		queued["requested_url"] = track.RequestedURL
	}
	return queued
}
