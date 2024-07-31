package bot

import (
	"read_books/internal/usecase/news"
	"read_books/internal/usecase/olympics"

	"github.com/bwmarrin/discordgo"
)

func sendNews(s *discordgo.Session, channelID string) {
	news.SendNews(s, channelID)
}

func sendOlympicUpdates(session *discordgo.Session, runningChannelID string, finishedChannelID string) {
	olympics.SendOlympicUpdates(session, runningChannelID, finishedChannelID)
}
