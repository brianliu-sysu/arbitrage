package main

import (
	app "github.com/brianliu-sysu/uniswapv3/internal/app/arbitrage"
	"go.uber.org/fx"
)

func main() {
	params := app.ParseFlags()
	fx.New(app.Module(params)).Run()
}
