package handlers

import (
	"context"
	"fmt"

	memorydomain "read_books/dokja_domain/dokja_memory"
	"read_books/internal/logger"
)

// musicHistoryLength is how many songs a person sees of their own history.
const musicHistoryLength = 10

// MusicExperience notes each song played in the memory of the person who asked for it, with how it
// ended. Slow or stopped memory never touches the music.
//
// A person's songs are theirs: they go to the memory service and are never logged here, only the
// shape of what happened.
type MusicExperience struct {
	domain  *memorydomain.Domain
	enabled func() bool
	spawn   func(func())
}

// NewMusicExperience wires the notes to the memory service. enabled is asked on every call so the
// operator's switch takes effect without a restart; it may be nil.
func NewMusicExperience(service memorydomain.Service, enabled func() bool) *MusicExperience {
	return &MusicExperience{domain: memorydomain.New(service), enabled: enabled, spawn: func(work func()) { go work() }}
}

func (e *MusicExperience) active() bool {
	return e != nil && e.domain != nil && (e.enabled == nil || e.enabled())
}

// Listened notes a song that left the queue, in the background. Called with the server's queue
// held, so it only schedules.
func (e *MusicExperience) Listened(play, person, title, uploader, ended string) {
	if !e.active() {
		return
	}
	listen, ok := memorydomain.PrepareMusicListen(play, person, title, uploader, ended)
	if !ok {
		return
	}
	e.spawn(func() {
		ctx, cancel := context.WithTimeout(context.Background(), experienceBudget)
		defer cancel()
		if _, err := e.domain.Handle(ctx, listen.RecordRequest()); err != nil {
			logger.Info(fmt.Sprintf("music experience not recorded: %v", err))
			return
		}
		if _, err := e.domain.Handle(ctx, listen.ResolveRequest()); err != nil {
			logger.Info(fmt.Sprintf("music experience not resolved: %v", err))
		}
	})
}

// History is what this person listened to, newest first. person is the verified sender of the
// request. ok is false when memory cannot answer.
func (e *MusicExperience) History(ctx context.Context, person string) ([]memorydomain.MusicPlay, bool) {
	if !e.active() {
		return nil, false
	}
	request, ok := memorydomain.MusicHistoryRequest(person, musicHistoryLength)
	if !ok {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(ctx, 2*experienceBudget)
	defer cancel()
	listing, err := e.domain.Handle(ctx, request)
	if err != nil {
		logger.Info(fmt.Sprintf("music history not read: %v", err))
		return nil, false
	}
	return memorydomain.MusicHistory(listing), true
}
