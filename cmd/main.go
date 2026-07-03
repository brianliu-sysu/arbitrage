package main

import (
	"github.com/brianliu-sysu/uniswapv3/internal/app"
	"go.uber.org/fx"
)

func main() {
	params := app.ParseFlags()
	fx.New(app.Module(params)).Run()
}
