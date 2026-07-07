package balancer

import "context"

// SnapshotRepository stores Balancer pool snapshots keyed by Vault PoolID.
type SnapshotRepository interface {
	Save(ctx context.Context, snapshot *Snapshot) error
	GetLatest(ctx context.Context, poolID PoolID) (*Snapshot, error)
	GetAtBlock(ctx context.Context, poolID PoolID, blockNumber uint64) (*Snapshot, error)
	DeleteAfterBlock(ctx context.Context, poolID PoolID, blockNumber uint64) error
}
