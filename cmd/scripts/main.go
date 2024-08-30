package main

import (
	"os"

	"github.com/iawia002/lux/app"
)

func main() {
	if err := app.New().Run(os.Args); err != nil {
		os.Exit(1)
	}
}
