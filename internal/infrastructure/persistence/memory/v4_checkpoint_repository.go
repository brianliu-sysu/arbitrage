package memory

import (
	"context"
	"sync"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/v4"
)

// V4CheckpointRepository is an in-memory blockchain.V4CheckpointRepository.
type V4CheckpointRepository struct {
	mu          sync.RWMutex
	checkpoints map[marketv4.PoolID]*blockchain.V4Checkpoint
}

func NewV4CheckpointRepository() *V4CheckpointRepository {
	return &V4CheckpointRepository{checkpoints: make(map[marketv4.PoolID]*blockchain.V4Checkpoint)}
}

func (r *V4CheckpointRepository) Save(ctx context.Context, checkpoint *blockchain.V4Checkpoint) error {
	return r.SaveMany(ctx, []*blockchain.V4Checkpoint{checkpoint})
}

func (r *V4CheckpointRepository) SaveMany(_ context.Context, checkpoints []*blockchain.V4Checkpoint) error {
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
		r.checkpoints[checkpoint.PoolID] = &copyCheckpoint
	}
	return nil
}

func (r *V4CheckpointRepository) Get(_ context.Context, id marketv4.PoolID) (*blockchain.V4Checkpoint, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	checkpoint, ok := r.checkpoints[id]
	if !ok {
		return nil, nil
	}
	copyCheckpoint := *checkpoint
	return &copyCheckpoint, nil
}

func (r *V4CheckpointRepository) Delete(_ context.Context, id marketv4.PoolID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.checkpoints, id)
	return nil
}
