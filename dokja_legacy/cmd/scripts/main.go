package main

import (
	"read_books/internal/legacy/scraper/sites"
)

func main() {
	runRedis()
	//if err := app.New().Run(os.Args); err != nil {
	//	os.Exit(1)
	//}
}

func runRedis() {

}

func runScraper() {
	var meme = scraper.MemedroidScraper{}
	fetch, err := meme.Fetch()
	if err != nil {
		return
	}
	for _, v := range fetch.([]string) {
		println(v)
	}
}
