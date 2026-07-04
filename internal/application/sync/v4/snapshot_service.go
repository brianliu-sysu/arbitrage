package syncv4

import (
	"context"
	"time"

	marketv4 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/v4"
)

// SnapshotPolicy decides when snapshots should be created.
type SnapshotPolicy struct {
	BlockInterval uint64
}

func (p SnapshotPolicy) ShouldSnapshot(lastSnapshotBlock, currentBlock uint64) bool {
	if p.BlockInterval == 0 {
		return false
	}
	if lastSnapshotBlock == 0 {
		return currentBlock >= p.BlockInterval
	}
	return currentBlock >= lastSnapshotBlock+p.BlockInterval
}

// SnapshotService creates and restores V4 pool snapshots.
type SnapshotService struct {
	snapshots marketv4.SnapshotRepository
	policy    SnapshotPolicy
	clock     func() time.Time
}

func NewSnapshotService(snapshots marketv4.SnapshotRepository, policy SnapshotPolicy) *SnapshotService {
	return &SnapshotService{
		snapshots: snapshots,
		policy:    policy,
		clock:     time.Now,
	}
}

func (s *SnapshotService) LoadLatest(ctx context.Context, poolID marketv4.PoolID) (*marketv4.Snapshot, error) {
	return s.snapshots.GetLatest(ctx, poolID)
}

func (s *SnapshotService) Create(ctx context.Context, pool *marketv4.Pool, blockNumber uint64) error {
	snapshot := marketv4.NewSnapshot(pool, blockNumber, s.clock().UTC())
	return s.snapshots.Save(ctx, snapshot)
}

func (s *SnapshotService) MaybeCreateSnapshot(ctx context.Context, pool *marketv4.Pool, blockNumber uint64) error {
	latest, err := s.snapshots.GetLatest(ctx, pool.ID)
	if err != nil {
		return err
	}

	lastBlock := uint64(0)
	if latest != nil {
		lastBlock = latest.BlockNumber
	}
	if !s.policy.ShouldSnapshot(lastBlock, blockNumber) {
		return nil
	}
	return s.Create(ctx, pool, blockNumber)
}

func (s *SnapshotService) RestorePool(ctx context.Context, pool *marketv4.Pool) (*marketv4.Snapshot, error) {
	snapshot, err := s.snapshots.GetLatest(ctx, pool.ID)
	if err != nil {
		return nil, err
	}
	if snapshot == nil {
		return nil, nil
	}
	if pool.LastBlockNumber > 0 && snapshot.BlockNumber <= pool.LastBlockNumber {
		return snapshot, nil
	}
	snapshot.RestoreTo(pool)
	return snapshot, nil
}

func (s *SnapshotService) DeleteAfterBlock(ctx context.Context, poolID marketv4.PoolID, blockNumber uint64) error {
	return s.snapshots.DeleteAfterBlock(ctx, poolID, blockNumber)
}
