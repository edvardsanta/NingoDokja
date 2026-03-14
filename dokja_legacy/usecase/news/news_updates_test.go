package news

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
		fetchValue: []string{"https://example.com/1", "https://example.com/2"},
	})

	links, err := useCase.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(links) != 2 {
		t.Fatalf("expected 2 links, got %d", len(links))
	}
}

func TestUseCaseExecuteRejectsUnexpectedType(t *testing.T) {
	useCase := NewUseCase(&fakeScraper{
		fetchValue: "invalid",
	})

	_, err := useCase.Execute()
	if err == nil {
		t.Fatal("expected error for unexpected result type")
	}
}

func TestUseCaseExecutePropagatesInitError(t *testing.T) {
	expectedErr := errors.New("boom")
	useCase := NewUseCase(&fakeScraper{
		initErr: expectedErr,
	})

	_, err := useCase.Execute()
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected init error %v, got %v", expectedErr, err)
	}
}
