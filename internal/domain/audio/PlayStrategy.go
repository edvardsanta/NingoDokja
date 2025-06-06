package domain

import "github.com/bwmarrin/discordgo"

type PlayStrategy interface {
	Play(vc *discordgo.VoiceConnection) error
	Stop() error
	SetVolume(vc *discordgo.VoiceConnection, volume int) error
}
