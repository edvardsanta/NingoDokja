package scraper

type Scraper interface {
	Fetch() (interface{}, error)
	Init(url string) error
	SetOption(...interface{})
}
