package protocol

import (
	"context"
	"fmt"

	"github.com/brianliu-sysu/uniswapv3/internal/domain/blockchain"
)

// SyncStartup defines the ordered capabilities required during sync startup.
type SyncStartup interface {
	StartAll(context.Context, uint64) error
	CatchUpAll(context.Context, uint64) error
	MarkAllPoolsReady(context.Context) error
	SetSystemReady(bool)
	RunScheduler(context.Context) error
}

func RunStartupAt(ctx context.Context, head blockchain.BlockHeader, startup SyncStartup) error {
	if err := startup.StartAll(ctx, head.Number); err != nil {
		return fmt.Errorf("start pools: %w", err)
	}

	if err := startup.CatchUpAll(ctx, head.Number); err != nil {
		return fmt.Errorf("catch up pools: %w", err)
	}

	if err := startup.MarkAllPoolsReady(ctx); err != nil {
		return fmt.Errorf("mark pools ready: %w", err)
	}

	startup.SetSystemReady(true)

	go func() {
		_ = startup.RunScheduler(ctx)
	}()

	return nil
}

// SyncOrchestrator coordinates bootstrap, catchup, and snapshot scheduling.
type SyncOrchestrator[PoolID comparable] struct {
	blocks    BlockReader
	lifecycle *PoolLifecycleService[PoolID]
	catchup   interface {
		CatchUpAll(context.Context, uint64) error
		CatchUpPool(context.Context, PoolID, uint64) error
	}
	blockConsumer interface {
		WithBlockConsumptionPaused(context.Context, func(context.Context) error) error
	}
	blockApply interface {
		MarkPoolsReady(context.Context, []PoolID) error
	}
	scheduler interface {
		Run(context.Context) error
	}
	readiness *ReadinessService[PoolID]
}

func NewSyncOrchestrator[PoolID comparable](
	blocks BlockReader,
	lifecycle *PoolLifecycleService[PoolID],
	catchup interface {
		CatchUpAll(context.Context, uint64) error
		CatchUpPool(context.Context, PoolID, uint64) error
	},
	blockConsumer interface {
		WithBlockConsumptionPaused(context.Context, func(context.Context) error) error
	},
	blockApply interface {
		MarkPoolsReady(context.Context, []PoolID) error
	},
	scheduler interface {
		Run(context.Context) error
	},
	readiness *ReadinessService[PoolID],
) *SyncOrchestrator[PoolID] {
	return &SyncOrchestrator[PoolID]{
		blocks:        blocks,
		lifecycle:     lifecycle,
		catchup:       catchup,
		blockConsumer: blockConsumer,
		blockApply:    blockApply,
		scheduler:     scheduler,
		readiness:     readiness,
	}
}

func (o *SyncOrchestrator[PoolID]) StartBootstrapAt(ctx context.Context, head blockchain.BlockHeader) error {
	return RunStartupAt(ctx, head, o)
}

func (o *SyncOrchestrator[PoolID]) StartAll(ctx context.Context, blockNumber uint64) error {
	return o.lifecycle.StartAll(ctx, blockNumber)
}

func (o *SyncOrchestrator[PoolID]) MarkAllPoolsReady(ctx context.Context) error {
	return o.blockApply.MarkPoolsReady(ctx, o.lifecycle.ListActive())
}

func (o *SyncOrchestrator[PoolID]) SetSystemReady(ready bool) {
	o.readiness.SetSystemReady(ready)
}

func (o *SyncOrchestrator[PoolID]) RunScheduler(ctx context.Context) error {
	if o.scheduler == nil {
		return nil
	}
	return o.scheduler.Run(ctx)
}

// AddPool registers a pool, catches it up until the chain head is stable, then marks it ready.
func (o *SyncOrchestrator[PoolID]) AddPool(ctx context.Context, poolID PoolID) error {
	if o == nil {
		return fmt.Errorf("sync orchestrator is nil")
	}
	if o.blocks == nil || o.lifecycle == nil || o.catchup == nil || o.blockConsumer == nil || o.blockApply == nil {
		return fmt.Errorf("sync orchestrator is missing dependencies")
	}
	return o.blockConsumer.WithBlockConsumptionPaused(ctx, func(ctx context.Context) error {
		head, err := o.blocks.GetLatestBlockHeader(ctx)
		if err != nil {
			return fmt.Errorf("load latest block: %w", err)
		}
		if err := o.lifecycle.RegisterAndBootstrapInactive(ctx, poolID, head.Number); err != nil {
			return err
		}
		if err := o.catchUpPoolUntilStableHead(ctx, poolID, head); err != nil {
			_ = o.lifecycle.Remove(ctx, poolID)
			return err
		}
		if err := o.blockApply.MarkPoolsReady(ctx, []PoolID{poolID}); err != nil {
			_ = o.lifecycle.Remove(ctx, poolID)
			return fmt.Errorf("mark pool ready: %w", err)
		}
		o.lifecycle.Activate(poolID)
		return nil
	})
}

func (o *SyncOrchestrator[PoolID]) CatchUpPool(ctx context.Context, poolID PoolID, blockNumber uint64) error {
	return o.catchup.CatchUpPool(ctx, poolID, blockNumber)
}

func (o *SyncOrchestrator[PoolID]) CatchUpAll(ctx context.Context, blockNumber uint64) error {
	return o.catchup.CatchUpAll(ctx, blockNumber)
}

func (o *SyncOrchestrator[PoolID]) catchUpPoolUntilStableHead(ctx context.Context, poolID PoolID, head blockchain.BlockHeader) error {
	for {
		if err := o.catchup.CatchUpPool(ctx, poolID, head.Number); err != nil {
			return fmt.Errorf("catch up pool to block %d: %w", head.Number, err)
		}
		current, err := o.blocks.GetLatestBlockHeader(ctx)
		if err != nil {
			return fmt.Errorf("load latest block after catchup: %w", err)
		}
		if current.Number <= head.Number {
			return nil
		}
		head = current
	}
}
