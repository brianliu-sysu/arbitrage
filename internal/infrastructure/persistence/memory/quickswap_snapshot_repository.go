package memory

import (
	"context"

	marketquick "github.com/brianliu-sysu/uniswapv3/internal/domain/market/quickswapv3"
	"github.com/ethereum/go-ethereum/common"
)

// QuickSwapSnapshotRepository is an in-memory QuickSwap V3 SnapshotRepository.
type QuickSwapSnapshotRepository struct {
	inner *CLV3SnapshotRepository
}

func NewQuickSwapSnapshotRepository() *QuickSwapSnapshotRepository {
	return &QuickSwapSnapshotRepository{inner: NewCLV3SnapshotRepository()}
}

func (r *QuickSwapSnapshotRepository) Save(ctx context.Context, snapshot *marketquick.Snapshot) error {
	return r.inner.Save(ctx, snapshot)
}

func (r *QuickSwapSnapshotRepository) GetLatest(ctx context.Context, poolAddress common.Address) (*marketquick.Snapshot, error) {
	return r.inner.GetLatest(ctx, poolAddress)
}

func (r *QuickSwapSnapshotRepository) GetAtBlock(ctx context.Context, poolAddress common.Address, blockNumber uint64) (*marketquick.Snapshot, error) {
	return r.inner.GetAtBlock(ctx, poolAddress, blockNumber)
}

func (r *QuickSwapSnapshotRepository) DeleteAfterBlock(ctx context.Context, poolAddress common.Address, blockNumber uint64) error {
	return r.inner.DeleteAfterBlock(ctx, poolAddress, blockNumber)
}
