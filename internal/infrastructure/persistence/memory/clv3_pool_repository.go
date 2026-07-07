package memory

import (
	"context"
	"fmt"
	"sync"

	marketclv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/clv3"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	"github.com/ethereum/go-ethereum/common"
)

// CLV3PoolRepository is an in-memory pool store for CLV3-style pools.
type CLV3PoolRepository struct {
	mu    sync.RWMutex
	pools map[common.Address]*marketclv3.Pool
}

func NewCLV3PoolRepository() *CLV3PoolRepository {
	return &CLV3PoolRepository{pools: make(map[common.Address]*marketclv3.Pool)}
}

func (r *CLV3PoolRepository) Save(_ context.Context, pool *marketclv3.Pool) error {
	if pool == nil {
		return fmt.Errorf("pool is nil")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pools[pool.Address] = pool.Clone()
	return nil
}

func (r *CLV3PoolRepository) Get(_ context.Context, address common.Address) (*marketclv3.Pool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	pool := r.pools[address]
	if pool == nil {
		return nil, nil
	}
	return pool.Clone(), nil
}

func (r *CLV3PoolRepository) Delete(_ context.Context, address common.Address) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.pools, address)
	return nil
}

func (r *CLV3PoolRepository) AdvanceSyncProgress(ctx context.Context, address common.Address, blockNumber uint64) error {
	return r.AdvanceSyncProgressMany(ctx, []common.Address{address}, blockNumber)
}

func (r *CLV3PoolRepository) AdvanceSyncProgressMany(_ context.Context, addresses []common.Address, blockNumber uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, address := range addresses {
		pool, ok := r.pools[address]
		if !ok || pool == nil {
			return fmt.Errorf("pool %s not found", address.Hex())
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
