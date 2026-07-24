package runtime

import (
	"context"
	"time"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync/protocol"
	"go.uber.org/zap"
)

type poolLister[PoolID comparable] interface {
	List(context.Context) ([]PoolID, error)
}

type poolOnboarder[PoolID comparable] interface {
	AddPool(context.Context, PoolID) error
}

func (r *syncLifecycle) runSubgraphDiscoveryWatchers() {
	for _, module := range r.runtime.protocols.modules {
		module.StartDiscovery(r, r.runtime.cfg, r.runtime.resources.protocols)
	}
}

func runSubgraphDiscovery[PoolID comparable](r *syncLifecycle, name string, interval time.Duration, enabled bool, registry poolLister[PoolID], lifecycle *syncapp.PoolLifecycleService[PoolID], onboarder poolOnboarder[PoolID]) {
	if !enabled || registry == nil || lifecycle == nil || onboarder == nil {
		return
	}
	if interval <= 0 {
		interval = 10 * time.Minute
	}
	r.startSafeGoroutine("subgraph-"+name, func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-r.runCtx.Done():
				return
			case <-ticker.C:
				reconcileSubgraphPools(r, name, registry, lifecycle, onboarder)
			}
		}
	})
}

func reconcileSubgraphPools[PoolID comparable](r *syncLifecycle, name string, registry poolLister[PoolID], lifecycle *syncapp.PoolLifecycleService[PoolID], onboarder poolOnboarder[PoolID]) {
	started := time.Now()
	active := lifecycle.ListActive()
	tracked, err := registry.List(r.runCtx)
	if err != nil {
		r.logger.Warn("subgraph pool refresh failed", zap.String("protocol", name), zap.Error(err), zap.Int64("duration_ms", time.Since(started).Milliseconds()))
		return
	}
	activeSet := make(map[PoolID]struct{}, len(active))
	for _, id := range active {
		activeSet[id] = struct{}{}
	}
	added := 0
	for _, id := range tracked {
		if _, ok := activeSet[id]; ok {
			continue
		}
		r.logger.Debug("subgraph pool discovered", zap.String("protocol", name), zap.Any("pool", id))
		if err := onboarder.AddPool(r.runCtx, id); err != nil {
			r.logger.Warn("subgraph pool onboarding failed", zap.String("protocol", name), zap.Any("pool", id), zap.Error(err))
			continue
		}
		added++
		r.logger.Debug("subgraph pool activated", zap.String("protocol", name), zap.Any("pool", id))
	}
	if added > 0 && r.runtime != nil && r.runtime.Arbitrage != nil {
		if routes, err := r.runtime.Arbitrage.RefreshArbitrageRoutes(r.runCtx); err != nil {
			r.logger.Warn("refresh arbitrage routes after subgraph update failed", zap.String("protocol", name), zap.Error(err))
		} else {
			r.logger.Info("arbitrage routes refreshed after subgraph update", zap.String("protocol", name), zap.Int("new_pools", added), zap.Int("routes", routes))
		}
	}
}
