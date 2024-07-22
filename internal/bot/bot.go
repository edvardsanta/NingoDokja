package bot

import (
	"fmt"
	"read_books/internal/logger"

	"github.com/bwmarrin/discordgo"
)

type Bot struct {
	Session       *discordgo.Session
	NewsChannelID string
	GuildID       string
}

func NewBot(token, newsChannelID, guildID string) *Bot {
	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		logger.Error("Erro ao criar a sessão do Discord", err)
		return nil
	}

	return &Bot{Session: dg, NewsChannelID: newsChannelID, GuildID: guildID}
}

func (b *Bot) Open() error {
	err := b.Session.Open()
	if err != nil {
		return err
	}
	b.Session.Identify.Intents = discordgo.IntentsGuildMessages

	for _, v := range slashCommands {
		_, err := b.Session.ApplicationCommandCreate(b.Session.State.User.ID, b.GuildID, v)
		if err != nil {
			fmt.Printf("Não foi poossivel criar o comando '%v': %v", v.Name, err)
		}
	}
	b.StartScheduler()

	return nil
}

func (b *Bot) Close() {
	b.Session.Close()
}

func (b *Bot) AddHandlers() {
	b.Session.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		// Verificar se a mensagem foi enviada no canal especificado
		if m.ChannelID == b.NewsChannelID {
			MessageCreateForSpecificChannel(s, m)
		} else {
			MessageCreate(s, m)
		}
	})

	b.Session.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		switch i.Type {
		case discordgo.InteractionApplicationCommand:
			HandleSlashCommands(s, i)
		}
	})
}
