package main

import (
	"fmt"
	"os"

	app "github.com/brianliu-sysu/uniswapv3/internal/app/migrate"
)

func main() {
	if err := app.Run(app.ParseFlags()); err != nil {
		fmt.Fprintf(os.Stderr, "migrate: %v\n", err)
		os.Exit(1)
	}
}
