package blockchain

import (
	"context"

	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ4"
)

// V4CheckpointRepository persists sync checkpoints keyed by PoolID.
type V4CheckpointRepository interface {
	Save(ctx context.Context, checkpoint *V4Checkpoint) error
	SaveMany(ctx context.Context, checkpoints []*V4Checkpoint) error
	Get(ctx context.Context, id marketv4.PoolID) (*V4Checkpoint, error)
	Delete(ctx context.Context, id marketv4.PoolID) error
}
