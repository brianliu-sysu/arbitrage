package clv3sync

import (
	"context"
	"time"

	syncapp "github.com/brianliu-sysu/uniswapv3/internal/application/sync"
	marketclv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/clv3"
	"github.com/ethereum/go-ethereum/common"
)

type SnapshotPolicy = syncapp.SnapshotPolicy

// SnapshotService creates and restores pool snapshots.
type SnapshotService struct {
	snapshots SnapshotRepository
	policy    SnapshotPolicy
	clock     func() time.Time
}

func NewSnapshotService(snapshots SnapshotRepository, policy SnapshotPolicy) *SnapshotService {
	return &SnapshotService{
		snapshots: snapshots,
		policy:    policy,
		clock:     time.Now,
	}
}

func (s *SnapshotService) LoadLatest(ctx context.Context, poolAddress common.Address) (*marketclv3.Snapshot, error) {
	return s.snapshots.GetLatest(ctx, poolAddress)
}

// LoadAtOrBefore returns an exact snapshot at blockNumber, or the latest snapshot at or before it.
func (s *SnapshotService) LoadAtOrBefore(ctx context.Context, poolAddress common.Address, blockNumber uint64) (*marketclv3.Snapshot, error) {
	exact, err := s.snapshots.GetAtBlock(ctx, poolAddress, blockNumber)
	if err != nil {
		return nil, err
	}
	if exact != nil {
		return exact, nil
	}
	latest, err := s.snapshots.GetLatest(ctx, poolAddress)
	if err != nil {
		return nil, err
	}
	if latest != nil && latest.BlockNumber > blockNumber {
		return nil, nil
	}
	return latest, nil
}

func (s *SnapshotService) Create(ctx context.Context, pool *marketclv3.Pool, blockNumber uint64) error {
	snapshot := marketclv3.NewSnapshot(pool, blockNumber, s.clock().UTC())
	return s.snapshots.Save(ctx, snapshot)
}

func (s *SnapshotService) MaybeCreateSnapshot(ctx context.Context, pool *marketclv3.Pool, blockNumber uint64) error {
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

func (s *SnapshotService) RestorePool(ctx context.Context, pool *marketclv3.Pool) (*marketclv3.Snapshot, error) {
	snapshot, err := s.snapshots.GetLatest(ctx, pool.Address)
	if err != nil {
		return nil, err
	}
	if snapshot == nil {
		return nil, nil
	}
	if pool.LastBlockNumber == 0 && pool.State.IsInitialized() {
		return snapshot, nil
	}
	if pool.LastBlockNumber > 0 && snapshot.BlockNumber <= pool.LastBlockNumber {
		return snapshot, nil
	}
	snapshot.RestoreTo(pool)
	return snapshot, nil
}

func (s *SnapshotService) DeleteAfterBlock(ctx context.Context, poolAddress common.Address, blockNumber uint64) error {
	return s.snapshots.DeleteAfterBlock(ctx, poolAddress, blockNumber)
}
