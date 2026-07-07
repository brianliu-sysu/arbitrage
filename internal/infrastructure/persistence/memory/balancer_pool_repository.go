package memory

import (
	"context"
	"fmt"
	"sync"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
)

// BalancerPoolRepository is an in-memory marketbalancer.PoolRepository.
type BalancerPoolRepository struct {
	mu    sync.RWMutex
	pools map[marketbalancer.PoolID]*marketbalancer.Pool
}

func NewBalancerPoolRepository() *BalancerPoolRepository {
	return &BalancerPoolRepository{pools: make(map[marketbalancer.PoolID]*marketbalancer.Pool)}
}

func (r *BalancerPoolRepository) Save(_ context.Context, pool *marketbalancer.Pool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pools[pool.ID] = pool.Clone()
	return nil
}

func (r *BalancerPoolRepository) Get(_ context.Context, id marketbalancer.PoolID) (*marketbalancer.Pool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	pool, ok := r.pools[id]
	if !ok {
		return nil, nil
	}
	return pool.Clone(), nil
}

func (r *BalancerPoolRepository) Delete(_ context.Context, id marketbalancer.PoolID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.pools, id)
	return nil
}

func (r *BalancerPoolRepository) AdvanceSyncProgress(ctx context.Context, id marketbalancer.PoolID, blockNumber uint64) error {
	return r.AdvanceSyncProgressMany(ctx, []marketbalancer.PoolID{id}, blockNumber)
}

func (r *BalancerPoolRepository) AdvanceSyncProgressMany(_ context.Context, ids []marketbalancer.PoolID, blockNumber uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, id := range ids {
		pool, ok := r.pools[id]
		if !ok || pool == nil {
			return fmt.Errorf("pool %s not found", id)
		}
		if blockNumber > pool.LastBlockNumber {
			pool.LastBlockNumber = blockNumber
		}
		if pool.Status == market.PoolStatusCatchingUp {
			pool.Status = market.PoolStatusSyncing
		}
	}
	return nil
}
