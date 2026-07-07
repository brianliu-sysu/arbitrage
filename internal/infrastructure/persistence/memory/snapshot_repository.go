package memory

import (
	"context"

	marketv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ3"
	"github.com/ethereum/go-ethereum/common"
)

// SnapshotRepository is an in-memory SnapshotRepository.
type SnapshotRepository struct {
	inner *CLV3SnapshotRepository
}

func NewSnapshotRepository() *SnapshotRepository {
	return &SnapshotRepository{inner: NewCLV3SnapshotRepository()}
}

func (r *SnapshotRepository) Save(ctx context.Context, snapshot *marketv3.Snapshot) error {
	return r.inner.Save(ctx, snapshot)
}

func (r *SnapshotRepository) GetLatest(ctx context.Context, poolAddress common.Address) (*marketv3.Snapshot, error) {
	return r.inner.GetLatest(ctx, poolAddress)
}

func (r *SnapshotRepository) GetAtBlock(ctx context.Context, poolAddress common.Address, blockNumber uint64) (*marketv3.Snapshot, error) {
	return r.inner.GetAtBlock(ctx, poolAddress, blockNumber)
}

func (r *SnapshotRepository) DeleteAfterBlock(ctx context.Context, poolAddress common.Address, blockNumber uint64) error {
	return r.inner.DeleteAfterBlock(ctx, poolAddress, blockNumber)
}
