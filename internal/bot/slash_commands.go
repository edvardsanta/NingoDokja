package bot

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
)

var slashCommands = []*discordgo.ApplicationCommand{
	{
		Name:        "help",
		Description: "Mostra a lista de comandos disponíveis",
	},
}

func HandleSlashCommands(s *discordgo.Session, i *discordgo.InteractionCreate) {
	switch i.ApplicationCommandData().Name {
	case "help":
		handleHelpCommand(s, i)
	}
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
