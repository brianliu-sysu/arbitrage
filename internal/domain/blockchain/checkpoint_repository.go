package blockchain

import (
	"context"

	"github.com/ethereum/go-ethereum/common"
)

type CheckpointRepository interface {
	Save(ctx context.Context, checkpoint *Checkpoint) error
	SaveMany(ctx context.Context, checkpoints []*Checkpoint) error
	Get(ctx context.Context, poolAddress common.Address) (*Checkpoint, error)
	Delete(ctx context.Context, poolAddress common.Address) error
}
