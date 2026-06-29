package logx

import (
	"context"

	"github.com/brianliu-sysu/arbitrage/internal/config"
	"go.uber.org/fx"
)

// Module 提供日志依赖与关闭钩子。
var Module = fx.Module(
	"logx",
	fx.Provide(newLogger),
	fx.Invoke(registerLoggerLifecycle),
)

func newLogger(cfg *config.AppConfig) (Logger, error) {
	return NewWithFile(cfg.LogFile, cfg.LogLevel)
}

func registerLoggerLifecycle(lc fx.Lifecycle, logger Logger) {
	lc.Append(fx.Hook{
		OnStop: func(context.Context) error {
			return logger.Close()
		},
	})
}
