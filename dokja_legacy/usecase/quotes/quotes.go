package quotes

import (
	"fmt"
	"math/rand"
	scraper2 "read_books/internal/dokja_legacy/scraper/sites"
	"read_books/internal/utils"
	"time"
)

var (
	scraperQuotes scraper2.Scraper
)

func init() {
	useCase := utils.GetUsecaseFromPath()
	scraperQuotes = scraper2.NewScraper(useCase)
}

func GetQuotes() ([]string, error) {
	results, err := scraperQuotes.Fetch()
	if err != nil {
		return nil, fmt.Errorf("erro ao obter citações: %w", err)
	}
	resultList := results.([]string)

	if len(resultList) == 0 {
		return nil, fmt.Errorf("nenhuma citação encontrada")
	}

	return resultList, nil
}

func GetRandomQuote() (string, error) {
	quotes, err := GetQuotes()
	if err != nil {
		return "", err
	}

	randSrc := rand.New(rand.NewSource(time.Now().UnixNano()))
	quote := quotes[randSrc.Intn(len(quotes))]

	return quote, nil
}
