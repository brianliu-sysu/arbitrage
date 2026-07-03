package memory

import (
	"context"
	"sync"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	"github.com/brianliu-sysu/uniswapv3/internal/infrastructure/persistence/codec"
	"github.com/ethereum/go-ethereum/common"
)

// PoolRepository is an in-memory PoolRepository for tests and local development.
type PoolRepository struct {
	mu    sync.RWMutex
	pools map[common.Address]*market.Pool
}

func NewPoolRepository() *PoolRepository {
	return &PoolRepository{pools: make(map[common.Address]*market.Pool)}
}

func (r *PoolRepository) Save(_ context.Context, pool *market.Pool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pools[pool.Address] = codec.ClonePool(pool)
	return nil
}

func (r *PoolRepository) Get(_ context.Context, address common.Address) (*market.Pool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return codec.ClonePool(r.pools[address]), nil
}

func (r *PoolRepository) Delete(_ context.Context, address common.Address) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.pools, address)
	return nil
}
