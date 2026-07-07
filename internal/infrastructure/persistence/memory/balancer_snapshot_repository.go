package memory

import (
	"context"
	"math/big"
	"sync"

	marketbalancer "github.com/brianliu-sysu/uniswapv3/internal/domain/market/balancer"
	"github.com/ethereum/go-ethereum/common"
)

// BalancerSnapshotRepository stores Balancer snapshots in memory.
type BalancerSnapshotRepository struct {
	mu        sync.RWMutex
	snapshots map[marketbalancer.PoolID]map[uint64]*marketbalancer.Snapshot
}

func NewBalancerSnapshotRepository() *BalancerSnapshotRepository {
	return &BalancerSnapshotRepository{snapshots: make(map[marketbalancer.PoolID]map[uint64]*marketbalancer.Snapshot)}
}

func (r *BalancerSnapshotRepository) Save(_ context.Context, snapshot *marketbalancer.Snapshot) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.snapshots[snapshot.PoolID] == nil {
		r.snapshots[snapshot.PoolID] = make(map[uint64]*marketbalancer.Snapshot)
	}
	r.snapshots[snapshot.PoolID][snapshot.BlockNumber] = cloneBalancerSnapshot(snapshot)
	return nil
}

func (r *BalancerSnapshotRepository) GetLatest(_ context.Context, poolID marketbalancer.PoolID) (*marketbalancer.Snapshot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var latest *marketbalancer.Snapshot
	for _, snapshot := range r.snapshots[poolID] {
		if latest == nil || snapshot.BlockNumber > latest.BlockNumber {
			latest = snapshot
		}
	}
	return cloneBalancerSnapshot(latest), nil
}

func (r *BalancerSnapshotRepository) GetAtBlock(_ context.Context, poolID marketbalancer.PoolID, blockNumber uint64) (*marketbalancer.Snapshot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return cloneBalancerSnapshot(r.snapshots[poolID][blockNumber]), nil
}

func (r *BalancerSnapshotRepository) DeleteAfterBlock(_ context.Context, poolID marketbalancer.PoolID, blockNumber uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for snapshotBlock := range r.snapshots[poolID] {
		if snapshotBlock > blockNumber {
			delete(r.snapshots[poolID], snapshotBlock)
		}
	}
	return nil
}

func cloneBalancerSnapshot(snapshot *marketbalancer.Snapshot) *marketbalancer.Snapshot {
	if snapshot == nil {
		return nil
	}
	tokens := make([]common.Address, len(snapshot.Tokens))
	copy(tokens, snapshot.Tokens)
	return &marketbalancer.Snapshot{
		PoolID:            snapshot.PoolID,
		BlockNumber:       snapshot.BlockNumber,
		Tokens:            tokens,
		Balances:          cloneBalancerIntMap(snapshot.Balances),
		Weights:           cloneBalancerIntMap(snapshot.Weights),
		Amplification:     cloneBalancerInt(snapshot.Amplification),
		SwapFeePercentage: cloneBalancerInt(snapshot.SwapFeePercentage),
		CreatedAt:         snapshot.CreatedAt,
	}
}

func cloneBalancerInt(value *big.Int) *big.Int {
	if value == nil {
		return big.NewInt(0)
	}
	return new(big.Int).Set(value)
}

func cloneBalancerIntMap(values map[common.Address]*big.Int) map[common.Address]*big.Int {
	out := make(map[common.Address]*big.Int, len(values))
	for token, value := range values {
		out[token] = cloneBalancerInt(value)
	}
	return out
}
