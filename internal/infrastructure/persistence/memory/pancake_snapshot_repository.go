package memory

import (
	"context"
	"sort"
	"sync"

	marketpancake "github.com/brianliu-sysu/uniswapv3/internal/domain/market/pancakev3"
	"github.com/brianliu-sysu/uniswapv3/internal/infrastructure/persistence/codec"
	"github.com/ethereum/go-ethereum/common"
)

// PancakeSnapshotRepository is an in-memory PancakeSwap V3 SnapshotRepository.
type PancakeSnapshotRepository struct {
	mu        sync.RWMutex
	snapshots map[common.Address][]*marketpancake.Snapshot
}

func NewPancakeSnapshotRepository() *PancakeSnapshotRepository {
	return &PancakeSnapshotRepository{snapshots: make(map[common.Address][]*marketpancake.Snapshot)}
}

func (r *PancakeSnapshotRepository) Save(_ context.Context, snapshot *marketpancake.Snapshot) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.snapshots[snapshot.PoolAddress] = append(r.snapshots[snapshot.PoolAddress], codec.ClonePancakeSnapshot(snapshot))
	sortPancakeSnapshots(r.snapshots[snapshot.PoolAddress])
	return nil
}

func (r *PancakeSnapshotRepository) GetLatest(_ context.Context, poolAddress common.Address) (*marketpancake.Snapshot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := r.snapshots[poolAddress]
	if len(items) == 0 {
		return nil, nil
	}
	return codec.ClonePancakeSnapshot(items[len(items)-1]), nil
}

func (r *PancakeSnapshotRepository) GetAtBlock(_ context.Context, poolAddress common.Address, blockNumber uint64) (*marketpancake.Snapshot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for i := len(r.snapshots[poolAddress]) - 1; i >= 0; i-- {
		if r.snapshots[poolAddress][i].BlockNumber == blockNumber {
			return codec.ClonePancakeSnapshot(r.snapshots[poolAddress][i]), nil
		}
	}
	return nil, nil
}

func (r *PancakeSnapshotRepository) DeleteAfterBlock(_ context.Context, poolAddress common.Address, blockNumber uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := r.snapshots[poolAddress]
	kept := make([]*marketpancake.Snapshot, 0, len(items))
	for _, snapshot := range items {
		if snapshot.BlockNumber <= blockNumber {
			kept = append(kept, snapshot)
		}
	}
	r.snapshots[poolAddress] = kept
	return nil
}

func sortPancakeSnapshots(items []*marketpancake.Snapshot) {
	sort.Slice(items, func(i, j int) bool {
		return items[i].BlockNumber < items[j].BlockNumber
	})
}
