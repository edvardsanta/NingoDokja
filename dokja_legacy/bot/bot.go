package bot

import (
	"fmt"
	"os"
	"read_books/internal/legacy/config"
	"read_books/internal/logger"
	"read_books/internal/orchestrator"
	"time"

	"github.com/avast/retry-go"
	"github.com/bwmarrin/discordgo"
)

type ChannelType string

const (
	ChannelTypeUnknown ChannelType = "unknown"
	ChannelTypeNews    ChannelType = "news"
	ChannelTypeMemes               = "memes"
	ChannelTypeSports  ChannelType = "sports"
	ChannelTypeChat    ChannelType = "chat"
)

type Channel struct {
	ID   string
	Name string
	Type ChannelType
	// Add more metadata if needed
}

type Bot struct {
	Session  *discordgo.Session
	Channels map[ChannelType]*Channel // key: channel ID
	GuildID  string
}

type Component struct {
	bot *Bot
}

func NewBot(token string, channels []*Channel, guildID string) *Bot {
	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		logger.Error("Erro ao criar a sessão do Discord", err)
		return nil
	}
	chMap := make(map[ChannelType]*Channel)
	for _, ch := range channels {
		chMap[ch.Type] = ch
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

func (c *Component) Name() string {
	return "discord-bot"
}

func (c *Component) Start() error {
	if c == nil || c.bot == nil {
		return fmt.Errorf("bot component is not configured")
	}

	c.bot.AddHandlers()
	return c.bot.Open()
}

func (c *Component) Stop() error {
	if c == nil || c.bot == nil {
		return nil
	}

	c.bot.Close()
	return nil
}

func (b *Bot) AddHandlers() {
	b.Session.AddHandler(func(s *discordgo.Session, m *discordgo.MessageCreate) {
		channel, ok := b.Channels[ChannelType(m.ChannelID)]
		if !ok {
			channel = &Channel{ID: m.ChannelID, Type: ChannelTypeUnknown}
		}

		switch channel.Type {
		case ChannelTypeChat:
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

func NewComponent() (orchestrator.Component, error) {
	var channels []*Channel
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

	var component orchestrator.Component
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
			component = &Component{bot: b}
			return nil
		},
		retry.Attempts(3),
		retry.Delay(5*time.Second),
		retry.DelayType(retry.FixedDelay),
	)

	if err != nil {
		return nil, err
	}

	return component, nil
}

func StartBotComponent(stop chan os.Signal) {
	component, err := NewComponent()
	if err != nil {
		logger.Error("Erro ao executar o bot", err)
		return
	}

	if err := component.Start(); err != nil {
		logger.Error("Erro ao abrir a conexão com o Discord", err)
		return
	}

	logger.Info("Ningo Dokja está rodando. Pressione CTRL+C para sair.")
	<-stop
	if err := component.Stop(); err != nil {
		logger.Error("Erro ao encerrar o bot", err)
	}
}
