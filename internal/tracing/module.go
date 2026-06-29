package tracing

import (
	"context"
	"fmt"
	"time"

	"github.com/brianliu-sysu/arbitrage/internal/config"
	"github.com/brianliu-sysu/arbitrage/internal/logx"
	"go.uber.org/fx"
)

// Module 管理 tracing 生命周期。
var Module = fx.Module(
	"tracing",
	fx.Provide(newTracingRuntime),
	fx.Invoke(registerTracingLifecycle),
)

type tracingRuntime struct {
	shutdown ShutdownFunc
}

func newTracingRuntime() *tracingRuntime {
	return &tracingRuntime{
		shutdown: func(context.Context) error { return nil },
	}
}

func registerTracingLifecycle(lc fx.Lifecycle, cfg *config.AppConfig, logger logx.Logger, rt *tracingRuntime) {
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			shutdown, err := Init(Config{
				Endpoint:    cfg.TracingEndpoint,
				ServiceName: "arbitrage",
			})
			if err != nil {
				return fmt.Errorf("init tracing: %w", err)
			}
			rt.shutdown = shutdown
			logger.Info("tracing initialized")
			return nil
		},
		OnStop: func(ctx context.Context) error {
			stopCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			if err := rt.shutdown(stopCtx); err != nil {
				return fmt.Errorf("shutdown tracing: %w", err)
			}
			logger.Info("tracing stopped")
			return nil
		},
	})
}
