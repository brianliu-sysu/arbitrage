package memory

import (
	"context"

	marketv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ3"
	"github.com/ethereum/go-ethereum/common"
)

// PoolRepository is an in-memory PoolRepository for tests and local development.
type PoolRepository struct {
	inner *CLV3PoolRepository
}

func NewPoolRepository() *PoolRepository {
	return &PoolRepository{inner: NewCLV3PoolRepository()}
}

func (r *PoolRepository) Save(ctx context.Context, pool *marketv3.Pool) error {
	if pool == nil {
		return nil
	}
	return r.inner.Save(ctx, pool.Pool.Clone())
}

func (r *PoolRepository) Get(ctx context.Context, address common.Address) (*marketv3.Pool, error) {
	pool, err := r.inner.Get(ctx, address)
	if pool == nil {
		return nil, err
	}
	return &marketv3.Pool{Pool: *pool}, nil
}

func (r *PoolRepository) Delete(ctx context.Context, address common.Address) error {
	return r.inner.Delete(ctx, address)
}

func (r *PoolRepository) AdvanceSyncProgress(ctx context.Context, address common.Address, blockNumber uint64) error {
	return r.inner.AdvanceSyncProgress(ctx, address, blockNumber)
}

func (r *PoolRepository) AdvanceSyncProgressMany(ctx context.Context, addresses []common.Address, blockNumber uint64) error {
	return r.inner.AdvanceSyncProgressMany(ctx, addresses, blockNumber)
}
