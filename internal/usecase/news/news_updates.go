package news

import (
	"read_books/internal/logger"
	scraper "read_books/internal/scraper/sites"

	"github.com/bwmarrin/discordgo"
)

func SendNews(s *discordgo.Session, channelID string) error {
	scraper := scraper.NewScraper("c_news")

	results, err := scraper.GetResultsByXpath()
	if err != nil {
		logger.Error("Erro ao buscar por XPath", err)
		return err
	}

	for _, newsUrl := range results {
		_, err = s.ChannelMessageSend(channelID, newsUrl)
		if err != nil {
			logger.Error("Erro ao enviar a notícia", err)
			return err
		}
	}

	logger.Info("Notícias enviadas com sucesso.")
	return nil
}
