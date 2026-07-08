package memory

import (
	"context"

	marketquick "github.com/brianliu-sysu/uniswapv3/internal/domain/market/quickswapv3"
	"github.com/ethereum/go-ethereum/common"
)

// QuickSwapPoolRepository is an in-memory QuickSwap V3 PoolRepository.
type QuickSwapPoolRepository struct {
	inner *CLV3PoolRepository
}

func NewQuickSwapPoolRepository() *QuickSwapPoolRepository {
	return &QuickSwapPoolRepository{inner: NewCLV3PoolRepository()}
}

func (r *QuickSwapPoolRepository) Save(ctx context.Context, pool *marketquick.Pool) error {
	if pool == nil {
		return nil
	}
	return r.inner.Save(ctx, pool.Pool.Clone())
}

func (r *QuickSwapPoolRepository) Get(ctx context.Context, address common.Address) (*marketquick.Pool, error) {
	pool, err := r.inner.Get(ctx, address)
	if pool == nil {
		return nil, err
	}
	return &marketquick.Pool{Pool: *pool}, nil
}

func (r *QuickSwapPoolRepository) Delete(ctx context.Context, address common.Address) error {
	return r.inner.Delete(ctx, address)
}

func (r *QuickSwapPoolRepository) AdvanceSyncProgress(ctx context.Context, address common.Address, blockNumber uint64) error {
	return r.inner.AdvanceSyncProgress(ctx, address, blockNumber)
}

func (r *QuickSwapPoolRepository) AdvanceSyncProgressMany(ctx context.Context, addresses []common.Address, blockNumber uint64) error {
	return r.inner.AdvanceSyncProgressMany(ctx, addresses, blockNumber)
}
