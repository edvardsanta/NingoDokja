package bot

import (
	"fmt"
	"os"
	"read_books/internal/config"
	"read_books/internal/logger"
	"time"

	"github.com/avast/retry-go"
	"github.com/bwmarrin/discordgo"
)

type ChannelType int

const (
	ChannelTypeUnknown ChannelType = iota
	ChannelTypeNews
	ChannelTypeMemes
	ChannelTypeSports
	ChannelTypeChat
)

type Channel struct {
	ID   string
	Name string
	Type ChannelType
	// Add more metadata if needed
}

type Bot struct {
	Session  *discordgo.Session
	Channels map[string]*Channel // key: channel ID
	GuildID  string
}

func NewBot(token string, channels []*Channel, guildID string) *Bot {
	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		logger.Error("Erro ao criar a sessão do Discord", err)
		return nil
	}
	chMap := make(map[string]*Channel)
	for _, ch := range channels {
		chMap[ch.ID] = ch
	}
	return &Bot{Session: dg, Channels: chMap, GuildID: guildID}
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
			logger.ErrorPrintf("Não foi poossivel criar o comando '%v': %v", v.Name, err)
		}
	}
	b.StartScheduler()

	return nil
}

func (b *Bot) Close() {
	err := b.Session.Close()
	if err != nil {
		return
	}
}

func (b *Bot) AddHandlers() {
	b.Session.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		channel, ok := b.Channels[m.ChannelID]
		if !ok {
			channel = &Channel{ID: m.ChannelID, Type: ChannelTypeUnknown}
		}

		switch channel.Type {
		case ChannelTypeNews:
			MessageCreateForSpecificChannel(s, m)
		default:
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

func StartBotComponent(stop chan os.Signal) {
	channels := []*Channel{}
	for _, chCfg := range config.AppConfig.Bot.Channels {
		var chType ChannelType
		switch chCfg.Type {
		case "news":
			chType = ChannelTypeNews
		case "memes":
			chType = ChannelTypeMemes
		case "chat":
			chType = ChannelTypeChat
		default:
			chType = ChannelTypeUnknown
		}
		channels = append(channels, &Channel{
			ID:   chCfg.ID,
			Name: chCfg.Name,
			Type: chType,
		})
	}

	err := retry.Do(
		func() error {
			b := NewBot(
				config.AppConfig.Bot.Token,
				channels,
				config.AppConfig.Bot.GuildID,
			)
			if b == nil {
				return fmt.Errorf("failed to create bot instance")
			}
			b.AddHandlers()

			err := b.Open()
			if err != nil {
				logger.Error("Erro ao abrir a conexão com o Discord", err)
				return err
			}

			logger.Info("Ningo Dokja está rodando. Pressione CTRL+C para sair.")
			<-stop
			b.Close()
			return nil
		},
		retry.Attempts(3),
		retry.Delay(5*time.Second),
		retry.DelayType(retry.FixedDelay),
	)

	if err != nil {
		logger.Error("Erro ao executar o bot", err)
	}
}
