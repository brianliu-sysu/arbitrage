package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	app "github.com/brianliu-sysu/uniswapv3/internal/app/arbitrage"
)

func main() {
	application, err := app.New(app.ParseFlags())
	if err != nil {
		fmt.Fprintf(os.Stderr, "arbitrage: %v\n", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := application.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "arbitrage: %v\n", err)
		os.Exit(1)
	}
}
