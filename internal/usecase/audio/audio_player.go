package audio

import (
	"github.com/bwmarrin/discordgo"
	"read_books/internal/domain/audio"
	"sync"
)

type PlayerStatus int

const (
	Stopped PlayerStatus = iota
	Playing
)

type AudioPlayer struct {
	Strategy domain.PlayStrategy
	Status   PlayerStatus
	volume   int
	mu       sync.Mutex
}

func (p *AudioPlayer) Play(vc *discordgo.VoiceConnection) error {
	p.mu.Lock()
	p.Status = Playing
	p.mu.Unlock()
	if p.Strategy == nil {
		return nil
	}
	return p.Strategy.Play()
}

func (p *AudioPlayer) Stop() {
	p.mu.Lock()
	p.Status = Stopped
	p.mu.Unlock()
	p.Strategy.Stop()
}

func (p *AudioPlayer) SetStrategy(strategy domain.PlayStrategy) {
	p.Strategy = strategy
}

func (p *AudioPlayer) SetVolume(vc *discordgo.VoiceConnection, volume int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.volume = volume
	if p.Strategy != nil {
		return p.Strategy.SetVolume(volume)
	}
	return nil
}

func (p *AudioPlayer) GetVolume() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.volume
}

func (p *AudioPlayer) GetStatus() PlayerStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.Status
}
