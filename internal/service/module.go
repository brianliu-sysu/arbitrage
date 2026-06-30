package service

import (
	"context"

	"github.com/brianliu-sysu/arbitrage/internal/api"
	"github.com/brianliu-sysu/arbitrage/internal/logx"
	"go.uber.org/fx"
)

// Module 提供多链服务与生命周期。
var Module = fx.Module(
	"service",
	fx.Provide(newMultiChain),
	fx.Provide(
		fx.Annotate(
			func(m *MultiChainService) api.QuoteProvider { return m },
			fx.As(new(api.QuoteProvider)),
		),
	),
	fx.Invoke(registerServiceLifecycle),
)

func newMultiChain(logger logx.Logger) *MultiChainService {
	return NewMultiChainService(logger)
}

func registerServiceLifecycle(lc fx.Lifecycle, multiChain *MultiChainService) {
	lc.Append(fx.Hook{
		OnStop: func(context.Context) error {
			multiChain.StopAll()
			return nil
		},
	})
}
