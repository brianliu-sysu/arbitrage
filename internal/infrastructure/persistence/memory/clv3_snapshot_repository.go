package memory

import (
	"context"
	"sort"
	"sync"

	marketclv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/clv3"
	"github.com/ethereum/go-ethereum/common"
)

// CLV3SnapshotRepository is an in-memory snapshot store for CLV3-style pools.
type CLV3SnapshotRepository struct {
	mu        sync.RWMutex
	snapshots map[common.Address][]*marketclv3.Snapshot
}

func NewCLV3SnapshotRepository() *CLV3SnapshotRepository {
	return &CLV3SnapshotRepository{snapshots: make(map[common.Address][]*marketclv3.Snapshot)}
}

func (r *CLV3SnapshotRepository) Save(_ context.Context, snapshot *marketclv3.Snapshot) error {
	if snapshot == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	cloned := cloneCLV3Snapshot(snapshot)
	r.snapshots[snapshot.PoolAddress] = append(r.snapshots[snapshot.PoolAddress], cloned)
	sortCLV3Snapshots(r.snapshots[snapshot.PoolAddress])
	return nil
}

func (r *CLV3SnapshotRepository) GetLatest(_ context.Context, poolAddress common.Address) (*marketclv3.Snapshot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := r.snapshots[poolAddress]
	if len(items) == 0 {
		return nil, nil
	}
	return cloneCLV3Snapshot(items[len(items)-1]), nil
}

func (r *CLV3SnapshotRepository) GetAtBlock(_ context.Context, poolAddress common.Address, blockNumber uint64) (*marketclv3.Snapshot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for i := len(r.snapshots[poolAddress]) - 1; i >= 0; i-- {
		if r.snapshots[poolAddress][i].BlockNumber == blockNumber {
			return cloneCLV3Snapshot(r.snapshots[poolAddress][i]), nil
		}
	}
	return nil, nil
}

func (r *CLV3SnapshotRepository) DeleteAfterBlock(_ context.Context, poolAddress common.Address, blockNumber uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := r.snapshots[poolAddress]
	kept := make([]*marketclv3.Snapshot, 0, len(items))
	for _, snapshot := range items {
		if snapshot.BlockNumber <= blockNumber {
			kept = append(kept, snapshot)
		}
	}
	r.snapshots[poolAddress] = kept
	return nil
}

func cloneCLV3Snapshot(snapshot *marketclv3.Snapshot) *marketclv3.Snapshot {
	if snapshot == nil {
		return nil
	}
	cloned := *snapshot
	cloned.State = snapshot.State.Clone()
	cloned.Ticks = snapshot.Ticks.Clone()
	cloned.Bitmap = snapshot.Bitmap.Clone()
	return &cloned
}

func sortCLV3Snapshots(items []*marketclv3.Snapshot) {
	sort.Slice(items, func(i, j int) bool {
		return items[i].BlockNumber < items[j].BlockNumber
	})
}
