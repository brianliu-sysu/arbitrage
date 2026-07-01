package snapshot

import (
	"github.com/brianliu-sysu/arbitrage/internal/config"
	"github.com/brianliu-sysu/arbitrage/internal/logx"
	"github.com/brianliu-sysu/arbitrage/internal/storage/postgres"
	"go.uber.org/fx"
)

// Module 提供快照 Runner 依赖。
var Module = fx.Module(
	"pool.snapshot",
	fx.Provide(newRunner),
)

func newRunner(cfg *config.AppConfig, repos *postgres.Repositories, logger logx.Logger) *Runner {
	return NewRunner(cfg, repos.Pool, logger)
}
