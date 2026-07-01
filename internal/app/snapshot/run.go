package snapshot

import (
	"context"
	"fmt"

	"github.com/brianliu-sysu/arbitrage/internal/config"
	"github.com/brianliu-sysu/arbitrage/internal/logx"
	poolsnapshot "github.com/brianliu-sysu/arbitrage/internal/pool/snapshot"
	"go.uber.org/fx"
)

type runParams struct {
	fx.In

	Lifecycle   fx.Lifecycle
	Shutdowner  fx.Shutdowner
	Config      *config.AppConfig
	Logger      logx.Logger
	Runner      *poolsnapshot.Runner
	ChainFilter string `name:"chain_filter"`
}

// registerRun 在 Fx OnStart 中执行快照并在完成后退出进程。
func registerRun(p runParams) {
	p.Lifecycle.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			if p.Config.DBURL == "" {
				return fmt.Errorf("db_url is required in config for snapshot")
			}
			if err := p.Runner.Run(ctx, p.ChainFilter); err != nil {
				return fmt.Errorf("snapshot: %w", err)
			}
			p.Logger.Info("snapshot completed successfully")
			return p.Shutdowner.Shutdown()
		},
	})
}
