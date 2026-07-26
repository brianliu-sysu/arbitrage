package chainruntime

import (
	"context"
	"time"

	"go.uber.org/zap"
)

type poolLister[PoolID comparable] interface {
	List(context.Context) ([]PoolID, error)
}

type discoveredPoolLifecycle[PoolID comparable] interface {
	ListActive() []PoolID
	AddPool(context.Context, PoolID) error
}

func (r *syncLifecycle) runSubgraphDiscoveryWatchers() {
	for _, module := range r.runtime.protocols.modules {
		module.StartDiscovery(r, r.runtime.cfg)
	}
}

func runSubgraphDiscovery[PoolID comparable](r *syncLifecycle, name string, interval time.Duration, enabled bool, registry poolLister[PoolID], lifecycle discoveredPoolLifecycle[PoolID]) {
	if !enabled || registry == nil || lifecycle == nil {
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
				reconcileSubgraphPools(r, name, registry, lifecycle)
			}
		}
	})
}

func reconcileSubgraphPools[PoolID comparable](r *syncLifecycle, name string, registry poolLister[PoolID], lifecycle discoveredPoolLifecycle[PoolID]) {
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
	for _, id := range tracked {
		if _, ok := activeSet[id]; ok {
			continue
		}
		r.logger.Debug("subgraph pool discovered", zap.String("protocol", name), zap.Any("pool", id))
		if err := lifecycle.AddPool(r.runCtx, id); err != nil {
			r.logger.Warn("subgraph pool onboarding failed", zap.String("protocol", name), zap.Any("pool", id), zap.Error(err))
			continue
		}
		r.logger.Debug("subgraph pool activated", zap.String("protocol", name), zap.Any("pool", id))
	}
}
