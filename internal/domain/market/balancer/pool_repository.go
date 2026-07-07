package balancer

import "context"

// PoolRepository persists Balancer pool aggregates keyed by Vault PoolID.
type PoolRepository interface {
	Save(ctx context.Context, pool *Pool) error
	Get(ctx context.Context, id PoolID) (*Pool, error)
	Delete(ctx context.Context, id PoolID) error
	AdvanceSyncProgress(ctx context.Context, id PoolID, blockNumber uint64) error
	AdvanceSyncProgressMany(ctx context.Context, ids []PoolID, blockNumber uint64) error
}
