package memes

import (
	"fmt"
	scraper2 "read_books/internal/dokja_legacy/scraper/sites"
	"read_books/internal/logger"
)

type UseCase struct {
	source scraper2.Scraper
}

func NewUseCase(source scraper2.Scraper) *UseCase {
	return &UseCase{source: source}
}

func NewDefaultUseCase() *UseCase {
	return NewUseCase(scraper2.NewScraper("memes"))
}

func (u *UseCase) Execute() ([]string, error) {
	if u == nil || u.source == nil {
		return nil, fmt.Errorf("meme source is not configured")
	}

	if err := u.source.Init(""); err != nil {
		return nil, err
	}

	results, err := u.source.Fetch()
	if err != nil {
		logger.Error("Erro ao buscar memes", err)
		return nil, err
	}

	links, ok := results.([]string)
	if !ok {
		return nil, fmt.Errorf("unexpected meme result type %T", results)
	}

	return links, nil
}
