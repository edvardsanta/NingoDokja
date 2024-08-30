package bot

import (
	"log"
	"read_books/internal/logger"
	"read_books/internal/usecase/audio"
	"strings"

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
	{
		Name:    "!olympic_medals",
		Execute: olympicDay,
	},
	{
		Name:    "!join",
		Execute: joinVoiceChannel,
	},
	{
		Name:    "!playradio",
		Execute: playRadio,
	},
	{
		Name:    "!playplaylist",
		Execute: playPlaylist,
	},
}

func joinVoiceChannel(s *discordgo.Session, m *discordgo.MessageCreate) {
	guild, err := s.State.Guild(m.GuildID)
	if err != nil {
		log.Printf("Erro encontrar servidor: %v", err)
		return
	}

	if _, err := joinChannel(s, guild, m.Author.ID, true); err != nil {
		logger.Error("Erro ao entrar no canal: %v", err)
		return
	}
}
func playRadio(s *discordgo.Session, m *discordgo.MessageCreate) {
	url := getArgument(m.Content, 1)
	if url == "" {
		s.ChannelMessageSend(m.ChannelID, "Please provide a radio URL.")
		return
	}
	guild, err := s.State.Guild(m.GuildID)
	if err != nil {
		log.Printf("Erro encontrar servidor: %v", err)
		return
	}
	vc, err := joinChannel(s, guild, m.Author.ID, false)
	if err != nil {
		logger.Error("Erro ao entrar no canal: %v", err)
		return
	}

	if err := audio.PlayRadioStream(vc, url); err != nil {
		log.Printf("Error playing radio: %v", err)
		s.ChannelMessageSend(m.ChannelID, "Failed to play radio.")
	}
}

func playPlaylist(s *discordgo.Session, m *discordgo.MessageCreate) {
	guild, err := s.State.Guild(m.GuildID)
	if err != nil {
		log.Printf("Erro encontrar servidor: %v", err)
		return
	}
	vc, err := joinChannel(s, guild, m.Author.ID, false)
	if err != nil {
		logger.Error("Erro ao entrar no canal: %v", err)
		return
	}

	if err := audio.PlayAllSounds(vc); err != nil {
		log.Printf("Error playing radio: %v", err)
		s.ChannelMessageSend(m.ChannelID, "Failed to play radio.")
	}
}

func ping(s *discordgo.Session, m *discordgo.MessageCreate) {
	s.ChannelMessageSend(m.ChannelID, "Pong!")
}

func olympicDay(s *discordgo.Session, m *discordgo.MessageCreate) {
	logger.Info("Enviando atualizações das olimpiadas")
	sendOlympicUpdates(s, m.ChannelID, "")
}

func cnnNews(s *discordgo.Session, m *discordgo.MessageCreate) {
	sendNews(s, m.ChannelID)
}

func HandleCommand(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.ID == s.State.User.ID {
		return
	}

	for _, cmd := range commands {
		if m.Content == cmd.Name || strings.HasPrefix(m.Content, cmd.Name+" ") {
			cmd.Execute(s, m)
			return
		}
	}
}

func getArgument(input string, index int) string {
	parts := strings.Fields(input)
	if index < len(parts) {
		return parts[index]
	}
	return ""
}
