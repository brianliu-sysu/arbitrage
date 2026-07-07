package memory

import (
	"context"
	"sync"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
)

// BalancerCheckpointRepository stores Balancer checkpoints in memory.
type BalancerCheckpointRepository struct {
	mu          sync.RWMutex
	checkpoints map[marketbalancer.PoolID]*blockchain.BalancerCheckpoint
}

func NewBalancerCheckpointRepository() *BalancerCheckpointRepository {
	return &BalancerCheckpointRepository{checkpoints: make(map[marketbalancer.PoolID]*blockchain.BalancerCheckpoint)}
}

func (r *BalancerCheckpointRepository) Save(ctx context.Context, checkpoint *blockchain.BalancerCheckpoint) error {
	return r.SaveMany(ctx, []*blockchain.BalancerCheckpoint{checkpoint})
}

func (r *BalancerCheckpointRepository) SaveMany(_ context.Context, checkpoints []*blockchain.BalancerCheckpoint) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, checkpoint := range checkpoints {
		if checkpoint == nil {
			continue
		}
		cloned := *checkpoint
		r.checkpoints[checkpoint.PoolID] = &cloned
	}
	return nil
}

func (r *BalancerCheckpointRepository) Get(_ context.Context, id marketbalancer.PoolID) (*blockchain.BalancerCheckpoint, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	checkpoint := r.checkpoints[id]
	if checkpoint == nil {
		return nil, nil
	}
	cloned := *checkpoint
	return &cloned, nil
}

func (r *BalancerCheckpointRepository) Delete(_ context.Context, id marketbalancer.PoolID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.checkpoints, id)
	return nil
}
