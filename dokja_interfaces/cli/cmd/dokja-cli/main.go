package main

import (
	"context"
	"fmt"
	"os"

	"dokja_interfaces/cli/cli"
)

func main() {
	app := cli.NewApp()
	if err := app.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
