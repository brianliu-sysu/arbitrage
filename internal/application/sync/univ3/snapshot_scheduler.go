package syncv3

import (
	"context"
	"fmt"
	"time"

	marketv3 "github.com/brianliu-sysu/uniswapv3/internal/domain/market/univ3"
)

// SnapshotScheduler periodically creates snapshots as a fallback safety net.
type SnapshotScheduler struct {
	config    Config
	pools     marketv3.PoolRepository
	snapshots *SnapshotService
	lifecycle *PoolLifecycleService
}

func NewSnapshotScheduler(
	config Config,
	pools marketv3.PoolRepository,
	snapshots *SnapshotService,
	lifecycle *PoolLifecycleService,
) *SnapshotScheduler {
	return &SnapshotScheduler{
		config:    config,
		pools:     pools,
		snapshots: snapshots,
		lifecycle: lifecycle,
	}
}

func (s *SnapshotScheduler) Run(ctx context.Context) error {
	if s.config.SnapshotFallback <= 0 {
		<-ctx.Done()
		return ctx.Err()
	}

	ticker := time.NewTicker(s.config.SnapshotFallback)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := s.runOnce(ctx); err != nil {
				return err
			}
		}
	}
}

func (s *SnapshotScheduler) runOnce(ctx context.Context) error {
	for _, poolAddress := range s.lifecycle.ListActive() {
		pool, err := s.pools.Get(ctx, poolAddress)
		if err != nil {
			return fmt.Errorf("load pool %s: %w", poolAddress.Hex(), err)
		}
		if pool == nil || pool.LastBlockNumber == 0 {
			continue
		}
		if err := s.snapshots.Create(ctx, pool, pool.LastBlockNumber); err != nil {
			return fmt.Errorf("fallback snapshot pool %s: %w", poolAddress.Hex(), err)
		}
	}
	return nil
}

// RunOnce exposes fallback snapshot execution for tests and manual triggers.
func (s *SnapshotScheduler) RunOnce(ctx context.Context) error {
	return s.runOnce(ctx)
}
