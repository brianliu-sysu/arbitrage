package syncapp

import (
	"context"
	"time"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/market"
	"github.com/ethereum/go-ethereum/common"
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

// SnapshotService creates and restores pool snapshots.
type SnapshotService struct {
	snapshots market.SnapshotRepository
	policy    SnapshotPolicy
	clock     func() time.Time
}

func NewSnapshotService(snapshots market.SnapshotRepository, policy SnapshotPolicy) *SnapshotService {
	return &SnapshotService{
		snapshots: snapshots,
		policy:    policy,
		clock:     time.Now,
	}
}

func (s *SnapshotService) LoadLatest(ctx context.Context, poolAddress common.Address) (*market.Snapshot, error) {
	return s.snapshots.GetLatest(ctx, poolAddress)
}

func (s *SnapshotService) Create(ctx context.Context, pool *market.Pool, blockNumber uint64) error {
	snapshot := market.NewSnapshot(pool, blockNumber, s.clock().UTC())
	return s.snapshots.Save(ctx, snapshot)
}

func (s *SnapshotService) MaybeCreateSnapshot(ctx context.Context, pool *market.Pool, blockNumber uint64) error {
	latest, err := s.snapshots.GetLatest(ctx, pool.Address)
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

func (s *SnapshotService) RestorePool(ctx context.Context, pool *market.Pool) (*market.Snapshot, error) {
	snapshot, err := s.snapshots.GetLatest(ctx, pool.Address)
	if err != nil {
		return nil, err
	}
	if snapshot == nil {
		return nil, nil
	}
	snapshot.RestoreTo(pool)
	return snapshot, nil
}

func (s *SnapshotService) DeleteAfterBlock(ctx context.Context, poolAddress common.Address, blockNumber uint64) error {
	return s.snapshots.DeleteAfterBlock(ctx, poolAddress, blockNumber)
}
