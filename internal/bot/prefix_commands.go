package bot

import (
	"github.com/bwmarrin/discordgo"
)

type Command struct {
	Name    string
	Execute func(s *discordgo.Session, m *discordgo.MessageCreate)
}

var commands = []Command{
	{
		Name:    "!ping",
		Execute: ping,
	},
	{
		Name:    "!news",
		Execute: cnnNews,
	},
}

func ping(s *discordgo.Session, m *discordgo.MessageCreate) {
	s.ChannelMessageSend(m.ChannelID, "Pong!")
}

func cnnNews(s *discordgo.Session, m *discordgo.MessageCreate) {
	sendNews(s, m.ChannelID)
}

func HandleCommand(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID {
		return
	}

	for _, cmd := range commands {
		if m.Content == cmd.Name {
			cmd.Execute(s, m)
			return
		}
	}
}
