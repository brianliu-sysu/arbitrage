package univ4

import (
	"context"
)

// SnapshotRepository stores V4 pool snapshots keyed by PoolID.
type SnapshotRepository interface {
	Save(ctx context.Context, snapshot *Snapshot) error
	GetLatest(ctx context.Context, id PoolID) (*Snapshot, error)
	GetAtBlock(ctx context.Context, id PoolID, blockNumber uint64) (*Snapshot, error)
	DeleteAfterBlock(ctx context.Context, id PoolID, blockNumber uint64) error
}
