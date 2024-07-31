package fetcher

import (
	"fmt"
	"net/http"
	"time"

	"github.com/antchfx/htmlquery"
	"github.com/go-resty/resty/v2"
	"golang.org/x/net/html"
)

// FetchHTML obtém o HTML de uma URL e retorna como um documento html.Node.
func FetchHTML(url string) (*html.Node, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("erro ao fazer o GET da URL: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("erro ao obter resposta da URL, status: %d", resp.StatusCode)
	}

	doc, err := htmlquery.Parse(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("erro ao ler o HTML: %v", err)
	}

	return doc, nil
}

func FetchJson(url string, target interface{}) error {
	client := resty.New()
	client.SetTimeout(10 * time.Second)

	resp, err := client.R().
		SetHeader("Accept", "application/json").
		SetHeader("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/127.0.0.0 Safari/537.36").
		SetResult(target).
		Get(url)
	if err != nil {
		return fmt.Errorf("erro ao fazer o GET da URL: %v", err)
	}

	if resp.IsError() {
		return fmt.Errorf("received non-200 response: %d", resp.StatusCode())
	}
	return nil
}
