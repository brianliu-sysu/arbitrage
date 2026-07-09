package poolmanager

import (
	"context"
	"fmt"
	"sync"
)

// SyncOnboarder adds a pool to protocol sync and marks it ready after catchup.
type SyncOnboarder[PoolID comparable] interface {
	AddPool(ctx context.Context, poolID PoolID) error
}

// ArbitrageRefresher rebuilds arbitrage routes after pool topology changes.
type ArbitrageRefresher interface {
	RefreshArbitrageRoutes(ctx context.Context) (int, error)
}

// AddPoolResult summarizes the route refresh performed after onboarding.
type AddPoolResult struct {
	Routes int
}

// PoolManager coordinates hot pool onboarding across sync and arbitrage.
type PoolManager[PoolID comparable] struct {
	sync      SyncOnboarder[PoolID]
	arbitrage ArbitrageRefresher
	mu        sync.Mutex
}

func NewPoolManager[PoolID comparable](
	sync SyncOnboarder[PoolID],
	arbitrage ArbitrageRefresher,
) *PoolManager[PoolID] {
	return &PoolManager[PoolID]{
		sync:      sync,
		arbitrage: arbitrage,
	}
}

// AddPool registers, bootstraps, catches up, marks ready, then refreshes arbitrage routes.
func (m *PoolManager[PoolID]) AddPool(ctx context.Context, poolID PoolID) (AddPoolResult, error) {
	if m == nil {
		return AddPoolResult{}, fmt.Errorf("pool manager is nil")
	}
	if m.sync == nil {
		return AddPoolResult{}, fmt.Errorf("sync onboarder is not configured")
	}
	if m.arbitrage == nil {
		return AddPoolResult{}, fmt.Errorf("arbitrage refresher is not configured")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.sync.AddPool(ctx, poolID); err != nil {
		return AddPoolResult{}, fmt.Errorf("sync add pool: %w", err)
	}
	routes, err := m.arbitrage.RefreshArbitrageRoutes(ctx)
	if err != nil {
		return AddPoolResult{}, fmt.Errorf("refresh arbitrage routes: %w", err)
	}
	return AddPoolResult{Routes: routes}, nil
}
