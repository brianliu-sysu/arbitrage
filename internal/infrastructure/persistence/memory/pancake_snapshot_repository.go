package memory

import (
	"context"

	marketpancake "github.com/brianliu-sysu/uniswapv3/internal/domain/market/pancakev3"
	"github.com/ethereum/go-ethereum/common"
)

// PancakeSnapshotRepository is an in-memory PancakeSwap V3 SnapshotRepository.
type PancakeSnapshotRepository struct {
	inner *CLV3SnapshotRepository
}

func NewPancakeSnapshotRepository() *PancakeSnapshotRepository {
	return &PancakeSnapshotRepository{inner: NewCLV3SnapshotRepository()}
}

func (r *PancakeSnapshotRepository) Save(ctx context.Context, snapshot *marketpancake.Snapshot) error {
	return r.inner.Save(ctx, snapshot)
}

func (r *PancakeSnapshotRepository) GetLatest(ctx context.Context, poolAddress common.Address) (*marketpancake.Snapshot, error) {
	return r.inner.GetLatest(ctx, poolAddress)
}

func (r *PancakeSnapshotRepository) GetAtBlock(ctx context.Context, poolAddress common.Address, blockNumber uint64) (*marketpancake.Snapshot, error) {
	return r.inner.GetAtBlock(ctx, poolAddress, blockNumber)
}

func (r *PancakeSnapshotRepository) DeleteAfterBlock(ctx context.Context, poolAddress common.Address, blockNumber uint64) error {
	return r.inner.DeleteAfterBlock(ctx, poolAddress, blockNumber)
}
