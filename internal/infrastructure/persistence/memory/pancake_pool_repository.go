package memory

import (
	"context"

	marketpancake "github.com/brianliu-sysu/uniswapv3/internal/domain/market/pancakev3"
	"github.com/ethereum/go-ethereum/common"
)

// PancakePoolRepository is an in-memory PancakeSwap V3 PoolRepository.
type PancakePoolRepository struct {
	inner *CLV3PoolRepository
}

func NewPancakePoolRepository() *PancakePoolRepository {
	return &PancakePoolRepository{inner: NewCLV3PoolRepository()}
}

func (r *PancakePoolRepository) Save(ctx context.Context, pool *marketpancake.Pool) error {
	if pool == nil {
		return nil
	}
	return r.inner.Save(ctx, pool.Pool.Clone())
}

func (r *PancakePoolRepository) Get(ctx context.Context, address common.Address) (*marketpancake.Pool, error) {
	pool, err := r.inner.Get(ctx, address)
	if pool == nil {
		return nil, err
	}
	return &marketpancake.Pool{Pool: *pool}, nil
}

func (r *PancakePoolRepository) Delete(ctx context.Context, address common.Address) error {
	return r.inner.Delete(ctx, address)
}

func (r *PancakePoolRepository) AdvanceSyncProgress(ctx context.Context, address common.Address, blockNumber uint64) error {
	return r.inner.AdvanceSyncProgress(ctx, address, blockNumber)
}

func (r *PancakePoolRepository) AdvanceSyncProgressMany(ctx context.Context, addresses []common.Address, blockNumber uint64) error {
	return r.inner.AdvanceSyncProgressMany(ctx, addresses, blockNumber)
}
