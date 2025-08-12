package memes

import (
	"github.com/bwmarrin/discordgo"
	"read_books/internal/logger"
	scraper "read_books/internal/scraper/sites"
	"read_books/internal/utils"
)

var (
	scraperCase scraper.Scraper
)

func init() {
	useCase := utils.GetUsecaseFromPath()
	scraperCase = scraper.NewScraper(useCase)
	scraperCase.Init("")
}

func SendMemes(s *discordgo.Session, channelID string) error {
	results, err := scraperCase.Fetch()
	if err != nil {
		logger.Error("Erro ao buscar memes", err)
		return err
	}

	for _, memesUrl := range results.([]string) {
		_, err = s.ChannelMessageSend(channelID, memesUrl)

		if err != nil {
			logger.Error("Erro ao enviar um meme", err)
			return err
		}
	}

	logger.Info("Memes enviados com sucesso.")
	return nil
}
