package news

import (
	"fmt"
	"read_books/internal/legacy/scraper/sites"
	"read_books/internal/logger"
)

type UseCase struct {
	source scraper.Scraper
}

func NewUseCase(source scraper.Scraper) *UseCase {
	return &UseCase{source: source}
}

func NewDefaultUseCase() *UseCase {
	return NewUseCase(scraper.scraper.NewScraper("news"))
}

func (u *UseCase) Execute() ([]string, error) {
	if u == nil || u.source == nil {
		return nil, fmt.Errorf("news source is not configured")
	}

	if err := u.source.Init(""); err != nil {
		return nil, err
	}

	results, err := u.source.Fetch()
	if err != nil {
		logger.Error("Erro ao buscar por XPath", err)
		return nil, err
	}

	links, ok := results.([]string)
	if !ok {
		return nil, fmt.Errorf("unexpected news result type %T", results)
	}

	return links, nil
}
