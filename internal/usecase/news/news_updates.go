package news

import (
	"read_books/internal/logger"
	"read_books/internal/scraper/sites"
	"read_books/internal/utils"

	"github.com/bwmarrin/discordgo"
)

var (
	scraperNews scraper.Scraper
)

func init() {
	useCase := utils.GetUsecaseFromPath()
	scraperNews = scraper.NewScraper(useCase)
}

func SendNews(s *discordgo.Session, channelID string) error {
	results, err := scraperNews.Fetch()
	if err != nil {
		logger.Error("Erro ao buscar por XPath", err)
		return err
	}

	for _, newsUrl := range results.([]string) {
		_, err = s.ChannelMessageSend(channelID, newsUrl)
		if err != nil {
			logger.Error("Erro ao enviar a notícia", err)
			return err
		}
	}

	logger.Info("Notícias enviadas com sucesso.")
	return nil
}
