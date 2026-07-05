package memory

import (
	"context"
	"fmt"
	"sync"

	marketpancake "github.com/brianliu-sysu/uniswapv3/internal/domain/market/pancakev3"
	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	"github.com/brianliu-sysu/uniswapv3/internal/infrastructure/persistence/codec"
	"github.com/ethereum/go-ethereum/common"
)

// PancakePoolRepository is an in-memory PancakeSwap V3 PoolRepository.
type PancakePoolRepository struct {
	mu    sync.RWMutex
	pools map[common.Address]*marketpancake.Pool
}

func NewPancakePoolRepository() *PancakePoolRepository {
	return &PancakePoolRepository{pools: make(map[common.Address]*marketpancake.Pool)}
}

func (r *PancakePoolRepository) Save(_ context.Context, pool *marketpancake.Pool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pools[pool.Address] = codec.ClonePancakePool(pool)
	return nil
}

func (r *PancakePoolRepository) Get(_ context.Context, address common.Address) (*marketpancake.Pool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return codec.ClonePancakePool(r.pools[address]), nil
}

func (r *PancakePoolRepository) Delete(_ context.Context, address common.Address) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.pools, address)
	return nil
}

func (r *PancakePoolRepository) AdvanceSyncProgress(ctx context.Context, address common.Address, blockNumber uint64) error {
	return r.AdvanceSyncProgressMany(ctx, []common.Address{address}, blockNumber)
}

func (r *PancakePoolRepository) AdvanceSyncProgressMany(_ context.Context, addresses []common.Address, blockNumber uint64) error {
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
