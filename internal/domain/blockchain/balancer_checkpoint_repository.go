package blockchain

import (
	"context"

	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
)

// BalancerCheckpointRepository persists sync checkpoints keyed by Balancer PoolID.
type BalancerCheckpointRepository interface {
	Save(ctx context.Context, checkpoint *BalancerCheckpoint) error
	SaveMany(ctx context.Context, checkpoints []*BalancerCheckpoint) error
	Get(ctx context.Context, id marketbalancer.PoolID) (*BalancerCheckpoint, error)
	Delete(ctx context.Context, id marketbalancer.PoolID) error
}
