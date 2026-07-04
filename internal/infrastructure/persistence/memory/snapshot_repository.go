package memory

import (
	"context"
	"sort"
	"sync"

	marketv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/v3"
	"github.com/brianliu-sysu/uniswapv3/internal/infrastructure/persistence/codec"
	"github.com/ethereum/go-ethereum/common"
)

// SnapshotRepository is an in-memory SnapshotRepository.
type SnapshotRepository struct {
	mu        sync.RWMutex
	snapshots map[common.Address][]*marketv3.Snapshot
}

func NewSnapshotRepository() *SnapshotRepository {
	return &SnapshotRepository{snapshots: make(map[common.Address][]*marketv3.Snapshot)}
}

func (r *SnapshotRepository) Save(_ context.Context, snapshot *marketv3.Snapshot) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.snapshots[snapshot.PoolAddress] = append(r.snapshots[snapshot.PoolAddress], codec.CloneSnapshot(snapshot))
	sortSnapshots(r.snapshots[snapshot.PoolAddress])
	return nil
}

func (r *SnapshotRepository) GetLatest(_ context.Context, poolAddress common.Address) (*marketv3.Snapshot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := r.snapshots[poolAddress]
	if len(items) == 0 {
		return nil, nil
	}
	return codec.CloneSnapshot(items[len(items)-1]), nil
}

func (r *SnapshotRepository) GetAtBlock(_ context.Context, poolAddress common.Address, blockNumber uint64) (*marketv3.Snapshot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for i := len(r.snapshots[poolAddress]) - 1; i >= 0; i-- {
		if r.snapshots[poolAddress][i].BlockNumber == blockNumber {
			return codec.CloneSnapshot(r.snapshots[poolAddress][i]), nil
		}
	}
	return nil, nil
}

func (r *SnapshotRepository) DeleteAfterBlock(_ context.Context, poolAddress common.Address, blockNumber uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := r.snapshots[poolAddress]
	kept := make([]*marketv3.Snapshot, 0, len(items))
	for _, snapshot := range items {
		if snapshot.BlockNumber <= blockNumber {
			kept = append(kept, snapshot)
		}
	}
	r.snapshots[poolAddress] = kept
	return nil
}

func sortSnapshots(items []*marketv3.Snapshot) {
	sort.Slice(items, func(i, j int) bool {
		return items[i].BlockNumber < items[j].BlockNumber
	})
}
