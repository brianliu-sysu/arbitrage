package protocol

import (
	"context"
	"fmt"
	"time"
)

type SnapshotSchedulerProtocol[PoolID comparable, Pool any] interface {
	LoadPool(context.Context, PoolID) (*Pool, error)
	CreateSnapshot(context.Context, *Pool, uint64) error
	PoolLastBlock(*Pool) uint64
	FormatPoolID(PoolID) string
}

// SnapshotScheduler periodically creates snapshots as a fallback safety net.
type SnapshotScheduler[PoolID comparable, Pool any] struct {
	fallbackInterval time.Duration
	lifecycle        *PoolLifecycleService[PoolID]
	protocol         SnapshotSchedulerProtocol[PoolID, Pool]
}

func NewSnapshotScheduler[PoolID comparable, Pool any](
	fallbackInterval time.Duration,
	lifecycle *PoolLifecycleService[PoolID],
	protocol SnapshotSchedulerProtocol[PoolID, Pool],
) *SnapshotScheduler[PoolID, Pool] {
	return &SnapshotScheduler[PoolID, Pool]{
		fallbackInterval: fallbackInterval,
		lifecycle:        lifecycle,
		protocol:         protocol,
	}
}

func (s *SnapshotScheduler[PoolID, Pool]) Run(ctx context.Context) error {
	if s.fallbackInterval <= 0 {
		<-ctx.Done()
		return ctx.Err()
	}

	ticker := time.NewTicker(s.fallbackInterval)
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
	for _, poolID := range s.lifecycle.ListActive() {
		pool, err := s.protocol.LoadPool(ctx, poolID)
		if err != nil {
			return fmt.Errorf("load pool %s: %w", s.protocol.FormatPoolID(poolID), err)
		}
		if pool == nil || s.protocol.PoolLastBlock(pool) == 0 {
			continue
		}
		lastBlock := s.protocol.PoolLastBlock(pool)
		if err := s.protocol.CreateSnapshot(ctx, pool, lastBlock); err != nil {
			return fmt.Errorf("fallback snapshot pool %s: %w", s.protocol.FormatPoolID(poolID), err)
		}
	}
	return nil
}

// RunOnce exposes fallback snapshot execution for tests and manual triggers.
func (s *SnapshotScheduler[PoolID, Pool]) RunOnce(ctx context.Context) error {
	return s.runOnce(ctx)
}
