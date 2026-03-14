package bot

import (
	"context"
	chatservice "read_books/internal/dokja_legacy/services/chat"
	"read_books/internal/legacy/config"
	"read_books/internal/logger"
	"read_books/internal/orchestrator"

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

	chatOrchestrator := orchestrator.NewChatOrchestrator(
		chatservice.NewZMQResponder(config.AppConfig.QueueConfig.Addr),
		orchestrator.DefaultChatOrchestratorConfig(),
	)
	discordAdapter := NewDiscordChatAdapter(s, m)

	if err := chatOrchestrator.Handle(context.Background(), m.Content, m.Author.ID, discordAdapter); err != nil {
		logger.Error("Error handling chat orchestration:", err)
	}
}
