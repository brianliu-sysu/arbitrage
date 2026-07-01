package snapshot

import (
	"github.com/brianliu-sysu/arbitrage/internal/config"
	"github.com/brianliu-sysu/arbitrage/internal/logx"
	poolsnapshot "github.com/brianliu-sysu/arbitrage/internal/pool/snapshot"
	"github.com/brianliu-sysu/arbitrage/internal/storage/postgres"
	"go.uber.org/fx"
)

// New 构建 snapshot 一次性任务的 Fx 容器（config → storage → snapshot runner）。
func New(configPath, chainFilter string) *fx.App {
	return fx.New(
		fx.Supply(
			fx.Annotate(configPath, fx.ResultTags(`name:"config_path"`)),
			fx.Annotate(chainFilter, fx.ResultTags(`name:"chain_filter"`)),
		),
		config.Module,
		logx.Module,
		postgres.Module,
		poolsnapshot.Module,
		fx.Invoke(registerRun),
	)
}
