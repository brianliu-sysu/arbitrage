package memory

import (
	"context"
	"sync"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	"github.com/ethereum/go-ethereum/common"
)

// CheckpointRepository is an in-memory CheckpointRepository.
type CheckpointRepository struct {
	mu          sync.RWMutex
	checkpoints map[common.Address]*blockchain.Checkpoint
}

func NewCheckpointRepository() *CheckpointRepository {
	return &CheckpointRepository{checkpoints: make(map[common.Address]*blockchain.Checkpoint)}
}

func (r *CheckpointRepository) Save(ctx context.Context, checkpoint *blockchain.Checkpoint) error {
	return r.SaveMany(ctx, []*blockchain.Checkpoint{checkpoint})
}

func (r *CheckpointRepository) SaveMany(_ context.Context, checkpoints []*blockchain.Checkpoint) error {
	if len(checkpoints) == 0 {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, checkpoint := range checkpoints {
		if checkpoint == nil {
			continue
		}
		copyCheckpoint := *checkpoint
		r.checkpoints[checkpoint.PoolAddress] = &copyCheckpoint
	}
	return nil
}

func (r *CheckpointRepository) Get(_ context.Context, poolAddress common.Address) (*blockchain.Checkpoint, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	checkpoint, ok := r.checkpoints[poolAddress]
	if !ok {
		return nil, nil
	}
	copyCheckpoint := *checkpoint
	return &copyCheckpoint, nil
}

func (r *CheckpointRepository) Delete(_ context.Context, poolAddress common.Address) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.checkpoints, poolAddress)
	return nil
}
