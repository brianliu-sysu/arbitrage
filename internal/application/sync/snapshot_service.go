package syncapp

import (
	"context"
	"time"
)

// SnapshotRepository stores pool snapshots keyed by pool ID.
type SnapshotRepository[PoolID comparable, Snapshot any] interface {
	Save(ctx context.Context, snapshot *Snapshot) error
	GetLatest(ctx context.Context, poolID PoolID) (*Snapshot, error)
	GetAtBlock(ctx context.Context, poolID PoolID, blockNumber uint64) (*Snapshot, error)
	DeleteAfterBlock(ctx context.Context, poolID PoolID, blockNumber uint64) error
}

// SnapshotOps configures protocol-specific snapshot behavior.
type SnapshotOps[PoolID comparable, Pool any, Snapshot any] struct {
	PoolID        func(*Pool) PoolID
	NewSnapshot   func(*Pool, uint64, time.Time) *Snapshot
	RestoreTo     func(*Snapshot, *Pool)
	BlockNumber   func(*Snapshot) uint64
	LastBlock     func(*Pool) uint64
	IsInitialized func(*Pool) bool
}

// SnapshotService creates and restores pool snapshots.
type SnapshotService[PoolID comparable, Pool any, Snapshot any] struct {
	snapshots SnapshotRepository[PoolID, Snapshot]
	policy    SnapshotPolicy
	clock     func() time.Time
	ops       SnapshotOps[PoolID, Pool, Snapshot]
}

func NewSnapshotService[PoolID comparable, Pool any, Snapshot any](
	snapshots SnapshotRepository[PoolID, Snapshot],
	policy SnapshotPolicy,
	ops SnapshotOps[PoolID, Pool, Snapshot],
) *SnapshotService[PoolID, Pool, Snapshot] {
	return &SnapshotService[PoolID, Pool, Snapshot]{
		snapshots: snapshots,
		policy:    policy,
		clock:     time.Now,
		ops:       ops,
	}
}

func (s *SnapshotService[PoolID, Pool, Snapshot]) LoadLatest(ctx context.Context, poolID PoolID) (*Snapshot, error) {
	return s.snapshots.GetLatest(ctx, poolID)
}

// LoadAtOrBefore returns an exact snapshot at blockNumber, or the latest snapshot at or before it.
func (s *SnapshotService[PoolID, Pool, Snapshot]) LoadAtOrBefore(
	ctx context.Context,
	poolID PoolID,
	blockNumber uint64,
) (*Snapshot, error) {
	exact, err := s.snapshots.GetAtBlock(ctx, poolID, blockNumber)
	if err != nil {
		return nil, err
	}
	if exact != nil {
		return exact, nil
	}
	latest, err := s.snapshots.GetLatest(ctx, poolID)
	if err != nil {
		return nil, err
	}
	if latest != nil && s.ops.BlockNumber(latest) > blockNumber {
		return nil, nil
	}
	return latest, nil
}

func (s *SnapshotService[PoolID, Pool, Snapshot]) Create(ctx context.Context, pool *Pool, blockNumber uint64) error {
	snapshot := s.ops.NewSnapshot(pool, blockNumber, s.clock().UTC())
	return s.snapshots.Save(ctx, snapshot)
}

func (s *SnapshotService[PoolID, Pool, Snapshot]) MaybeCreateSnapshot(ctx context.Context, pool *Pool, blockNumber uint64) error {
	latest, err := s.snapshots.GetLatest(ctx, s.ops.PoolID(pool))
	if err != nil {
		return err
	}

	lastBlock := uint64(0)
	if latest != nil {
		lastBlock = s.ops.BlockNumber(latest)
	}
	if !s.policy.ShouldSnapshot(lastBlock, blockNumber) {
		return nil
	}
	return s.Create(ctx, pool, blockNumber)
}

func (s *SnapshotService[PoolID, Pool, Snapshot]) RestorePool(ctx context.Context, pool *Pool) (*Snapshot, error) {
	snapshot, err := s.snapshots.GetLatest(ctx, s.ops.PoolID(pool))
	if err != nil {
		return nil, err
	}
	if snapshot == nil {
		return nil, nil
	}
	if s.ops.LastBlock(pool) == 0 && s.ops.IsInitialized(pool) {
		return snapshot, nil
	}
	if s.ops.LastBlock(pool) > 0 && s.ops.BlockNumber(snapshot) <= s.ops.LastBlock(pool) {
		return snapshot, nil
	}
	s.ops.RestoreTo(snapshot, pool)
	return snapshot, nil
}

func (s *SnapshotService[PoolID, Pool, Snapshot]) DeleteAfterBlock(ctx context.Context, poolID PoolID, blockNumber uint64) error {
	return s.snapshots.DeleteAfterBlock(ctx, poolID, blockNumber)
}
