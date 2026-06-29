package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/brianliu-sysu/arbitrage/internal/config"
	"github.com/brianliu-sysu/arbitrage/internal/logx"
	"go.uber.org/fx"
)

// Module 提供 HTTP API 服务及生命周期。
var Module = fx.Module(
	"httpapi",
	fx.Provide(newServer),
	fx.Invoke(registerHTTPServerLifecycle),
)

func newServer(cfg *config.AppConfig, quoteSvc QuoteProvider, logger logx.Logger) *Server {
	addr := fmt.Sprintf(":%d", cfg.HTTPPort)
	return NewServer(addr, quoteSvc, nil, logger, cfg.HTTPRateLimit, cfg.APIKey)
}

func registerHTTPServerLifecycle(lc fx.Lifecycle, cfg *config.AppConfig, logger logx.Logger, srv *Server) {
	if cfg.HTTPPort <= 0 {
		logger.Info("http server disabled")
		return
	}

	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			go func() {
				if err := srv.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
					logger.Error("http server stopped unexpectedly", "error", err)
				}
			}()
			logger.Info("HTTP API listening", "addr", fmt.Sprintf("0.0.0.0:%d", cfg.HTTPPort))
			return nil
		},
		OnStop: func(ctx context.Context) error {
			stopCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			return srv.ShutdownGraceful(stopCtx)
		},
	})
}
