package app

import (
	"github.com/brianliu-sysu/arbitrage/internal/app/bootstrap"
	"github.com/brianliu-sysu/arbitrage/internal/cache"
	"github.com/brianliu-sysu/arbitrage/internal/config"
	"github.com/brianliu-sysu/arbitrage/internal/httpapi"
	"github.com/brianliu-sysu/arbitrage/internal/logx"
	"github.com/brianliu-sysu/arbitrage/internal/service"
	"github.com/brianliu-sysu/arbitrage/internal/store"
	"github.com/brianliu-sysu/arbitrage/internal/tracing"
	"go.uber.org/fx"
)

// New 构建应用 Fx 容器。
func New(configPath string) *fx.App {
	return fx.New(
		fx.Supply(
			fx.Annotate(configPath, fx.ResultTags(`name:"config_path"`)),
		),
		config.Module,
		logx.Module,
		tracing.Module,
		store.Module,
		cache.Module,
		service.Module,
		httpapi.Module,
		fx.Invoke(bootstrap.RegisterChains),
	)
}
