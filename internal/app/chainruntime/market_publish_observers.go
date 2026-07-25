package chainruntime

import (
	"context"

	"github.com/brianliu-sysu/uniswapv3/internal/application/marketstore"
	"go.uber.org/zap"
)

type arbitrageRouteObserver struct {
	runtime *chainRuntime
	logger  *zap.Logger
}

func (o *arbitrageRouteObserver) AfterMarketPublished(ctx context.Context, publication marketstore.Publication) {
	if !publication.TopologyChanged || o.runtime == nil || o.runtime.Arbitrage == nil {
		return
	}
	routes, err := o.runtime.Arbitrage.RefreshArbitrageRoutes(ctx)
	if err != nil {
		o.logger.Warn("refresh arbitrage routes after market topology change failed",
			zap.Uint64("block", publication.Version.Number),
			zap.Uint64("topology_version", publication.TopologyVersion),
			zap.Error(err),
		)
		return
	}
	o.logger.Info("arbitrage routes refreshed after market topology change",
		zap.Uint64("block", publication.Version.Number),
		zap.Uint64("topology_version", publication.TopologyVersion),
		zap.Int("routes", routes),
	)
}

func configureMarketPublishObservers(runtime *chainRuntime, logger *zap.Logger) {
	if runtime == nil || runtime.MarketStore == nil {
		return
	}
	observers := make([]marketstore.PublishObserver, 0, 2)
	if runtime.cfg.ArbitrageEnabled() {
		observers = append(observers,
			&arbitrageRouteObserver{runtime: runtime, logger: logger.Named("market-topology")},
		)
	}
	if runtime.resources != nil &&
		runtime.resources.stores != nil &&
		runtime.resources.stores.hasSeparateRuntime() {
		observers = append(observers, &marketPersistenceObserver{
			runtime: runtime,
			logger:  logger.Named("market-persistence"),
		})
	}
	runtime.MarketStore.SetPublishObservers(observers...)
}
