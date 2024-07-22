package bot

import (
	"github.com/bwmarrin/discordgo"
)

func MessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID {
		return
	}
	HandleCommand(s, m)
}

// TODO: Para um canal especifico deve fazer uma ação especifica
func MessageCreateForSpecificChannel(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID {
		return
	}
	HandleCommand(s, m)
}
