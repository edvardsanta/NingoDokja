package memes

import (
	"errors"
	"testing"
)

type fakeScraper struct {
	initErr    error
	fetchErr   error
	fetchValue interface{}
}

func (f *fakeScraper) Init(string) error {
	return f.initErr
}

func (f *fakeScraper) Fetch() (interface{}, error) {
	if f.fetchErr != nil {
		return nil, f.fetchErr
	}

	return f.fetchValue, nil
}

func (f *fakeScraper) SetOption(...interface{}) {}

func TestUseCaseExecuteReturnsLinks(t *testing.T) {
	useCase := NewUseCase(&fakeScraper{
		fetchValue: []string{"https://example.com/meme-1"},
	})

	links, err := useCase.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(links))
	}
}

func TestUseCaseExecuteRejectsUnexpectedType(t *testing.T) {
	useCase := NewUseCase(&fakeScraper{
		fetchValue: 42,
	})

	_, err := useCase.Execute()
	if err == nil {
		t.Fatal("expected error for unexpected result type")
	}
}

func TestUseCaseExecutePropagatesFetchError(t *testing.T) {
	expectedErr := errors.New("boom")
	useCase := NewUseCase(&fakeScraper{
		fetchErr: expectedErr,
	})

	_, err := useCase.Execute()
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected fetch error %v, got %v", expectedErr, err)
	}
}
