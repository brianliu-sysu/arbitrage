package syncapp

import (
	"context"
	"fmt"
	"time"
)

// SnapshotSchedulerDeps configures periodic fallback snapshot creation.
type SnapshotSchedulerDeps[PoolID comparable, Pool any] struct {
	FallbackInterval time.Duration
	Lifecycle        *PoolLifecycleService[PoolID]
	LoadPool         func(context.Context, PoolID) (*Pool, error)
	CreateSnapshot   func(context.Context, *Pool, uint64) error
	PoolLastBlock    func(*Pool) uint64
	FormatPoolID     func(PoolID) string
}

// SnapshotScheduler periodically creates snapshots as a fallback safety net.
type SnapshotScheduler[PoolID comparable, Pool any] struct {
	deps SnapshotSchedulerDeps[PoolID, Pool]
}

func NewSnapshotScheduler[PoolID comparable, Pool any](deps SnapshotSchedulerDeps[PoolID, Pool]) *SnapshotScheduler[PoolID, Pool] {
	return &SnapshotScheduler[PoolID, Pool]{deps: deps}
}

func (s *SnapshotScheduler[PoolID, Pool]) Run(ctx context.Context) error {
	if s.deps.FallbackInterval <= 0 {
		<-ctx.Done()
		return ctx.Err()
	}

	ticker := time.NewTicker(s.deps.FallbackInterval)
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

func (s *SnapshotScheduler[PoolID, Pool]) runOnce(ctx context.Context) error {
	for _, poolID := range s.deps.Lifecycle.ListActive() {
		pool, err := s.deps.LoadPool(ctx, poolID)
		if err != nil {
			return fmt.Errorf("load pool %s: %w", s.deps.FormatPoolID(poolID), err)
		}
		if pool == nil || s.deps.PoolLastBlock(pool) == 0 {
			continue
		}
		lastBlock := s.deps.PoolLastBlock(pool)
		if err := s.deps.CreateSnapshot(ctx, pool, lastBlock); err != nil {
			return fmt.Errorf("fallback snapshot pool %s: %w", s.deps.FormatPoolID(poolID), err)
		}
	}
	return nil
}

// RunOnce exposes fallback snapshot execution for tests and manual triggers.
func (s *SnapshotScheduler[PoolID, Pool]) RunOnce(ctx context.Context) error {
	return s.runOnce(ctx)
}
