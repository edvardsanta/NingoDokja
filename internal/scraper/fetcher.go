package fetcher

import (
	"fmt"
	"net/http"

	"github.com/antchfx/htmlquery"
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
