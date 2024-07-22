package bot

import (
	"read_books/internal/logger"
	scraper "read_books/internal/scraper/sites"

	"github.com/bwmarrin/discordgo"
)

func sendNews(s *discordgo.Session, channelID string) {
	scraper := scraper.NewScraper("c_news")

	results, err := scraper.GetResultsByXpath()
	if err != nil {
		logger.Error("Erro ao buscar por XPath", err)
		return
	}

	for _, newsUrl := range results {
		_, err = s.ChannelMessageSend(channelID, newsUrl)
		if err != nil {
			logger.Error("Erro ao enviar a imagem", err)
			return
		}
	}

	logger.Info("Noticias enviadas com sucesso.")
}
