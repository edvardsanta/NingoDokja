package bot

import (
	"context"
	"read_books/internal/logger"

	"github.com/bwmarrin/discordgo"
)

type DiscordChatAdapter struct {
	session       *discordgo.Session
	message       *discordgo.MessageCreate
	thinkingMsg   *discordgo.Message
	takingLongMsg *discordgo.Message
}

func NewDiscordChatAdapter(session *discordgo.Session, message *discordgo.MessageCreate) *DiscordChatAdapter {
	return &DiscordChatAdapter{
		session: session,
		message: message,
	}
}

func (a *DiscordChatAdapter) SendThinking(context.Context) error {
	msg, err := a.session.ChannelMessageSend(a.message.ChannelID, "Estou pensando, um momento por favor...")
	if err != nil {
		return err
	}
	a.thinkingMsg = msg
	return nil
}

func (a *DiscordChatAdapter) SendTakingLong(context.Context) error {
	msg, err := a.session.ChannelMessageSend(a.message.ChannelID, "Hmm, isso está tomando mais tempo que o esperado...")
	if err != nil {
		return err
	}
	a.takingLongMsg = msg
	return nil
}

func (a *DiscordChatAdapter) SendReply(_ context.Context, content string) error {
	_, err := a.session.ChannelMessageSendReply(a.message.ChannelID, content, a.reference())
	return err
}

func (a *DiscordChatAdapter) SendFailure(_ context.Context, _ error) error {
	_, err := a.session.ChannelMessageSendReply(
		a.message.ChannelID,
		"Vou precisar pensar mais sobre isso. Pode me perguntar novamente em alguns instantes?",
		a.reference(),
	)
	return err
}

func (a *DiscordChatAdapter) SendTimeout(context.Context) error {
	_, err := a.session.ChannelMessageSendReply(
		a.message.ChannelID,
		"Desculpe, não consegui processar sua solicitação a tempo. Tente novamente mais tarde.",
		a.reference(),
	)
	return err
}

func (a *DiscordChatAdapter) Cleanup(context.Context) error {
	if a.thinkingMsg != nil {
		if err := a.session.ChannelMessageDelete(a.message.ChannelID, a.thinkingMsg.ID); err != nil {
			logger.Error("Failed to delete thinking message:", err)
		}
		a.thinkingMsg = nil
	}

	if a.takingLongMsg != nil {
		if err := a.session.ChannelMessageDelete(a.message.ChannelID, a.takingLongMsg.ID); err != nil {
			logger.Error("Failed to delete taking-long message:", err)
		}
		a.takingLongMsg = nil
	}

	return nil
}

func (a *DiscordChatAdapter) reference() *discordgo.MessageReference {
	return &discordgo.MessageReference{
		MessageID: a.message.ID,
		ChannelID: a.message.ChannelID,
		GuildID:   a.message.GuildID,
	}
}
