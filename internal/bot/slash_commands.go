package bot

import (
	"fmt"
	"github.com/bwmarrin/discordgo"
	"read_books/internal/logger"
)

var slashCommands = []*discordgo.ApplicationCommand{
	{
		Name:        "help",
		Description: "Mostra a lista de comandos disponíveis",
	},
	{
		Name:        "play",
		Description: "Toca musica no canal que foi chamado",
	},
	{
		Name:        "stop",
		Description: "Para música",
	},
	{
		Name:        "playradio",
		Description: "Play a radio stream in the current voice channel",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Name:        "url",
				Description: "The URL of the radio stream",
				Type:        discordgo.ApplicationCommandOptionString,
				Required:    true,
			},
		},
	},
}

func HandleSlashCommands(s *discordgo.Session, i *discordgo.InteractionCreate) {
	switch i.ApplicationCommandData().Name {
	case "help":
		handleHelpCommand(s, i)
	case "play":
		handlePlayCommand(s, i)
	case "stop":
		handleStopCommand(s, i)
	case "playradio":
		handlePlayRadioCommand(s, i)
	}
}
func handlePlayCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	guild, err := s.State.Guild(i.GuildID)
	if err != nil {
		logger.Error("Erro ao entrar no canal: %v", err)
		return
	}

	// Responder imediatamente para evitar expiração da interação
	initialResponse := &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "Iniciando configurações para tocar musicas.",
		},
	}
	err = s.InteractionRespond(i.Interaction, initialResponse)
	if err != nil {
		logger.Error("Erro ao responder à interação: %v", err)
		editResponse(s, i, "Erro ao tentar tocar música no canal.")
		return
	}

	// Processar o comando (por exemplo, entrar no canal de voz)
	if _, err := joinChannel(s, guild, i.Member.User.ID); err != nil {
		logger.Error("Erro ao entrar no canal: %v", err)
		return
	}

	editResponse(s, i, "A música está tocando no canal atual.")
}
func editResponse(s *discordgo.Session, i *discordgo.InteractionCreate, content string) {
	_, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content: &content,
	})
	if err != nil {
		logger.Error("Erro ao editar a resposta da interação: %v", err)
	}
}
func handleStopCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	response := &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "Parando musica.",
		},
	}

	s.InteractionRespond(i.Interaction, response)
}

func handleHelpCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	helpMessage := "Lista de comandos disponíveis:\n"
	for _, cmd := range commands {
		helpMessage += fmt.Sprintf("%s\n", cmd.Name)
	}

	response := &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: helpMessage,
		},
	}

	s.InteractionRespond(i.Interaction, response)
}

func handlePlayRadioCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {

}
