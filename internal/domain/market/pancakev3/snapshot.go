package pancakev3

import (
	"context"
	"time"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/market/clv3"
	"github.com/ethereum/go-ethereum/common"
)

// Snapshot is a point-in-time copy of PancakeSwap V3 pool market state at a block height.
type Snapshot = clv3.Snapshot

func NewSnapshot(pool *Pool, blockNumber uint64, createdAt time.Time) *Snapshot {
	return clv3.NewSnapshot(&pool.Pool, blockNumber, createdAt)
}

func RestoreSnapshot(snapshot *Snapshot, pool *Pool) {
	if snapshot == nil || pool == nil {
		return
	}
	snapshot.RestoreTo(&pool.Pool)
}

// SnapshotRepository stores PancakeSwap V3 pool snapshots keyed by contract address.
type SnapshotRepository interface {
	Save(ctx context.Context, snapshot *Snapshot) error
	GetLatest(ctx context.Context, poolAddress common.Address) (*Snapshot, error)
	GetAtBlock(ctx context.Context, poolAddress common.Address, blockNumber uint64) (*Snapshot, error)
	DeleteAfterBlock(ctx context.Context, poolAddress common.Address, blockNumber uint64) error
}
