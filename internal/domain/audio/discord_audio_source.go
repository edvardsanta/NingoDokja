package domain

import (
	"fmt"
	"github.com/bwmarrin/discordgo"
)

type DiscordAudioSource struct {
	vc *discordgo.VoiceConnection
}

func NewDiscordAudioSource(vc *discordgo.VoiceConnection) *DiscordAudioSource {
	return &DiscordAudioSource{vc}
}

func (d *DiscordAudioSource) ReadPCMFrame() ([]byte, error) {
	pkt, ok := <-d.vc.OpusRecv
	if !ok {
		return nil, fmt.Errorf("canal de áudio do discord fechado")
	}
	return pkt.Opus, nil
}

func (d *DiscordAudioSource) Close() error {
	return nil // nada a fechar
}
