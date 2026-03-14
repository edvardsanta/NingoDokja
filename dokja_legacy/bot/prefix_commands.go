package bot

import (
	"log"
	"read_books/internal/logger"
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
		Name:    "!join",
		Execute: joinVoiceChannel,
	},
}

func recordAudio(s *discordgo.Session, m *discordgo.MessageCreate) {

}

func joinVoiceChannel(s *discordgo.Session, m *discordgo.MessageCreate) {
	guild, err := s.State.Guild(m.GuildID)
	if err != nil {
		log.Printf("Erro encontrar servidor: %v", err)
		return
	}

	if _, err := joinChannel(s, guild, m.Author.ID); err != nil {
		logger.Error("Erro ao entrar no canal: %v", err)
		return
	}
}
func playRadio(s *discordgo.Session, m *discordgo.MessageCreate) {

}

func playPlaylist(s *discordgo.Session, m *discordgo.MessageCreate) {
	guild, err := s.State.Guild(m.GuildID)
	if err != nil {
		log.Printf("Erro encontrar servidor: %v", err)
		return
	}
	_, err = joinChannel(s, guild, m.Author.ID)
	if err != nil {
		logger.Error("Erro ao entrar no canal: %v", err)
		return
	}

	/*if err := audio.PlayAllSounds(vc); err != nil {
		log.Printf("Error playing radio: %v", err)
		s.ChannelMessageSend(m.ChannelID, "Failed to play radio.")
	}*/
}

func ping(s *discordgo.Session, m *discordgo.MessageCreate) {
	s.ChannelMessageSend(m.ChannelID, "Pong!")
}

// obsolete
func olympicDay(s *discordgo.Session, m *discordgo.MessageCreate) {
	logger.Info("Enviando atualizações das olimpiadas")
	sendOlympicUpdates(s, m.ChannelID, "")
}

func cnnNews(s *discordgo.Session, m *discordgo.MessageCreate) {
	sendNews(s, m.ChannelID)
}

func setVolume(s *discordgo.Session, m *discordgo.MessageCreate) {

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
