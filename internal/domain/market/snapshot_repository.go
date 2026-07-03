package market

import (
	"context"

	"github.com/ethereum/go-ethereum/common"
)

type SnapshotRepository interface {
	Save(ctx context.Context, snapshot *Snapshot) error
	GetLatest(ctx context.Context, poolAddress common.Address) (*Snapshot, error)
	GetAtBlock(ctx context.Context, poolAddress common.Address, blockNumber uint64) (*Snapshot, error)
	DeleteAfterBlock(ctx context.Context, poolAddress common.Address, blockNumber uint64) error
}
