package memory

import (
	"context"
	"fmt"
	"sync"

	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/v4"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
)

// V4PoolRepository is an in-memory marketv4.PoolRepository.
type V4PoolRepository struct {
	mu    sync.RWMutex
	pools map[marketv4.PoolID]*marketv4.Pool
}

func NewV4PoolRepository() *V4PoolRepository {
	return &V4PoolRepository{pools: make(map[marketv4.PoolID]*marketv4.Pool)}
}

func (r *V4PoolRepository) Save(_ context.Context, pool *marketv4.Pool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pools[pool.ID] = pool.Clone()
	return nil
}

func (r *V4PoolRepository) Get(_ context.Context, id marketv4.PoolID) (*marketv4.Pool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	pool, ok := r.pools[id]
	if !ok {
		return nil, nil
	}
	return pool.Clone(), nil
}

func (r *V4PoolRepository) Delete(_ context.Context, id marketv4.PoolID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.pools, id)
	return nil
}

func (r *V4PoolRepository) AdvanceSyncProgress(ctx context.Context, id marketv4.PoolID, blockNumber uint64) error {
	return r.AdvanceSyncProgressMany(ctx, []marketv4.PoolID{id}, blockNumber)
}

func (r *V4PoolRepository) AdvanceSyncProgressMany(_ context.Context, ids []marketv4.PoolID, blockNumber uint64) error {
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
