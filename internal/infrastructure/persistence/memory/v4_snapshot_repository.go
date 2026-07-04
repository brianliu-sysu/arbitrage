package memory

import (
	"context"
	"sort"
	"sync"

	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/v4"
)

// V4SnapshotRepository is an in-memory marketv4.SnapshotRepository.
type V4SnapshotRepository struct {
	mu        sync.RWMutex
	snapshots map[marketv4.PoolID][]*marketv4.Snapshot
}

func NewV4SnapshotRepository() *V4SnapshotRepository {
	return &V4SnapshotRepository{snapshots: make(map[marketv4.PoolID][]*marketv4.Snapshot)}
}

func (r *V4SnapshotRepository) Save(_ context.Context, snapshot *marketv4.Snapshot) error {
	if snapshot == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	clone := *snapshot
	clone.State = snapshot.State.Clone()
	clone.Ticks = snapshot.Ticks.Clone()
	clone.Bitmap = snapshot.Bitmap.Clone()

	entries := r.snapshots[snapshot.PoolID]
	for i, existing := range entries {
		if existing.BlockNumber == snapshot.BlockNumber {
			entries[i] = &clone
			r.snapshots[snapshot.PoolID] = entries
			return nil
		}
	}
	r.snapshots[snapshot.PoolID] = append(entries, &clone)
	sort.Slice(r.snapshots[snapshot.PoolID], func(i, j int) bool {
		return r.snapshots[snapshot.PoolID][i].BlockNumber < r.snapshots[snapshot.PoolID][j].BlockNumber
	})
	return nil
}

func (r *V4SnapshotRepository) GetLatest(_ context.Context, id marketv4.PoolID) (*marketv4.Snapshot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entries := r.snapshots[id]
	if len(entries) == 0 {
		return nil, nil
	}
	latest := entries[len(entries)-1]
	clone := *latest
	clone.State = latest.State.Clone()
	clone.Ticks = latest.Ticks.Clone()
	clone.Bitmap = latest.Bitmap.Clone()
	return &clone, nil
}

func (r *V4SnapshotRepository) GetAtBlock(_ context.Context, id marketv4.PoolID, blockNumber uint64) (*marketv4.Snapshot, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for i := len(r.snapshots[id]) - 1; i >= 0; i-- {
		snapshot := r.snapshots[id][i]
		if snapshot.BlockNumber <= blockNumber {
			clone := *snapshot
			clone.State = snapshot.State.Clone()
			clone.Ticks = snapshot.Ticks.Clone()
			clone.Bitmap = snapshot.Bitmap.Clone()
			return &clone, nil
		}
	}
	return nil, nil
}

func (r *V4SnapshotRepository) DeleteAfterBlock(_ context.Context, id marketv4.PoolID, blockNumber uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	entries := r.snapshots[id]
	kept := make([]*marketv4.Snapshot, 0, len(entries))
	for _, snapshot := range entries {
		if snapshot.BlockNumber <= blockNumber {
			kept = append(kept, snapshot)
		}
	}
	r.snapshots[id] = kept
	return nil
}
