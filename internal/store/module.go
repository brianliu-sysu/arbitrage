package store

import (
	"context"
	"fmt"

	"github.com/brianliu-sysu/arbitrage/internal/config"
	"github.com/brianliu-sysu/arbitrage/internal/logx"
	"go.uber.org/fx"
)

// Module 提供持久化依赖。
var Module = fx.Module(
	"store",
	fx.Provide(newStorer),
	fx.Invoke(registerStoreLifecycle),
)

type storeRuntime struct {
	cfg *config.AppConfig
	log logx.Logger
	st  Storer
}

func newStorer(cfg *config.AppConfig, logger logx.Logger) (Storer, *storeRuntime, error) {
	if cfg.DBURL == "" {
		st := NewNoopStore()
		return st, &storeRuntime{cfg: cfg, log: logger, st: st}, nil
	}

	st, err := NewPostgresStore(context.Background(), cfg.DBURL)
	if err != nil {
		return nil, nil, fmt.Errorf("connect postgres: %w", err)
	}
	return st, &storeRuntime{cfg: cfg, log: logger, st: st}, nil
}

func registerStoreLifecycle(lc fx.Lifecycle, rt *storeRuntime) {
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			if rt.cfg.DBURL == "" {
				rt.log.Info("database disabled")
				return nil
			}
			if err := RunMigrations(rt.cfg.DBURL); err != nil {
				return fmt.Errorf("run migrations: %w", err)
			}
			rt.log.Info("database connected and migrated")
			return nil
		},
		OnStop: func(context.Context) error {
			if rt.st != nil {
				rt.st.Close()
			}
			return nil
		},
	})
}
